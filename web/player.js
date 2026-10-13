// Best Shader - WebGL2 player for the bundled Shadertoy passes.
//
// Mirrors the Go runner: shadertoy.glsl is the Image pass and names its
// buffers in `// #bufferA "..."`, `// #bufferB "..."` comments (all from
// `go run . -bundle`). The passes form a chain: the buffers render in letter
// order, each reading the one before it on iChannel0 (Buffer A reads
// nothing), and the Image pass reads the last buffer.
//
// The buffers are float targets like Shadertoy's, which needs
// EXT_color_buffer_float. Buffer A (poses.glsl) holds the cube poses: it only
// writes the bottom 5 rows, one column per cube (6 * SPH_FACE^2 = 294), and
// the scene reads them with texelFetch. So it renders into a fixed 32-bit
// float target of POSE_W x POSE_H instead of a canvas-sized one: cheaper, and
// every pose survives when the canvas is narrower than 294 pixels. The later
// buffers are canvas-sized and filter linear, so they are 32-bit float where
// the browser can filter that (OES_texture_float_linear), half float
// otherwise.
//
// URL parameters: ?scale=0.75 fixes the render scale (default: adaptive, at
// most 1080 lines), ?t=20 starts at iTime 20.
//
// Adaptive scale: the median frame interval over a window of frames decides,
// so single hitches (GC, tab switches) do not count. Vsync quantises the
// interval (a frame that misses 60 Hz shows at 33 ms), hence the wide gap
// between the two thresholds: one step down costs 0.64x the pixels, so a
// step up from below FAST_MS cannot land above SLOW_MS and oscillate.

const MAX_LINES = 1080; // render height cap at scale 1
const SCALE_STEP = 0.8; // scale ratio between adaptive levels
const MAX_LEVEL = 4; // lowest adaptive scale is SCALE_STEP^MAX_LEVEL (0.41)
const SLOW_MS = 45; // median frame interval above which the scale steps down
const FAST_MS = 20; // median frame interval below which it steps back up
const WINDOW = 90; // frame intervals per decision
const POSE_W = 1024; // Buffer A target: one column per cube (SPH_FACE <= 13)
const POSE_H = 8; // and the 5 rows of poses.glsl

const canvas = document.getElementById("view");
const params = new URLSearchParams(location.search);

function fail(msg) {
  const el = document.getElementById("error");
  el.textContent = msg;
  el.hidden = false;
  throw new Error(msg);
}

const gl = canvas.getContext("webgl2", {
  antialias: false,
  alpha: false,
  depth: false,
});
if (!gl) fail("WebGL2 is not available in this browser.");

const vertexSource = `#version 300 es
void main() {
    vec2 p = vec2((gl_VertexID << 1) & 2, gl_VertexID & 2);
    gl_Position = vec4(p * 2.0 - 1.0, 0.0, 1.0);
}
`;

// Same uniform set as the Go runner's fragmentPrefix.
const fragmentPrefix = `#version 300 es
precision highp float;
precision highp int;
precision highp sampler2D;
uniform vec3  iResolution;
uniform float iTime;
uniform float iTimeDelta;
uniform float iFrameRate;
uniform int   iFrame;
uniform vec4  iMouse;
uniform vec4  iDate;
uniform float iSampleRate;
uniform float iChannelTime[4];
uniform vec3  iChannelResolution[4];
uniform sampler2D iChannel0;
uniform sampler2D iChannel1;
uniform sampler2D iChannel2;
uniform sampler2D iChannel3;
out vec4 shadertoyFragColor;
`;

// The buffers keep their alpha (Buffer B carries depth there), the Image pass
// is opaque.
const bufferPostfix = `
void main() {
    shadertoyFragColor = vec4(0.0, 0.0, 0.0, 1.0);
    mainImage(shadertoyFragColor, gl_FragCoord.xy);
}
`;

const imagePostfix = `
void main() {
    shadertoyFragColor = vec4(0.0, 0.0, 0.0, 1.0);
    mainImage(shadertoyFragColor, gl_FragCoord.xy);
    shadertoyFragColor.a = 1.0;
}
`;

function compile(type, source, name) {
  const sh = gl.createShader(type);
  gl.shaderSource(sh, source);
  gl.compileShader(sh);
  if (!gl.getShaderParameter(sh, gl.COMPILE_STATUS)) {
    fail(`${name}: compile failed\n\n${gl.getShaderInfoLog(sh)}`);
  }
  return sh;
}

function buildPass(body, postfix, name) {
  const prog = gl.createProgram();
  gl.attachShader(prog, compile(gl.VERTEX_SHADER, vertexSource, "vertex"));
  gl.attachShader(
    prog,
    compile(gl.FRAGMENT_SHADER, fragmentPrefix + body + postfix, name),
  );
  gl.linkProgram(prog);
  if (!gl.getProgramParameter(prog, gl.LINK_STATUS)) {
    fail(`${name}: link failed\n\n${gl.getProgramInfoLog(prog)}`);
  }
  const loc = {};
  for (const n of [
    "iResolution",
    "iTime",
    "iTimeDelta",
    "iFrameRate",
    "iFrame",
    "iMouse",
    "iDate",
    "iChannel0",
    "iChannelResolution",
  ]) {
    loc[n] = gl.getUniformLocation(prog, n);
  }
  return { prog, loc };
}

async function fetchText(url) {
  const res = await fetch(url);
  if (!res.ok) fail(`${url}: ${res.status} ${res.statusText}`);
  return res.text();
}

const imageSrc = await fetchText("shadertoy.glsl");
const bufferNames = [];
for (const letter of "ABCD") {
  const re = new RegExp(`^\\s*//\\s*#buffer${letter}\\s+"([^"]+)"`, "m");
  const name = imageSrc.match(re)?.[1];
  if (!name) break;
  bufferNames.push(name);
}
if (bufferNames.length && !gl.getExtension("EXT_color_buffer_float")) {
  fail(
    "This browser cannot render to float textures (EXT_color_buffer_float).",
  );
}
const floatLinear = !!gl.getExtension("OES_texture_float_linear");
const buffers = [];
for (const name of bufferNames) {
  buffers.push(buildPass(await fetchText(name), bufferPostfix, name));
}
const image = buildPass(imageSrc, imagePostfix, "shadertoy.glsl");

gl.bindVertexArray(gl.createVertexArray());

// ---- buffer targets ----------------------------------------------------------
const targets = []; // { tex, fbo, w, h }, one per buffer

function makeTarget(w, h, internal, filter) {
  const tex = gl.createTexture();
  gl.bindTexture(gl.TEXTURE_2D, tex);
  gl.texStorage2D(gl.TEXTURE_2D, 1, internal, w, h);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, filter);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, filter);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
  gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);

  const fbo = gl.createFramebuffer();
  gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
  gl.framebufferTexture2D(
    gl.FRAMEBUFFER,
    gl.COLOR_ATTACHMENT0,
    gl.TEXTURE_2D,
    tex,
    0,
  );
  if (gl.checkFramebufferStatus(gl.FRAMEBUFFER) !== gl.FRAMEBUFFER_COMPLETE) {
    fail("Buffer framebuffer is incomplete.");
  }
  gl.bindFramebuffer(gl.FRAMEBUFFER, null);
  return { tex, fbo, w, h };
}

// ensureTargets keeps the pose target and (re)allocates the canvas-sized ones
// at w x h.
function ensureTargets(w, h) {
  for (let k = 0; k < buffers.length; k++) {
    if (k === 0) {
      targets[0] ??= makeTarget(POSE_W, POSE_H, gl.RGBA32F, gl.NEAREST);
      continue;
    }
    const t = targets[k];
    if (t && t.w === w && t.h === h) continue;
    if (t) {
      gl.deleteFramebuffer(t.fbo);
      gl.deleteTexture(t.tex);
    }
    const internal = floatLinear ? gl.RGBA32F : gl.RGBA16F;
    targets[k] = makeTarget(w, h, internal, gl.LINEAR);
  }
}

// ---- sizing ------------------------------------------------------------------
const fixedScale = params.has("scale")
  ? Math.min(Math.max(parseFloat(params.get("scale")) || 1, 0.1), 2)
  : null;
let level = 0; // adaptive level, scale = SCALE_STEP^level
let scale = fixedScale ?? 1;

function resize() {
  const dpr = window.devicePixelRatio || 1;
  let w = canvas.clientWidth * dpr;
  let h = canvas.clientHeight * dpr;
  const cap = Math.min(1, MAX_LINES / h);
  w = Math.max(1, Math.round(w * cap * scale));
  h = Math.max(1, Math.round(h * cap * scale));
  if (canvas.width !== w || canvas.height !== h) {
    canvas.width = w;
    canvas.height = h;
  }
  ensureTargets(w, h);
}

// ---- input -------------------------------------------------------------------
let paused = false;
window.addEventListener("keydown", (e) => {
  if (e.code === "Space") {
    paused = !paused;
    e.preventDefault();
  } else if (e.code === "KeyF") {
    if (document.fullscreenElement) document.exitFullscreen();
    else document.documentElement.requestFullscreen();
  }
});
canvas.addEventListener("dblclick", () => {
  if (document.fullscreenElement) document.exitFullscreen();
  else document.documentElement.requestFullscreen();
});

// ---- frame loop --------------------------------------------------------------
let time = parseFloat(params.get("t")) || 0;
let frame = 0;
let last = null;
let samples = []; // frame intervals (ms) since the last decision
let settle = 30; // frames to skip after start, a scale change or a hidden tab

document.addEventListener("visibilitychange", () => {
  last = null;
  samples = [];
  settle = 30;
});

// setUniforms sets the uniforms of pass p at w x h and binds channel (the
// previous buffer's target, or null) on iChannel0.
function setUniforms(p, w, h, dt, channel) {
  const now = new Date();
  gl.uniform3f(p.loc.iResolution, w, h, 1);
  gl.uniform1f(p.loc.iTime, time);
  gl.uniform1f(p.loc.iTimeDelta, dt);
  gl.uniform1f(p.loc.iFrameRate, dt > 0 ? 1 / dt : 60);
  gl.uniform1i(p.loc.iFrame, frame);
  gl.uniform4f(p.loc.iMouse, 0, 0, 0, 0);
  gl.uniform4f(
    p.loc.iDate,
    now.getFullYear(),
    now.getMonth(),
    now.getDate(),
    now.getHours() * 3600 + now.getMinutes() * 60 + now.getSeconds(),
  );
  gl.uniform1i(p.loc.iChannel0, 0);
  const res = new Float32Array(12);
  if (channel) res.set([channel.w, channel.h, 1]);
  gl.uniform3fv(p.loc.iChannelResolution, res);
  gl.bindTexture(gl.TEXTURE_2D, channel ? channel.tex : null);
}

function adapt(ms) {
  if (fixedScale !== null) return;
  if (settle > 0) {
    settle--;
    return;
  }
  samples.push(ms);
  if (samples.length < WINDOW) return;
  const median = samples.sort((a, b) => a - b)[WINDOW >> 1];
  samples = [];
  let next = level;
  if (median > SLOW_MS && level < MAX_LEVEL) next++;
  else if (median < FAST_MS && level > 0) next--;
  if (next === level) return;
  level = next;
  scale = SCALE_STEP ** level;
  settle = 30;
  console.info(
    `median frame ${median.toFixed(1)} ms, render scale now ${scale.toFixed(2)}`,
  );
}

function draw(nowMs) {
  const raw = last === null ? 0 : nowMs - last;
  const dt = Math.min(raw / 1000, 0.1);
  last = nowMs;
  if (!paused) {
    time += dt;
    if (raw > 0) adapt(raw);
  }
  resize();

  // The buffer chain: each buffer reads the one before it.
  let prev = null;
  for (let k = 0; k < buffers.length; k++) {
    const t = targets[k];
    gl.bindFramebuffer(gl.FRAMEBUFFER, t.fbo);
    gl.viewport(0, 0, t.w, t.h);
    gl.useProgram(buffers[k].prog);
    setUniforms(buffers[k], t.w, t.h, dt, prev);
    gl.drawArrays(gl.TRIANGLES, 0, 3);
    prev = t;
  }

  // Image: the last buffer (if any) on iChannel0.
  gl.bindFramebuffer(gl.FRAMEBUFFER, null);
  gl.viewport(0, 0, canvas.width, canvas.height);
  gl.useProgram(image.prog);
  setUniforms(image, canvas.width, canvas.height, dt, prev);
  gl.drawArrays(gl.TRIANGLES, 0, 3);

  if (!paused) frame++;
  requestAnimationFrame(draw);
}

requestAnimationFrame(draw);
