// sphere.glsl - the cube ball in the scene: the cube poses Buffer A wrote
// (poses.glsl, sphere_pose.glsl explains the motion), and the ray queries
// against the cubes. Buffer A is on iChannel0.
//
// Units here are cube edges, in ball space: origin at the ball centre, y up.
// SPH_SCALE converts to world units, SPH_YAW turns the ball about y.

#include "sphere_data.glsl"

const float SPH_RHO = 0.8660254;  // circumradius of a cube
const float SPH_REQ = 0.6203505;  // radius of the sphere of equal volume

// Set once per pixel by spherePrepare().
vec3 sphO;        // ball centre (world)
float sphRad;     // height of the ball centre when it rests on the floor (world)
float sphBound;   // no cube reaches further from the centre (cube edges)
vec2 sphYaw;      // cos and sin of SPH_YAW

void spherePrepare() {
    vec2 h = texelFetch(iChannel0, ivec2(0, 4), 0).xy;
    sphRad = h.x;
    sphO = vec3(SPH_POS.x, h.y, SPH_POS.y);
    sphYaw = vec2(cos(SPH_YAW), sin(SPH_YAW));
    sphBound = SPH_RHI + SPH_RHO;
}

// Centre of cube i (ball space).
vec3 cubeCentre(int i) { return texelFetch(iChannel0, ivec2(i, 0), 0).xyz; }

// Centre and axes of cube i (ball space).
void cubePose(int i, out vec3 c, out mat3 m) {
    c = texelFetch(iChannel0, ivec2(i, 0), 0).xyz;
    vec3 x = texelFetch(iChannel0, ivec2(i, 1), 0).xyz;
    vec3 y = texelFetch(iChannel0, ivec2(i, 2), 0).xyz;
    m = mat3(x, y, cross(x, y));
}

// World point/direction <-> ball space.
vec3 toBall(vec3 p) {
    vec3 q = p - sphO;
    return vec3(sphYaw.x * q.x - sphYaw.y * q.z, q.y, sphYaw.y * q.x + sphYaw.x * q.z) / SPH_SCALE;
}
vec3 ballDir(vec3 v) { return vec3(sphYaw.x * v.x - sphYaw.y * v.z, v.y, sphYaw.y * v.x + sphYaw.x * v.z); }
vec3 fromBallDir(vec3 v) { return vec3(sphYaw.x * v.x + sphYaw.y * v.z, v.y, -sphYaw.y * v.x + sphYaw.x * v.z); }

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
// cells, whose bounding spheres Buffer A writes. A query tests these first
// and evaluates only the cubes of the clusters it meets.

// Bounding sphere of cluster k (ball space): centre, radius.
vec4 clusterSphere(int k) { return texelFetch(iChannel0, ivec2(k, 3), 0); }

// First cell row/column of block b of a face (as in poses.glsl).
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
// height of the ball centre when it rests on the floor, and the shell's
// porosity.
const float SPH_SOLID = 0.8;
float sphereSky(vec3 p) {
    vec3 v = sphO - p;
    float d2 = dot(v, v);
    float R = sphRad;
    return 1.0 - SPH_SOLID * min(R * R / d2, 1.0) * max(v.y, 0.0) / sqrt(d2);
}
