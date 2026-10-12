// sphere_pose.glsl - the cube ball's motion: where every cube is at a given
// time, as a closed formula of the cube's index and the time, no tables.
//
// The ball is a shell of 6 SPH_FACE^2 equal cubes with rounded edges. It
// bounces on the floor, slowly, like a rubber ball: its centre flies on
// parabolas of one gravity, and on every landing the ball squashes (its
// bottom presses flat and spreads, its top comes down, its waist swells)
// and after take-off the shape swings on, damped. As a regular ball (a
// cubed sphere, every cube facing outwards) it touches the floor three
// times, the third time deeper, winding up; then it jumps twice as high,
// and on the way up its cubes turn irregular about the centre (scattered,
// swirled, tumbled and sunk to different depths), most at the top; on the
// way down they return, regular again as it lands.
//
// Each cube owns a cell of a cubed sphere: the cone from the ball centre
// over a quadrilateral of grid vertices. A vertex moves as a function of the
// vertex alone (scatter, swirl and timing hash its grid position or vary
// smoothly over the sphere), so neighbouring cells always share their walls
// and the cells partition space. A cube stands at the radius where it just
// fits its cone; cubes in disjoint cones cannot touch. In the irregular
// state the cubes may stand SPH_SINK further in, which packs the ball as
// densely as the regular one at the price of rare, slight overlaps.
//
// cmd/spheregen mirrors this formula line for line (formula.go), checks it
// over the whole loop (no folded cells, the deepest overlap of two cubes,
// the floor, how smoothly the cubes move) and writes the constants in
// sphere_data.glsl.
//
// Units here are cube edges, in ball space: origin at the ball centre, y up.
//
// Defines:
//   -D SPH_U=0.5      freezes the morph at u (0 regular, 1 irregular)

#include "sphere_data.glsl"

float sphT;       // time (seconds)
float sphU;       // morph: 0 regular, 1 irregular
float sphQ;       // how far the floor presses the ball in (edges), > 0 only on the floor
float sphA;       // shape: > 0 squashed, < 0 stretched (edges)
float sphAir;     // height of the ball centre above its resting height in the air (world)

// ---- timing -----------------------------------------------------------------

float smootherstep(float x) { return x * x * x * (x * (x * 6.0 - 15.0) + 10.0); }

// How irregular the ball is at the share x of the big jump: it swells from
// take-off on, is fully irregular at the top and returns the same way,
// regular again as the ball lands.
float sphMorph(float x) {
    float s = sin(PI * x);
    return s * s;
}

// The bounce at iTime. The ball touches the floor three times with two
// small bounces between, the third time deeper and longer, and then jumps
// high; in that jump it turns irregular on the way up and is regular again
// as it lands. The centre moves like a ball's: on parabolas of one gravity in the
// air, and on the floor as a spring that presses in by sphQ, from the
// landing speed of the last arc to the take-off speed of the next (the
// wind-up presses further in on top). The shape follows sphQ on the floor;
// after take-off it swings on at the spring's frequency, damped and faded
// out before the next landing, about the centre.
void sphereTime() {
    sphT = iTime;
    float t = mod(sphT, SPH_PERIOD);
    sphU = 0.0;
    sphQ = 0.0;
    sphA = 0.0;
    sphAir = 0.0;
    for (int i = 0; i < 3; i++) {
        bool big = i == 2;
        float tc = big ? SPH_DEEP_T * SPH_CONTACT : SPH_CONTACT, ta = big ? SPH_BIG_AIR : SPH_AIR;
        float h = big ? SPH_BIG * SPH_HOP : SPH_HOP, v = big ? SPH_VBIG : SPH_VLAND;
        if (t < tc) {
            float x = t / tc, sx = sin(PI * x);
            sphQ = tc / PI * sx * mix(i == 0 ? SPH_VBIG : SPH_VLAND, v, smootherstep(x));
            if (big) sphQ += SPH_DEEP * sx * sx;
            sphA = sphQ;
            break;
        }
        t -= tc;
        if (t < ta || big) {
            float x = t / ta;
            if (big) sphU = sphMorph(x);
            const float w = PI / SPH_CONTACT;
            sphA = -v / w * exp(-SPH_DAMP * t) * sin(w * t) * (1.0 - smootherstep(x));
            sphAir = 4.0 * h * x * (1.0 - x);
            break;
        }
        t -= ta;
    }
#ifdef SPH_U
    sphU = float(SPH_U);
#endif
}

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

// Angular frequency of k cycles per loop: the motion repeats every period.
float sphOmega(float k) { return TAU * k / SPH_PERIOD; }

// Progress at the morph u of something at direction d with the random
// number h. Everything starts at take-off and arrives at the top, but at its
// own pace: u (1 + k (1 - u)) with k within +-SPH_SPREAD, ahead mostly as a
// front that sweeps from the upper front over the ball.
float sphStage(float u, float h, vec3 d) {
    const vec3 front = normalize(vec3(0.3, 0.8, -0.52));
    float late = mix(h, 0.5 - 0.5 * dot(d, front), SPH_SWEEP);
    return u * (1.0 + SPH_SPREAD * (1.0 - 2.0 * late) * (1.0 - u));
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
// the same from every face that shares it) at the morph u: scattered and
// swirled in the irregular state, spread away from the floor while pressed.
vec3 cellVertex(ivec3 k, float u) {
    vec3 d = nowell(vec3(k) / float(SPH_FACE));
    const int m = 2 * SPH_FACE + 1;
    ivec3 e = k + SPH_FACE;
    uint h = uint(e.x + m * (e.y + m * e.z));
    float s = sphStage(u, rnd(h, 3u), d);
    vec3 o = (vec3(rnd(h, 0u), rnd(h, 1u), rnd(h, 2u)) * 2.0 - 1.0) * (SPH_JITTER * s);
    // a smooth swirl moves neighbouring vertices alike, so it can move them
    // far without folding a cell: the curl of the stream function
    // sum cos(k.d + phase) over three waves, which keeps areas; its timing
    // must be smooth too
    float sw = SPH_WARP * sphStage(u, 0.5, d);
    const vec3 k0 = vec3(0.9, 1.3, -0.4), k1 = vec3(-1.2, 0.3, 1.1), k2 = vec3(0.5, -0.8, -1.3);
    o += cross(d, k0) * (sw * cos(dot(k0, d) + sphOmega(1.0) * sphT));
    o += cross(d, k1) * (sw * cos(dot(k1, d) + sphOmega(2.0) * sphT + 1.7));
    o += cross(d, k2) * (sw * cos(dot(k2, d) - sphOmega(1.0) * sphT + 4.1));
    d = normalize(d + o - d * dot(o, d));
    // pressed onto the floor, the bottom spreads: the vertices move away
    // from the bottom pole, most near it, monotonically in the angle from
    // it, so the cells widen there (and make room for the cubes to come in)
    // and narrow a little above the waist
    if (sphQ > 0.0) {
        float c = -d.y;
        d = normalize(d + vec3(d.x * c, 1.0 - c * c, d.z * c) * (SPH_SPLAY * sphQ * (1.0 + c) * (1.0 + c) / 4.0));
    }
    return d;
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

// Smooth minimum of a, b >= 0, never above min(a, b), and 0 where either is.
float sminP(float a, float b) {
    if (a <= 0.0 || b <= 0.0) return 0.0;
    return a * b / sqrt(sqrt(a * a * a * a + b * b * b * b));
}

// How far the rounded cube with axes m reaches below its centre.
float lowestOf(mat3 m) { return (0.5 - SPH_BEVEL) * (abs(m[0].y) + abs(m[1].y) + abs(m[2].y)) + SPH_BEVEL; }

// The smallest radius that keeps the cube inside its cone: the rounded
// cube's support along each wall normal against the wall's distance wd (a
// smooth maximum, never below the true one, so that the cube does not jerk
// when another wall takes over).
float coneFit(mat3 m, vec3 w0, vec3 w1, vec3 w2, vec3 w3, vec4 wd) {
    vec4 sup = vec4(dot(abs(w0 * m), vec3(1.0)), dot(abs(w1 * m), vec3(1.0)),
                    dot(abs(w2 * m), vec3(1.0)), dot(abs(w3 * m), vec3(1.0)))
             * (0.5 - SPH_BEVEL) + SPH_BEVEL + SPH_MARG;
    vec4 fit = sup / wd;
    return smax(smax(fit.x, fit.y, SPH_FIT_K), smax(fit.z, fit.w, SPH_FIT_K), SPH_FIT_K);
}

// Cube i at the morph u: its direction d, its axes m and the walls of its
// cell (planes through the centre and two neighbouring corners, normals
// inwards; wd is how far d stays from them). Returns its progress s
// towards the irregular state.
float cubeCell(int i, float u, out vec3 d, out mat3 m, out vec3 w0, out vec3 w1, out vec3 w2, out vec3 w3, out vec4 wd) {
    // the cell: corners counter-clockwise seen from outside
    int f = i / (SPH_FACE * SPH_FACE), r = i % (SPH_FACE * SPH_FACE);
    ivec3 fn, fu, fv;
    faceBasis(f, fn, fu, fv);
    ivec3 o = SPH_FACE * fn + (2 * (r % SPH_FACE) - SPH_FACE) * fu + (2 * (r / SPH_FACE) - SPH_FACE) * fv;
    vec3 q0 = cellVertex(o, u), q1 = cellVertex(o + 2 * fu, u);
    vec3 q2 = cellVertex(o + 2 * fu + 2 * fv, u), q3 = cellVertex(o + 2 * fv, u);

    // the regular state stands in the middle of the cell, the irregular one
    // off it
    uint h = uint(i) + 0x10000u;
    float s = sphStage(u, rnd(h, 0u), normalize(q0 + q1 + q2 + q3));
    vec2 xy = 0.5 + SPH_OFFSET * s * (vec2(rnd(h, 1u), rnd(h, 2u)) - 0.5);
    d = normalize(mix(mix(q0, q1, xy.x), mix(q3, q2, xy.x), xy.y));

    // axes: z outwards, x and y symmetric between the cell's two directions
    vec3 tu = q1 + q2 - q0 - q3, tv = q2 + q3 - q0 - q1;
    tu = normalize(tu - d * dot(tu, d));
    tv = normalize(tv - d * dot(tv, d));
    vec3 a = normalize(normalize(tu + tv) + normalize(tu - tv));
    m = mat3(a, cross(d, a), d);

    w0 = normalize(cross(q0, q1));
    w1 = normalize(cross(q1, q2));
    w2 = normalize(cross(q2, q3));
    w3 = normalize(cross(q3, q0));
    wd = vec4(dot(w0, d), dot(w1, d), dot(w2, d), dot(w3, d));

    // A tumbled cube reaches further towards the walls; in a narrow cell it
    // tumbles less, so that it need not stand out far beyond SPH_RCAP.
    float narrow = -smax(smax(-wd.x, -wd.y, SPH_NARROW_K), smax(-wd.z, -wd.w, SPH_NARROW_K), SPH_NARROW_K);
    float room = clamp((SPH_RCAP * narrow - 0.5 - SPH_MARG) / ((0.5 - SPH_BEVEL) * (sqrt(3.0) - 1.0)), 0.0, 1.0);
    vec3 axis = normalize(vec3(rnd(h, 3u), rnd(h, 4u), rnd(h, 5u)) * 2.0 - 1.0 + vec3(0.0, 0.0, 1e-3));
    m = axisAngle(axis, SPH_TUMBLE * mix(0.35, 1.0, rnd(h, 6u)) * room * s) * m;
    return s;
}

// Centre and axes of cube i (ball space) by the formula.
void cubeFormula(int i, out vec3 c, out mat3 m) {
    vec3 d, w0, w1, w2, w3;
    vec4 wd;
    float s = cubeCell(i, sphU, d, m, w0, w1, w2, w3, wd);

    // The cube stands where it fits its cone, or further out: at the radius
    // it holds, which moves straight from the regular radius to the cube's
    // own irregular one (so that the cube does not swing in first and out
    // again where its tumble makes it need more room). The irregular cubes
    // may stand SPH_SINK further in: neighbours seldom stand at the same
    // depth, so they rarely meet, and then only slightly.
    float hold = SPH_RREG;
    if (s > 0.0) {
        vec3 d1, v0, v1, v2, v3;
        mat3 m1;
        vec4 vd;
        cubeCell(i, 1.0, d1, m1, v0, v1, v2, v3, vd);
        hold = mix(SPH_RREG, smax(coneFit(m1, v0, v1, v2, v3, vd) - SPH_SINK, SPH_RIRR, SPH_SMAX_IRR_K), s);
    }
    float lo = coneFit(m, w0, w1, w2, w3, wd) - SPH_SINK * s;
    float rad = smax(lo, hold, SPH_SMAX_K);

    // The squash: the shell swells at the equator and comes down at the
    // top, and on the floor its lowest points are cut flat by the floor. A
    // cube the floor reaches turns onto the face that looks most downwards
    // (cubes that stand on an edge do not turn). The ball is regular
    // whenever it touches the floor.
    float shape = sphA * (SPH_BULGE * (1.0 - d.y * d.y) - SPH_TOP * d.y * d.y);
    float r1 = rad + shape;
    if (sphQ > 0.0 && i / (SPH_FACE * SPH_FACE) == 3) {
        float floorY = SPH_LOW - sphQ;
        float press = rad * -d.y + lowestOf(m) - floorY;
        vec3 ay = abs(vec3(m[0].y, m[1].y, m[2].y));
        int k = ay.y > ay.x ? 1 : 0;
        if (ay.z > ay[k]) k = 2;
        vec3 fd = m[k].y > 0.0 ? -m[k] : m[k];
        float wt = smoothstep(0.0, SPH_FLAT_TURN, press) * smoothstep(0.75, 0.85, -fd.y);
        if (wt > 0.0 && fd.y > -0.99999) {
            m = axisAngle(normalize(cross(fd, vec3(0.0, -1.0, 0.0))), wt * acos(-fd.y)) * m;
            lo = coneFit(m, w0, w1, w2, w3, wd) - SPH_SINK * s;
            rad = smax(lo, hold, SPH_SMAX_K);
            r1 = rad + shape;
        }
        if (press > 0.0) r1 = min(r1, (floorY - lowestOf(m)) / -d.y);
    }
    // the squash moves a cube in no further than to lo
    float dr = rad - r1;
    c = d * (dr > 0.0 ? rad - sminP(dr, rad - lo) : r1);
}
