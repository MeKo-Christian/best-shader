// sphere_pose.glsl - the cube ball's motion: where every cube is at a given
// time, as a closed formula of the cube's index and the time, no tables.
//
// The ball is a shell of 6 SPH_FACE^2 equal cubes with rounded edges. It
// morphs without ever resting between a regular state (a cubed sphere, every
// cube facing outwards) and an irregular one (the cubes scattered, swirled,
// tumbled and sunk to different depths), breathes, and hops on the floor;
// after each landing a ripple runs up the shell.
//
// Each cube owns a cell of a cubed sphere: the cone from the ball centre
// over a quadrilateral of grid vertices. A vertex moves as a function of the
// vertex alone (scatter, swirl, wobble and timing hash its grid position or
// vary smoothly over the sphere), so neighbouring cells always share their
// walls and the cells partition space. A cube stands at the radius where it
// just fits its cone; cubes in disjoint cones cannot touch. In the irregular
// state the cubes may stand SPH_SINK further in, which packs the ball as
// densely as the regular one at the price of rare, slight overlaps.
//
// cmd/spheregen mirrors this formula line for line (formula.go), checks it
// over the whole loop (no folded cells, the deepest overlap of two cubes,
// the lift candidates) and writes the constants in sphere_data.glsl.
//
// Units here are cube edges, in ball space: origin at the ball centre, y up.
//
// Defines:
//   -D SPH_U=0.5      freezes the morph at u (0 regular, 1 irregular)

#include "sphere_data.glsl"

float sphT;       // time (seconds)
float sphU;       // morph: 0 regular, 1 irregular
float sphHop;     // phase of the current hop, 0 at the landing

void sphereTime() {
    sphT = iTime;
#ifdef SPH_U
    sphU = float(SPH_U);
#else
    sphU = 0.5 - 0.5 * cos(TAU * sphT / SPH_PERIOD);
#endif
    sphHop = fract(sphT / SPH_PERIOD * float(SPH_HOPS));
}

// ---- timing -----------------------------------------------------------------

uint hashU(uint x) {
    x ^= x >> 16;
    x *= 0x7feb352du;
    x ^= x >> 15;
    x *= 0x846ca68bu;
    x ^= x >> 16;
    return x;
}

// The c-th random number in [0, 1) of the key k, exact in float.
float rnd(uint k, uint c) { return float(hashU(k * 8u + c) >> 8) / 16777216.0; }

float smootherstep(float x) { return x * x * x * (x * (x * 6.0 - 15.0) + 10.0); }

// Angular frequency of k cycles per loop: the motion repeats every period.
float sphOmega(float k) { return TAU * k / SPH_PERIOD; }

// Eased progress of something at direction d with the random number h: it
// starts up to SPH_SPREAD of the morph late, in part as a front that sweeps
// from the upper front over the ball.
float sphStage(float h, vec3 d) {
    const vec3 front = normalize(vec3(0.3, 0.8, -0.52));
    float delay = mix(h, 0.5 - 0.5 * dot(d, front), SPH_SWEEP);
    return smootherstep(clamp((sphU - SPH_SPREAD * delay) / (1.0 - SPH_SPREAD), 0.0, 1.0));
}

// ---- cube poses -----------------------------------------------------------

void faceBasis(int f, out ivec3 n, out ivec3 u, out ivec3 v) {
    v = ivec3(0, 1, 0);
    if (f == 0) { n = ivec3(1, 0, 0); u = ivec3(0, 0, -1); }
    else if (f == 1) { n = ivec3(-1, 0, 0); u = ivec3(0, 0, 1); }
    else if (f == 2) { n = ivec3(0, 1, 0); u = ivec3(1, 0, 0); v = ivec3(0, 0, -1); }
    else if (f == 3) { n = ivec3(0, -1, 0); u = ivec3(1, 0, 0); v = ivec3(0, 0, 1); }
    else if (f == 4) { n = ivec3(0, 0, 1); u = ivec3(1, 0, 0); }
    else { n = ivec3(0, 0, -1); u = ivec3(-1, 0, 0); }
}

// Point of the cube [-1, 1]^3 onto the unit sphere (Nowell).
vec3 nowell(vec3 x) {
    vec3 q = x * x;
    return x * sqrt(1.0 - 0.5 * q.yzx - 0.5 * q.zxy + q.yzx * q.zxy / 3.0);
}

// Direction of the grid vertex k (a point of the cube [-SPH_FACE, SPH_FACE]^3,
// the same from every face that shares it): scattered and warped in the
// irregular state, wobbling as the morph proceeds.
vec3 cellVertex(ivec3 k) {
    vec3 d = nowell(vec3(k) / float(SPH_FACE));
    const int m = 2 * SPH_FACE + 1;
    ivec3 e = k + SPH_FACE;
    uint h = uint(e.x + m * (e.y + m * e.z));
    float s = sphStage(rnd(h, 3u), d);
    vec3 o = (vec3(rnd(h, 0u), rnd(h, 1u), rnd(h, 2u)) * 2.0 - 1.0) * (SPH_JITTER * s);
    // a smooth swirl moves neighbouring vertices alike, so it can move them
    // far without folding a cell: the curl of the stream function
    // sum cos(k.d + phase) over three waves, which keeps areas; its timing
    // must be smooth too
    float sw = SPH_WARP * sphStage(0.5, d);
    const vec3 k0 = vec3(0.9, 1.3, -0.4), k1 = vec3(-1.2, 0.3, 1.1), k2 = vec3(0.5, -0.8, -1.3);
    o += cross(d, k0) * (sw * cos(dot(k0, d) + sphOmega(1.0) * sphT));
    o += cross(d, k1) * (sw * cos(dot(k1, d) + sphOmega(2.0) * sphT + 1.7));
    o += cross(d, k2) * (sw * cos(dot(k2, d) - sphOmega(1.0) * sphT + 4.1));
    o += sin(vec3(sphOmega(7.0), sphOmega(9.0), sphOmega(11.0)) * sphT
             + TAU * vec3(rnd(h, 4u), rnd(h, 5u), rnd(h, 6u))) * (SPH_WOBBLE * s);
    return normalize(d + o - d * dot(o, d));
}

// Rotation by angle a about the unit axis k (Rodrigues).
mat3 axisAngle(vec3 k, float a) {
    float c = cos(a), s = sin(a);
    vec3 t = (1.0 - c) * k;
    return mat3(t.x * k + vec3(c, s * k.z, -s * k.y),
                t.y * k + vec3(-s * k.z, c, s * k.x),
                t.z * k + vec3(s * k.y, -s * k.x, c));
}

// Smooth maximum, never below max(a, b).
float smax(float a, float b, float k) { return 0.5 * (a + b + sqrt((a - b) * (a - b) + k * k)); }

// Outward push of a cube at direction d: after each landing a wave runs from
// the floor contact up the shell, fading.
float ripple(vec3 d) {
    float front = acos(clamp(-d.y, -1.0, 1.0)) / PI;   // 0 bottom, 1 top
    float x = sphHop * 2.5 - front;
    if (x <= 0.0 || x >= 1.0) return 0.0;
    float s = sin(PI * x);
    return SPH_RIPPLE * (1.0 - 0.7 * front) * s * s;
}

// Centre and axes of cube i (ball space) by the formula.
void cubeFormula(int i, out vec3 c, out mat3 m) {
    // the cell: corners counter-clockwise seen from outside
    int f = i / (SPH_FACE * SPH_FACE), r = i % (SPH_FACE * SPH_FACE);
    ivec3 fn, fu, fv;
    faceBasis(f, fn, fu, fv);
    ivec3 o = SPH_FACE * fn + (2 * (r % SPH_FACE) - SPH_FACE) * fu + (2 * (r / SPH_FACE) - SPH_FACE) * fv;
    vec3 q0 = cellVertex(o), q1 = cellVertex(o + 2 * fu);
    vec3 q2 = cellVertex(o + 2 * fu + 2 * fv), q3 = cellVertex(o + 2 * fv);

    // the regular state stands in the middle of the cell, the irregular one
    // off it
    uint h = uint(i) + 0x10000u;
    float s = sphStage(rnd(h, 0u), normalize(q0 + q1 + q2 + q3));
    vec2 xy = 0.5 + SPH_OFFSET * s * (vec2(rnd(h, 1u), rnd(h, 2u)) - 0.5);
    vec3 d = normalize(mix(mix(q0, q1, xy.x), mix(q3, q2, xy.x), xy.y));

    // axes: z outwards, x and y symmetric between the cell's two directions
    vec3 tu = q1 + q2 - q0 - q3, tv = q2 + q3 - q0 - q1;
    tu = normalize(tu - d * dot(tu, d));
    tv = normalize(tv - d * dot(tv, d));
    vec3 a = normalize(normalize(tu + tv) + normalize(tu - tv));
    m = mat3(a, cross(d, a), d);

    // the walls: planes through the centre and two neighbouring corners,
    // normals inwards; wd is how far d stays from them
    vec3 w0 = normalize(cross(q0, q1)), w1 = normalize(cross(q1, q2));
    vec3 w2 = normalize(cross(q2, q3)), w3 = normalize(cross(q3, q0));
    vec4 wd = vec4(dot(w0, d), dot(w1, d), dot(w2, d), dot(w3, d));

    // A tumbled cube reaches further towards the walls; in a narrow cell it
    // tumbles less, so that it need not stand out far beyond SPH_RCAP.
    float narrow = min(min(wd.x, wd.y), min(wd.z, wd.w));
    float room = clamp((SPH_RCAP * narrow - 0.5 - SPH_MARG) / ((0.5 - SPH_BEVEL) * (sqrt(3.0) - 1.0)), 0.0, 1.0);
    vec3 axis = normalize(vec3(rnd(h, 3u), rnd(h, 4u), rnd(h, 5u)) * 2.0 - 1.0 + vec3(0.0, 0.0, 1e-3));
    float ang = (SPH_TUMBLE * mix(0.35, 1.0, rnd(h, 6u)) * room
                 + SPH_WOBROT * sin(sphOmega(5.0) * sphT + TAU * rnd(h, 7u))) * s;
    m = axisAngle(axis, ang) * m;

    // the smallest radius that keeps the cube inside the cone: the rounded
    // cube's support along each wall normal against the wall's distance. The
    // irregular cubes may stand SPH_SINK further in: neighbours seldom stand
    // at the same depth, so they rarely meet, and then only slightly.
    vec4 sup = vec4(dot(abs(w0 * m), vec3(1.0)), dot(abs(w1 * m), vec3(1.0)),
                    dot(abs(w2 * m), vec3(1.0)), dot(abs(w3 * m), vec3(1.0)))
             * (0.5 - SPH_BEVEL) + SPH_BEVEL + SPH_MARG;
    vec4 fit = sup / wd;
    float hold = mix(SPH_RREG, SPH_RIRR, s) + SPH_BREATH * (0.5 - 0.5 * cos(sphOmega(4.0) * sphT));
    float rad = smax(max(max(fit.x, fit.y), max(fit.z, fit.w)) - SPH_SINK * s, hold, 0.15);
    c = d * (rad + ripple(d));
}

