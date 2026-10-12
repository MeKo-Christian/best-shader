package main

import (
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-gl/gl/v4.1-core/gl"
)

const vertexShaderSource = `#version 410
in vec2 vert;
void main() { gl_Position = vec4(vert, 0.0, 1.0); }
`

// fragmentPrefix declares the Shadertoy uniforms. iChannelN samples texture
// unit N; unbound channels read as black, so shaders that reference them still
// compile and run.
const fragmentPrefix = `#version 410
precision highp float;
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
`

const fragmentPostfix = `void main() {
    shadertoyFragColor = vec4(0.0, 0.0, 0.0, 1.0);
    mainImage(shadertoyFragColor, gl_FragCoord.xy);
}
`

// frameUniforms are the per-frame Shadertoy inputs.
type frameUniforms struct {
	width, height int
	time          float32
	timeDelta     float32
	frameRate     float32
	frame         int32
	mouse         [4]float32
	date          [4]float32
	channelRes    [4][3]float32 // iChannelResolution; zero for unbound channels
}

// program is a linked Shadertoy program plus its uniform locations.
type program struct {
	id                                         uint32
	res, time, delta, rate, frame, mouse, date int32
	channelRes                                 int32
}

func (p *program) delete() {
	if p != nil && p.id != 0 {
		gl.DeleteProgram(p.id)
	}
}

// buildProgram wraps body in the Shadertoy prefix/postfix plus defines,
// compiles and links it. Compiler logs are remapped to file:line.
func buildProgram(body *source, defs defineList) (*program, error) {
	frag := &source{}
	frag.addGenerated(fragmentPrefix, "<prefix>")
	frag.append(defs.source())
	frag.append(body)
	frag.addGenerated(fragmentPostfix, "<postfix>")

	vertShader, err := compileShader(vertexShaderSource, gl.VERTEX_SHADER)
	if err != nil {
		return nil, fmt.Errorf("vertex shader: %w", err)
	}
	defer gl.DeleteShader(vertShader)

	fragShader, err := compileShader(frag.String(), gl.FRAGMENT_SHADER)
	if err != nil {
		return nil, errors.New(frag.remapLog(err.Error()))
	}
	defer gl.DeleteShader(fragShader)

	id := gl.CreateProgram()
	gl.AttachShader(id, vertShader)
	gl.AttachShader(id, fragShader)
	gl.BindAttribLocation(id, 0, gl.Str("vert\x00"))
	gl.LinkProgram(id)

	var status int32
	gl.GetProgramiv(id, gl.LINK_STATUS, &status)

	if status == gl.FALSE {
		log := programLog(id)
		gl.DeleteProgram(id)

		return nil, fmt.Errorf("link: %s", frag.remapLog(log))
	}

	loc := func(name string) int32 { return gl.GetUniformLocation(id, gl.Str(name+"\x00")) }
	prog := &program{
		id: id, res: loc("iResolution"), time: loc("iTime"), delta: loc("iTimeDelta"),
		rate: loc("iFrameRate"), frame: loc("iFrame"), mouse: loc("iMouse"), date: loc("iDate"),
		channelRes: loc("iChannelResolution"),
	}
	// Channel N samples unit N. Only a multipass Image pass binds a texture
	// (Buffer A on unit 0); every other unit stays empty and reads black.
	gl.UseProgram(id)

	for i := range int32(4) {
		gl.Uniform1i(loc(fmt.Sprintf("iChannel%d", i)), i)
	}

	gl.Uniform1f(loc("iSampleRate"), 44100)

	return prog, nil
}

func compileShader(src string, kind uint32) (uint32, error) {
	shader := gl.CreateShader(kind)
	csrc, free := gl.Strs(src + "\x00")
	gl.ShaderSource(shader, 1, csrc, nil)
	free()
	gl.CompileShader(shader)

	var status int32
	gl.GetShaderiv(shader, gl.COMPILE_STATUS, &status)

	if status == gl.FALSE {
		var n int32
		gl.GetShaderiv(shader, gl.INFO_LOG_LENGTH, &n)
		log := strings.Repeat("\x00", int(n+1))
		gl.GetShaderInfoLog(shader, n, nil, gl.Str(log))
		gl.DeleteShader(shader)

		return 0, errors.New(log)
	}

	return shader, nil
}

func programLog(id uint32) string {
	var n int32
	gl.GetProgramiv(id, gl.INFO_LOG_LENGTH, &n)
	log := strings.Repeat("\x00", int(n+1))
	gl.GetProgramInfoLog(id, n, nil, gl.Str(log))

	return log
}

// quad is a fullscreen triangle strip.
type quad struct{ vao, vbo uint32 }

func newQuad() *quad {
	verts := []float32{-1, -1, 1, -1, -1, 1, 1, 1}
	screen := &quad{}
	gl.GenVertexArrays(1, &screen.vao)
	gl.GenBuffers(1, &screen.vbo)
	gl.BindVertexArray(screen.vao)
	gl.BindBuffer(gl.ARRAY_BUFFER, screen.vbo)
	gl.BufferData(gl.ARRAY_BUFFER, len(verts)*4, gl.Ptr(verts), gl.STATIC_DRAW)
	gl.VertexAttribPointerWithOffset(0, 2, gl.FLOAT, false, 2*4, 0)
	gl.EnableVertexAttribArray(0)
	gl.BindVertexArray(0)

	return screen
}

func (q *quad) delete() {
	gl.DeleteVertexArrays(1, &q.vao)
	gl.DeleteBuffers(1, &q.vbo)
}

// draw renders one frame with prog into the currently bound framebuffer.
func (q *quad) draw(prog *program, uni frameUniforms) {
	gl.Viewport(0, 0, glSize(uni.width), glSize(uni.height))
	gl.ClearColor(0.1, 0.1, 0.1, 1)
	gl.Clear(gl.COLOR_BUFFER_BIT)

	if prog == nil {
		return
	}

	gl.UseProgram(prog.id)
	gl.Uniform3f(prog.res, float32(uni.width), float32(uni.height), 1)
	gl.Uniform1f(prog.time, uni.time)
	gl.Uniform1f(prog.delta, uni.timeDelta)
	gl.Uniform1f(prog.rate, uni.frameRate)
	gl.Uniform1i(prog.frame, uni.frame)
	gl.Uniform4f(prog.mouse, uni.mouse[0], uni.mouse[1], uni.mouse[2], uni.mouse[3])
	gl.Uniform4f(prog.date, uni.date[0], uni.date[1], uni.date[2], uni.date[3])
	gl.Uniform3fv(prog.channelRes, int32(len(uni.channelRes)), &uni.channelRes[0][0])
	gl.BindVertexArray(q.vao)
	gl.DrawArrays(gl.TRIANGLE_STRIP, 0, 4)
	gl.BindVertexArray(0)
}

// texFormat describes the colour texture of an fbo.
type texFormat struct {
	internal  int32  // internal format, e.g. gl.RGBA8
	pixelType uint32 // matching client pixel type, e.g. gl.UNSIGNED_BYTE
	filter    int32  // min and mag filter
}

// shotFormat is the 8-bit target that offscreen shots are read back from.
var shotFormat = texFormat{internal: gl.RGBA8, pixelType: gl.UNSIGNED_BYTE, filter: gl.NEAREST}

// fbo is an offscreen render target with one colour texture (no mipmaps,
// clamped to the edge). newFBO leaves it bound as the framebuffer.
type fbo struct {
	id, tex       uint32
	width, height int
}

func newFBO(w, h int, format texFormat) (*fbo, error) {
	target := &fbo{width: w, height: h}
	gl.GenTextures(1, &target.tex)
	gl.BindTexture(gl.TEXTURE_2D, target.tex)
	gl.TexImage2D(gl.TEXTURE_2D, 0, format.internal, glSize(w), glSize(h), 0, gl.RGBA, format.pixelType, nil)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, format.filter)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, format.filter)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	gl.BindTexture(gl.TEXTURE_2D, 0)

	gl.GenFramebuffers(1, &target.id)
	gl.BindFramebuffer(gl.FRAMEBUFFER, target.id)
	gl.FramebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, target.tex, 0)

	if st := gl.CheckFramebufferStatus(gl.FRAMEBUFFER); st != gl.FRAMEBUFFER_COMPLETE {
		target.delete()
		return nil, fmt.Errorf("framebuffer incomplete: 0x%x", st)
	}

	return target, nil
}

func (f *fbo) delete() {
	gl.BindFramebuffer(gl.FRAMEBUFFER, 0)
	gl.DeleteFramebuffers(1, &f.id)
	gl.DeleteTextures(1, &f.tex)
}

// readPixels reads the bound read framebuffer into a top-down RGBA image.
// GL rows start at the bottom, so they are flipped. Alpha is forced to opaque
// because Shadertoy ignores it too.
func readPixels(w, h int) *image.RGBA {
	buf := make([]uint8, w*h*4)

	gl.PixelStorei(gl.PACK_ALIGNMENT, 1)
	gl.ReadPixels(0, 0, glSize(w), glSize(h), gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(buf))
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	stride := w * 4
	for y := range h {
		copy(img.Pix[y*stride:(y+1)*stride], buf[(h-1-y)*stride:(h-y)*stride])
	}

	for i := 3; i < len(img.Pix); i += 4 {
		img.Pix[i] = 255
	}

	return img
}

// glSize converts a pixel dimension to the int32 (GLsizei) the GL API takes.
// Real sizes always fit; the clamp only guards the conversion.
func glSize(size int) int32 {
	if size < 0 {
		return 0
	}

	if size > math.MaxInt32 {
		return math.MaxInt32
	}

	return int32(size)
}

func writePNG(path string, img image.Image) error {
	return writeOutput(path, func(w io.Writer) error {
		return png.Encode(w, img)
	})
}

// writeOutput creates path, including missing parent directories, and fills
// it via write.
func writeOutput(path string, write func(w io.Writer) error) error {
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return err
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}

	err = write(file)
	if err != nil {
		file.Close()
		return err
	}

	return file.Close()
}
