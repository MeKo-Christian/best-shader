// sphere.glsl - the cube ball: where every cube is at a given time, and the
// ray queries against the cubes.
//
// The ball is a shell of SPH_N equal cubes with rounded edges. At rest it is
// either regular (a cubed sphere, every cube facing outwards) or irregular
// (the cubes scattered, tumbled and interlocked). cmd/spheregen plans the
// motion between the two so that no two cubes ever touch and none reaches
// into the floor, verifies it, and bakes it into sphere_data.glsl. The
// forward motion runs over u in [0, 1]:
//   expand   [0, UA]   the whole ball grows until no cube hides another as
//                      seen from the centre
//   breathe  [UA, UB]  every cube moves radially outward to its own shuffle
//                      radius
//   shuffle  [UB, US]  at fixed radii the cubes slide along great circles to
//                      their target directions and tumble to their target
//                      orientations
//   settle   [US, UZ]  every cube moves radially inward
//   pack     [UZ, 1]   one after the other, the cubes sink until they touch
//                      their neighbours
// The ball rises as its lowest cube moves outwards (SPH_LIFT); the way back is
// the same motion reversed. cubePose() must match the generator's pose()
// (cmd/spheregen/plan.go): the generator verifies exactly this function.
//
// Units here are cube edges, in ball space: origin at the ball centre, y up.
// SPH_SCALE converts to world units, SPH_YAW turns the ball about y.
//
// Defines:
//   -D SPH_HOLD=3.0   seconds the ball rests regular and irregular
//   -D SPH_MOVE=9.0   seconds of the motion between them
//   -D SPH_U=0.5      freezes the motion at u (0 regular, 1 irregular)

#include "sphere_data.glsl"

#ifndef SPH_HOLD
#define SPH_HOLD 3.0
#endif
#ifndef SPH_MOVE
#define SPH_MOVE 9.0
#endif

const float SPH_RHO = 0.8660254;  // circumradius of a cube
const float SPH_REQ = 0.6203505;  // radius of the sphere of equal volume

// Set once per pixel by spherePrepare().
float sphU;       // forward time of the motion
vec3 sphO;        // ball centre (world)
float sphBound;   // no cube reaches further from the centre (cube edges)
vec2 sphYaw;      // cos and sin of SPH_YAW

// ---- timing -----------------------------------------------------------------

uint hashU(uint x) {
    x ^= x >> 16;
    x *= 0x7feb352du;
    x ^= x >> 15;
    x *= 0x846ca68bu;
    x ^= x >> 16;
    return x;
}

// Per cube and phase random number in [0, 1), exact in float.
float cubeHash(int i, int phase) {
    return float(hashU(uint(i) * 3u + uint(phase)) >> 8) / 16777216.0;
}

float smootherstep(float x) { return x * x * x * (x * (x * 6.0 - 15.0) + 10.0); }

// Eased progress in the phase [a, b] of a cube whose start is delayed by
// sp * h of the phase.
float stage(float u, float a, float b, float h, float sp) {
    float x = clamp((u - a) / (b - a), 0.0, 1.0);
    return smootherstep(clamp((x - sp * h) / (1.0 - sp), 0.0, 1.0));
}

// Quadratic B-spline over u through the control points of SPH_LIFT/SPH_BOUND.
#define BSPLINE(ctrl, u) { \
    float x_ = clamp(u, 0.0, 1.0) * float(SPH_LIFT_SEGS); \
    int k_ = min(int(x_), SPH_LIFT_SEGS - 1); \
    float f_ = x_ - float(k_); \
    res = 0.5 * (1.0 - f_) * (1.0 - f_) * ctrl[k_] + (0.5 + f_ - f_ * f_) * ctrl[k_ + 1] + 0.5 * f_ * f_ * ctrl[k_ + 2]; }

void spherePrepare() {
#ifdef SPH_U
    sphU = float(SPH_U);
#else
    // hold regular, forward, hold irregular, back
    float t = mod(iTime, 2.0 * (SPH_HOLD + SPH_MOVE));
    sphU = clamp((t - SPH_HOLD) / SPH_MOVE, 0.0, 1.0);
    if (t > 2.0 * SPH_HOLD + SPH_MOVE) sphU = 1.0 - (t - 2.0 * SPH_HOLD - SPH_MOVE) / SPH_MOVE;
#endif
    float res;
    BSPLINE(SPH_LIFT, sphU);
    sphO = vec3(SPH_POS.x, res * SPH_SCALE, SPH_POS.y);
    BSPLINE(SPH_BOUND, sphU);
    sphBound = res;
    sphYaw = vec2(cos(SPH_YAW), sin(SPH_YAW));
}

// World point/direction <-> ball space.
vec3 toBall(vec3 p) {
    vec3 q = p - sphO;
    return vec3(sphYaw.x * q.x - sphYaw.y * q.z, q.y, sphYaw.y * q.x + sphYaw.x * q.z) / SPH_SCALE;
}
vec3 ballDir(vec3 v) { return vec3(sphYaw.x * v.x - sphYaw.y * v.z, v.y, sphYaw.y * v.x + sphYaw.x * v.z); }
vec3 fromBallDir(vec3 v) { return vec3(sphYaw.x * v.x + sphYaw.y * v.z, v.y, -sphYaw.y * v.x + sphYaw.x * v.z); }

// ---- cube poses -----------------------------------------------------------

void faceBasis(int f, out vec3 n, out vec3 u, out vec3 v) {
    v = vec3(0.0, 1.0, 0.0);
    if (f == 0) { n = vec3(1.0, 0.0, 0.0); u = vec3(0.0, 0.0, -1.0); }
    else if (f == 1) { n = vec3(-1.0, 0.0, 0.0); u = vec3(0.0, 0.0, 1.0); }
    else if (f == 2) { n = vec3(0.0, 1.0, 0.0); u = vec3(1.0, 0.0, 0.0); v = vec3(0.0, 0.0, -1.0); }
    else if (f == 3) { n = vec3(0.0, -1.0, 0.0); u = vec3(1.0, 0.0, 0.0); v = vec3(0.0, 0.0, 1.0); }
    else if (f == 4) { n = vec3(0.0, 0.0, 1.0); u = vec3(1.0, 0.0, 0.0); }
    else { n = vec3(0.0, 0.0, -1.0); u = vec3(-1.0, 0.0, 0.0); }
}

// Point (x, y) in [-1, 1]^2 of a cube face onto the unit sphere (Nowell).
vec3 spherify(vec3 n, vec3 u, vec3 v, float x, float y) {
    float xx = x * x, yy = y * y;
    return n * sqrt(1.0 - 0.5 * xx - 0.5 * yy + xx * yy / 3.0)
         + u * (x * sqrt(0.5 - yy / 6.0)) + v * (y * sqrt(0.5 - xx / 6.0));
}

// Face coordinates of cube k in the regular ball.
void regularCell(int k, out vec3 n, out vec3 u, out vec3 v, out vec2 xy) {
    int f = k / (SPH_FACE * SPH_FACE), r = k % (SPH_FACE * SPH_FACE);
    faceBasis(f, n, u, v);
    xy = (vec2(r % SPH_FACE, r / SPH_FACE) + 0.5) / float(SPH_FACE) * 2.0 - 1.0;
}

// Regular direction and axes: z outwards, x and y symmetric between the two
// grid lines through the cube.
void regularPose(int k, out vec3 d, out mat3 m) {
    const float e = 1.0 / 64.0;
    vec3 n, u, v;
    vec2 xy;
    regularCell(k, n, u, v, xy);
    d = spherify(n, u, v, xy.x, xy.y);
    vec3 tu = spherify(n, u, v, xy.x + e, xy.y) - spherify(n, u, v, xy.x - e, xy.y);
    vec3 tv = spherify(n, u, v, xy.x, xy.y + e) - spherify(n, u, v, xy.x, xy.y - e);
    tu = normalize(tu - d * dot(tu, d));
    tv = normalize(tv - d * dot(tv, d));
    vec3 a = normalize(normalize(tu + tv) + normalize(tu - tv));
    m = mat3(a, cross(d, a), d);
}

vec2 snorm2(uint x) { return max(vec2(ivec2(int(x << 16u), int(x)) >> 16) / 32767.0, -1.0); }
vec2 unorm2(uint x) { return vec2(x & 0xffffu, x >> 16u) / 65535.0; }

vec3 octDecode(vec2 e) {
    vec3 v = vec3(e, 1.0 - abs(e.x) - abs(e.y));
    if (v.z < 0.0) v.xy = (1.0 - abs(e.yx)) * vec2(e.x >= 0.0 ? 1.0 : -1.0, e.y >= 0.0 ? 1.0 : -1.0);
    return normalize(v);
}

// Rotation by angle a about the unit axis k (Rodrigues).
mat3 axisAngle(vec3 k, float a) {
    float c = cos(a), s = sin(a);
    vec3 t = (1.0 - c) * k;
    return mat3(t.x * k + vec3(c, s * k.z, -s * k.y),
                t.y * k + vec3(-s * k.z, c, s * k.x),
                t.z * k + vec3(s * k.y, -s * k.x, c));
}

vec3 slerpDir(vec3 a, vec3 b, float t) {
    float th = acos(clamp(dot(a, b), -1.0, 1.0));
    if (th < 1e-6) return normalize(mix(a, b, t));
    return (a * sin((1.0 - t) * th) + b * sin(t * th)) / sin(th);
}

// Radius and the shuffle progress es of cube i. The generator nests the
// phases as mix(mix(mix(mix(REST, R0, ea), rs, eb), r1, et), rEnd, ep); they
// follow each other, so only the current one needs evaluating.
float cubeRadius(int i, uvec4 q, out float es) {
    es = 0.0;
    if (sphU < SPH_UA) return mix(SPH_REST, SPH_R0, smootherstep(clamp(sphU / SPH_UA, 0.0, 1.0)));
    float rs = unorm2(q.w).x * SPH_RMAX;
    if (sphU < SPH_UB) return mix(SPH_R0, rs, stage(sphU, SPH_UA, SPH_UB, cubeHash(i, 0), SPH_SPREAD));
    if (sphU < SPH_US) {
        es = stage(sphU, SPH_UB, SPH_US, cubeHash(i, 1), SPH_SPREAD);
        return rs;
    }
    es = 1.0;
    float r1 = unorm2(q.z).y * SPH_RMAX;
    if (sphU < SPH_UZ) return mix(rs, r1, stage(sphU, SPH_US, SPH_UZ, cubeHash(i, 2), SPH_SPREAD));
    return mix(r1, unorm2(q.w).y * SPH_RMAX, stage(sphU, SPH_UZ, 1.0, cubeHash(i, 3), SPH_PACK_SPREAD));
}

// Centre of cube i (ball space).
vec3 cubeCentre(int i) {
    uvec4 q = SPH_DATA[i];
    float es;
    float r = cubeRadius(i, q, es);
    vec3 n, u, v;
    vec2 xy;
    regularCell(i, n, u, v, xy);
    vec3 d0 = spherify(n, u, v, xy.x, xy.y);
    if (es <= 0.0) return r * d0;
    vec3 d1 = octDecode(snorm2(q.x));
    return r * (es >= 1.0 ? d1 : slerpDir(d0, d1, es));
}

// Centre and axes of cube i (ball space).
void cubePose(int i, out vec3 c, out mat3 m) {
    uvec4 q = SPH_DATA[i];
    float es;
    float r = cubeRadius(i, q, es);
    vec3 d0;
    regularPose(i, d0, m);
    c = r * d0;
    if (es > 0.0) {
        c = r * slerpDir(d0, octDecode(snorm2(q.x)), es);
        m = axisAngle(octDecode(snorm2(q.y)), unorm2(q.z).x * PI * es) * m;
    }
}

// ---- ray queries ------------------------------------------------------------

// Rounded cube of edge 1 at the origin: distance and outward normal.
float roundCube(vec3 p) {
    vec3 q = abs(p) - (0.5 - SPH_BEVEL);
    return length(max(q, 0.0)) + min(max(q.x, max(q.y, q.z)), 0.0) - SPH_BEVEL;
}
vec3 roundCubeNormal(vec3 p) {
    vec3 q = abs(p) - (0.5 - SPH_BEVEL);
    vec3 a = abs(p);
    if (max(q.x, max(q.y, q.z)) <= 0.0) return sign(p) * step(a.yzx, a) * step(a.zxy, a);
    return normalize(sign(p) * max(q, 0.0));
}

// Rounded cube hit of a cube-space unit ray: the bounding box by slabs, then
// a few sphere-tracing steps over the rounded edges. -1 on a miss.
float roundCubeHit(vec3 ro, vec3 rd) {
    vec3 ird = 1.0 / rd;
    vec3 t1 = (-0.5 - ro) * ird, t2 = (0.5 - ro) * ird;
    vec3 tn = min(t1, t2), tf = max(t1, t2);
    float t = max(max(tn.x, tn.y), tn.z), tOut = min(min(tf.x, tf.y), tf.z);
    if (t > tOut || tOut < 0.0) return -1.0;
    t = max(t, 0.0);
    for (int k = 0; k < 12; k++) {
        float d = roundCube(ro + rd * t);
        if (d < 1e-4) return t;
        t += d;
        if (t > tOut) break;
    }
    return -1.0;
}

// Ray against the ball's bounding sphere (ball space, unit rd): entry, exit.
vec2 boundHit(vec3 ro, vec3 rd) {
    float b = dot(ro, rd);
    float h = b * b - dot(ro, ro) + sphBound * sphBound;
    if (h < 0.0) return vec2(1.0, -1.0);
    h = sqrt(h);
    return vec2(-b - h, -b + h);
}

// ---- culling ------------------------------------------------------------------
// A pixel asks the cubes many times: its camera samples, and the light from
// each hit. The cubes form 54 clusters, on every cube face 3 x 3 blocks of
// cells, whose bounding spheres over u the generator bakes. A query tests
// these first and evaluates only the cubes of the clusters it meets.

// Bounding sphere of cluster k at the current u (ball space): centre, radius.
vec4 clusterSphere(int k) {
    float x = clamp(sphU, 0.0, 1.0) * float(SPH_CL_SEGS);
    int j = min(int(x), SPH_CL_SEGS - 1);
    float f = x - float(j);
    int o = k * (SPH_CL_SEGS + 2) + j;
    return 0.5 * (1.0 - f) * (1.0 - f) * SPH_CLUSTER[o] + (0.5 + f - f * f) * SPH_CLUSTER[o + 1]
         + 0.5 * f * f * SPH_CLUSTER[o + 2];
}

// First cell row/column of block b of a face (cmd/spheregen clusterBlock()).
int blockStart(int b) {
    return b == 0 ? 0 : b == 1 ? (SPH_FACE + 1) / 3 : b == 2 ? (2 * SPH_FACE + 3) / 3 : SPH_FACE;
}

// Whether rays from ro within the angle spread of the unit direction rd can
// touch the sphere (c, r).
bool bundleMeets(vec3 ro, vec3 rd, float spread, vec3 c, float r) {
    vec3 oc = c - ro;
    float along = dot(oc, rd);
    if (along < -r) return false;
    float rr = r + spread * max(along + r, 0.0);
    return dot(oc, oc) - along * along < rr * rr;
}

#define SPH_CAND 32
int sphCand[SPH_CAND];
vec3 sphCandC[SPH_CAND];
int sphNCand = 0;     // candidates for the camera rays of this pixel

// Collects the cubes whose bounding spheres the camera rays from ro within
// the angle spread of rd (world) can touch.
void sphereCandidates(vec3 ro, vec3 rd, float spread) {
    sphNCand = 0;
    vec3 bro = toBall(ro), brd = ballDir(rd);
    if (!bundleMeets(bro, brd, spread, vec3(0.0), sphBound)) return;
    for (int k = 0; k < 54; k++) {
        vec4 cs = clusterSphere(k);
        if (!bundleMeets(bro, brd, spread, cs.xyz, cs.w)) continue;
        int f = k / 9, bj = (k / 3) % 3, bi = k % 3;
        for (int j = blockStart(bj); j < blockStart(bj + 1); j++) {
            for (int i = blockStart(bi); i < blockStart(bi + 1); i++) {
                int id = (f * SPH_FACE + j) * SPH_FACE + i;
                vec3 c = cubeCentre(id);
                if (sphNCand < SPH_CAND && bundleMeets(bro, brd, spread, c, SPH_RHO)) {
                    sphCand[sphNCand] = id;
                    sphCandC[sphNCand++] = c;
                }
            }
        }
    }
}

// Nearest cube hit of a camera ray of this pixel (t > 0, world normal) or -1.
// id is the cube, lp the hit point in cube space (edge 1, centred).
float sphereHit(vec3 ro, vec3 rd, out vec3 nor, out int id, out vec3 lp) {
    nor = vec3(0.0);
    id = -1;
    lp = vec3(0.0);
    if (sphNCand == 0) return -1.0;
    vec3 bro = toBall(ro), brd = ballDir(rd);

    float tBest = 1e9;
    for (int k = 0; k < sphNCand; k++) {
        int i = sphCand[k];
        vec3 oc = bro - sphCandC[k];
        float b = dot(oc, brd);
        float h = b * b - dot(oc, oc) + SPH_RHO * SPH_RHO;
        if (h < 0.0 || -b - sqrt(h) > tBest || -b + sqrt(h) < 0.0) continue;
        vec3 c;
        mat3 m;
        cubePose(i, c, m);
        vec3 p = (bro - c) * m, d = brd * m;   // into cube space
        d += 1e-7 * step(abs(d), vec3(1e-7));
        float t = roundCubeHit(p, d);
        if (t > 0.0 && t < tBest) {
            tBest = t;
            id = i;
            lp = p + d * t;
            nor = m * roundCubeNormal(lp);
        }
    }
    if (id < 0) return -1.0;
    nor = fromBallDir(nor);
    return tBest * SPH_SCALE;
}

// Visibility of a sphere light (centre lc, radius lr, world) from the world
// point p through the cubes, each taken as a sphere of equal volume: the
// fraction of the light's disc not covered by their discs, multiplied over
// the cubes. Noise free, so the wide penumbrae need no stochastic sampling.
// The cube skip (the receiver itself) is left out.
float sphereLightVisUncached(vec3 p, vec3 lc, float lr, int skip) {
    vec3 bp = toBall(p);
    vec3 L = ballDir(lc - p) / SPH_SCALE;
    float D = length(L);
    vec3 l = L / D;
    float a = lr / SPH_SCALE / D;               // angular radius of the light
    // the cone towards the light must meet the ball
    float dc = length(bp);
    float along = -dot(bp, l);
    if (along < -sphBound) return 1.0;
    if (sqrt(max(dc * dc - along * along, 0.0)) > sphBound + a * max(along, 0.0) + sphBound * a) return 1.0;

    float vis = 1.0;
    for (int k = 0; k < 54; k++) {
        // a cube of the cluster covers part of the light only if its centre
        // lies within a * dist + SPH_REQ of the line towards the light
        vec4 cs = clusterSphere(k);
        vec3 v = cs.xyz - bp;
        if (dot(v, l) < -cs.w || length(cross(v, l)) > cs.w + SPH_REQ + a * (length(v) + cs.w)) continue;
        int f = k / 9, bj = (k / 3) % 3, bi = k % 3;
        for (int j = blockStart(bj); j < blockStart(bj + 1); j++) {
            for (int i = blockStart(bi); i < blockStart(bi + 1); i++) {
                int id = (f * SPH_FACE + j) * SPH_FACE + i;
                if (id == skip) continue;
                v = cubeCentre(id) - bp;
                float dl = dot(v, l);
                if (dl <= 0.0) continue;
                float dist = length(v);
                float b = SPH_REQ / dist;                 // angular radius of the cube
                float sep = length(cross(v, l)) / dist;   // angle between the discs
                if (sep >= a + b) continue;
                float cover = min(b * b / (a * a), 1.0) * (1.0 - smoothstep(abs(a - b), a + b, sep));
                vis *= 1.0 - cover;
            }
        }
        if (vis < 1e-3) return 0.0;
    }
    return vis;
}

// The same, once per pixel and receiver (a cube, or -1 for the floor): the
// penumbrae are far wider than a pixel, so the other camera samples that hit
// the same receiver reuse it.
int sphVisId = -2;
float sphVis;
float sphereLightVis(vec3 p, vec3 lc, float lr, int skip) {
    if (skip != sphVisId) {
        sphVis = sphereLightVisUncached(p, lc, lr, skip);
        sphVisId = skip;
    }
    return sphVis;
}

// Sky occlusion of a point p (normal n, world) on cube skip by the cubes
// near it, each taken as a sphere of equal volume: the sum of their view
// factors (r / d)^2 cos(theta) where their centres lie above the point's
// horizon. Cubes further than SPH_AO_RANGE are left out.
#define SPH_AO_RANGE 3.0
float sphereOcclusion(vec3 p, vec3 n, int skip) {
    vec3 bp = toBall(p), bn = ballDir(n);
    float occ = 0.0;
    for (int k = 0; k < 54; k++) {
        vec4 cs = clusterSphere(k);
        vec3 v = cs.xyz - bp;
        if (length(v) > cs.w + SPH_AO_RANGE || dot(v, bn) < -cs.w) continue;
        int f = k / 9, bj = (k / 3) % 3, bi = k % 3;
        for (int j = blockStart(bj); j < blockStart(bj + 1); j++) {
            for (int i = blockStart(bi); i < blockStart(bi + 1); i++) {
                int id = (f * SPH_FACE + j) * SPH_FACE + i;
                if (id == skip) continue;
                v = cubeCentre(id) - bp;
                float d2 = dot(v, v), c = dot(v, bn);
                if (d2 < SPH_AO_RANGE * SPH_AO_RANGE && c > 0.0) occ += SPH_REQ * SPH_REQ * c / (d2 * sqrt(d2));
            }
        }
    }
    return occ;
}

// How much of the sky a floor point (normal up) loses to the ball: the view
// factor of a sphere above the receiver, (R / d)^2 cos(theta), with R the
// height of the ball centre (the ball touches the floor) and the shell's
// porosity.
const float SPH_SOLID = 0.8;
float sphereSky(vec3 p) {
    vec3 v = sphO - p;
    float d2 = dot(v, v);
    float R = sphO.y;
    return 1.0 - SPH_SOLID * min(R * R / d2, 1.0) * max(v.y, 0.0) / sqrt(d2);
}
