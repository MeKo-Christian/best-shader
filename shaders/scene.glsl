// scene.glsl - Buffer B: the sharp scene, linear RGB plus eye-space depth in
// alpha. It reads the cube poses from Buffer A (poses.glsl) on iChannel0. The
// Image pass (main.glsl) turns depth into a circle of confusion and applies
// the depth of field.
//
// Geometry: a tiled floor (y = 0) in front of a cubic beam lattice whose
// region and beams come from lattice.glsl, and on the floor a ball of
// milky glass cubes that turns from regular to irregular and back
// (sphere.glsl). The lattice shares the floor's
// orientation: its cells are CELL_TILES x CELL_TILES tiles and its joints
// stand on tile joint crossings, so we see it at about 45 degrees. A world ray
// is mapped into lattice space and traversed cell by cell (3D DDA); in each
// cell only the beams and joints on that cell's edges and corners can be
// hit, and they are intersected analytically as boxes.
//
// The camera is a pinhole with lens shift (like a shift lens or an
// off-centre crop): the sensor stays vertical, so vertical beams stay
// parallel. Its values and the floor grid come from a least-squares fit of the
// grout lines in reference/reference.png (about 3 px RMS), constrained to this
// physical model: square tiles on a flat plane seen by a pinhole camera.
//
// Defines (lengths in world units):
//   -D BEAM=0.13       beam thickness in lattice cells
//   -D CELL_TILES=2.0  lattice cell size in floor tiles (integer)
//   -D TILE=0.7336     floor tile size
//   -D TILE_ROT=47.006 rotation of the floor grid and the lattice against the
//                      camera in degrees
//   -D CAM_H=1.069     camera height
//   -D CAM_X=0.735     camera x
//   -D FOCAL=2.3034    focal length in half image heights (FOV 47.1 degrees)
//   -D SHIFT_X=-0.1158 lens shift in half image heights
//   -D SHIFT_Y=-0.2029
//   -D ROLL=-0.0927    camera roll in degrees
//   -D PITCH=0.0       camera pitch in degrees (positive = up)
//   -D CAM_DX=1.6      the camera goes round an ellipse this far either way
//   -D CAM_DY=0.8      and this high (world), turning to keep the ball in place
//   -D FADE=1.2        how fast light dies with depth inside the lattice
//   -D AA=2            supersampling (AA x AA samples per pixel)
//   -D AA_COC=1.5      blur radius in pixels above which a lattice pixel
//                      gets one sample: the bokeh hides the aliasing there
//   -D MAX_STEPS=160   DDA step limit per lattice ray
//
// Floor and light (floor lengths in tiles):
//   -D GAP=0.08        joint width between the slab walls
//   -D BEVEL=0.04      radius of the rounded slab edges
//   -D SLAB=0.12       depth of the joints down to the grout
//   -D ALBEDO=0.66     mean albedo of the marble slabs
//   -D VEIN=0.2        darkening of the marble veins
//   -D ROUGH=0.35      GGX roughness of the honed marble
//   -D LIGHT_R=1.5     radius of the sphere light (soft shadows, highlight)
//   -D LIGHT_X=-2.0    light position (world): above the open room, fitted
//   -D LIGHT_Y=6.0     to the brightness falloff of the reference floor
//   -D LIGHT_Z=10.0
//   -D MOOD=1          0: white light and sky; 1: an orange key light, a red
//                      rim light from the right and a purple sky
//   -D LIGHT_SAT=0.35  saturation of the MOOD 1 light colours (1: the full
//                      palette, 0: grey)
//   -D RIM_X=7.5       rim light position (world, MOOD 1)
//   -D RIM_Y=2.0
//   -D RIM_Z=11.0
//   -D RIM_R=1.0       radius of the rim light
//
// Cube ball (motion and placement in sphere.glsl):
//   -D GLASS=1          1: milky glass cubes; 0: polished marble
//   -D GLASS_DENSITY=2.5 how fast the milk hides the view through a cube
//                       (per cube edge; the light passes exp(-density path))
//   -D GLASS_LAYERS=3   most cubes a camera ray passes through
//   -D GLASS_TINT=0.3   saturation of the cubes' glass colours (the palette)
//   -D SPH_OPACITY=0.7  share of the light a glass cube stops in shadows
//                       and occlusion (1 for marble)
//   -D CUBE_ALBEDO=0.66 mean albedo of the cube marble (the floor's)
//   -D CUBE_ROUGH=0.06  GGX roughness of the polished cubes
//   -D SPH_DEBUG        shows how many cubes each pixel tests (green to red,
//                       white: the candidate list overflows)

#ifndef BEAM
#define BEAM 0.13
#endif
#ifndef CELL_TILES
#define CELL_TILES 2.0
#endif
#ifndef TILE
#define TILE 0.7336
#endif
#ifndef TILE_ROT
#define TILE_ROT 47.006
#endif
#ifndef CAM_H
#define CAM_H 1.069
#endif
#ifndef CAM_X
#define CAM_X 0.735
#endif
#ifndef FOCAL
#define FOCAL 2.3034
#endif
#ifndef SHIFT_X
#define SHIFT_X -0.1158
#endif
#ifndef SHIFT_Y
#define SHIFT_Y -0.2029
#endif
#ifndef ROLL
#define ROLL -0.0927
#endif
#ifndef PITCH
#define PITCH 0.0
#endif
#ifndef CAM_DX
#define CAM_DX 1.6
#endif
#ifndef CAM_DY
#define CAM_DY 0.8
#endif
#ifndef FADE
#define FADE 1.2
#endif
#ifndef AA
#define AA 2
#endif
#ifndef AA_COC
#define AA_COC 1.5
#endif
#ifndef GAP
#define GAP 0.08
#endif
#ifndef BEVEL
#define BEVEL 0.04
#endif
#ifndef SLAB
#define SLAB 0.12
#endif
#ifndef VEIN
#define VEIN 0.2
#endif
#ifndef ALBEDO
#define ALBEDO 0.66
#endif
#ifndef ROUGH
#define ROUGH 0.35
#endif
#ifndef LIGHT_R
#define LIGHT_R 1.5
#endif
#ifndef LIGHT_X
#define LIGHT_X -2.0
#endif
#ifndef LIGHT_Y
#define LIGHT_Y 6.0
#endif
#ifndef LIGHT_Z
#define LIGHT_Z 10.0
#endif
#ifndef MOOD
#define MOOD 1
#endif
#ifndef RIM_X
#define RIM_X 7.5
#endif
#ifndef RIM_Y
#define RIM_Y 2.0
#endif
#ifndef RIM_Z
#define RIM_Z 11.0
#endif
#ifndef RIM_R
#define RIM_R 1.0
#endif
#ifndef LIGHT_SAT
#define LIGHT_SAT 0.35
#endif
#ifndef CUBE_ALBEDO
#define CUBE_ALBEDO 0.66
#endif
#ifndef CUBE_ROUGH
#define CUBE_ROUGH 0.06
#endif
#ifndef GLASS
#define GLASS 1
#endif
#ifndef GLASS_DENSITY
#define GLASS_DENSITY 2.5
#endif
#ifndef GLASS_TINT
#define GLASS_TINT 0.3
#endif
#ifndef GLASS_LAYERS
#define GLASS_LAYERS 3
#endif
#if GLASS && !defined(SPH_OPACITY)
#define SPH_OPACITY 0.7
#endif

// Floor grid phase relative to the camera, in tiles (from the fit).
const vec2 TILE_OFF = vec2(0.5493, 0.0090);

#include "common.glsl"
#include "lattice.glsl"
#include "sphere.glsl"

#ifndef MAX_STEPS
#define MAX_STEPS 160
#endif
const float HB = 0.5 * BEAM;               // half beam, lattice cells
const float CELL = CELL_TILES * TILE;      // lattice cell, world units
// Depth (cells) beyond which the fade leaves the lattice black.
const float LAT_DMAX = 6.0 / (FADE * CELL) + 1.0;

// Light: a sphere light above the open room, in front of the camera.
const vec3 lightPos = vec3(LIGHT_X, LIGHT_Y, LIGHT_Z);

// The camera and the lights repeat every two loops of the ball.
const float SCENE_LOOP = 2.0 * SPH_PERIOD;

// Colours of the key light, the sky and the rim light (linear RGB), set per
// frame by lightsPrepare().
vec3 KEY_COL = vec3(1.0), SKY_COL = vec3(1.0), RIM_COL = vec3(0.0);

// The palette of the scene, of about equal brightness: orange, red, purple.
const vec3 PALETTE[3] = vec3[3](vec3(1.6, 0.52, 0.08), vec3(2.6, 0.09, 0.06), vec3(1.1, 0.33, 2.5));

#if MOOD == 1
const vec3 rimPos = vec3(RIM_X, RIM_Y, RIM_Z);

// The lights go round the palette, toned down to LIGHT_SAT at the same
// luminance; x in rounds. Each colour holds for a while and then blends into
// the next, since the blends are pale.
vec3 moodColour(float x) {
    x = fract(x) * 3.0;
    int i = int(x);
    vec3 c = mix(PALETTE[i], PALETTE[(i + 1) % 3], smoothstep(0.4, 1.0, fract(x)));
    return mix(vec3(dot(c, vec3(0.2126, 0.7152, 0.0722))), c, LIGHT_SAT);
}
#endif

// Over SCENE_LOOP the three lights pass the palette round once: at the start
// the key light is orange, the rim light red and the sky purple, then each
// takes on the colour of the next.
void lightsPrepare() {
#if MOOD == 1
    float x = iTime / SCENE_LOOP;
    KEY_COL = 1.2 * moodColour(x);
    RIM_COL = 0.95 * moodColour(x + 1.0 / 3.0);
    SKY_COL = 0.7 * moodColour(x + 2.0 / 3.0);
#endif
}

// Cells near the lattice: the region grown by one cell, since beams on the
// region's surface reach into the cells just outside it.
bool nearRegion(ivec3 c) {
    return c.y >= -1 && c.y <= LAT_YMAX && latSdf(vec2(c.xz) + 0.5) > -1.0;
}

void boxTest(vec3 ro, vec3 ird, vec3 c, vec3 hs, inout float tBest, inout vec3 cBest, inout vec3 hBest) {
    vec3 t1 = (c - hs - ro) * ird, t2 = (c + hs - ro) * ird;
    vec3 tn = min(t1, t2), tf = max(t1, t2);
    float n = max(max(tn.x, tn.y), tn.z);
    float f = min(min(tf.x, tf.y), tf.z);
    if (n < f && n > 0.0 && n < tBest) {
        tBest = n;
        cBest = c;
        hBest = hs;
    }
}

// Ray interval inside the half-space p[axis] * s < d.
vec2 halfSpace(vec3 ro, vec3 ird, int axis, float s, float d) {
    float t = (d * s - ro[axis]) * ird[axis];
    bool inside = ro[axis] * s < d;
    if (isinf(t) || isnan(t)) return inside ? vec2(-1e9, 1e9) : vec2(1e9, -1e9);
    return ird[axis] * s > 0.0 ? vec2(-1e9, t) : vec2(t, 1e9);
}

// Nearest lattice hit along a lattice-space ray (rd need not be unit), or -1.
// nor is the box face normal.
float traceLattice(vec3 ro, vec3 rd, out vec3 nor) {
    nor = vec3(0.0);
    vec3 ird = 1.0 / rd;

    // clip to the vertical extent and to the region grown by one cell
    vec2 ty = vec2(-1.0 - ro.y, float(LAT_YMAX) + 1.0 - ro.y) * ird.y;
    ty = vec2(min(ty.x, ty.y), max(ty.x, ty.y));
    vec2 hx = halfSpace(ro, ird, 0, 1.0, float(LAT_XC) + 1.0);
#if LAT_VIEW == 2
    vec2 hr = hx;
#else
    vec2 hz = halfSpace(ro, ird, 2, -1.0, 1.0 - float(LAT_ZF));
#if LAT_VIEW == 0
    vec2 hr = vec2(min(hx.x, hz.x), max(hx.y, hz.y));
#else
    vec2 hr = vec2(max(hx.x, hz.x), min(hx.y, hz.y));
#endif
#endif
    float tIn = max(ty.x, hr.x), tOut = min(ty.y, hr.y);
    if (tIn >= tOut || tOut < 0.0) return -1.0;

    float t = max(tIn, 0.0) + 1e-4;
    ivec3 c = ivec3(floor(ro + rd * t));
    vec3 stp = sign(rd);
    vec3 tMax = (vec3(c) + max(stp, 0.0) - ro) * ird;
    vec3 tDel = abs(ird);

    float tBest = 1e9;
    vec3 cBest = vec3(0.0), hBest = vec3(1.0);

    // States of the cell's eight corner joints, index x + 2y + 4z. A step
    // keeps the four corners the cells share: then only the corners whose
    // index has the bit side (1 << axis, or 0 for the low side) are new.
    int k[8];
    int axis = -1, side = 0;

    for (int i = ZERO; i < MAX_STEPS; i++) {
        float tExit = min(min(tMax.x, tMax.y), tMax.z);

        bool nearCell = nearRegion(c);
        if (nearCell) {
            if (latSdf(vec2(c.xz) + 0.5) > LAT_DMAX) break;

            int bit = axis < 0 ? 0 : 1 << axis;
            for (int j = ZERO; j < 8; j++)
                if (axis < 0 || (j & bit) == side) k[j] = joint(c + ivec3(j & 1, (j >> 1) & 1, j >> 2));

            vec3 o = vec3(c);
            for (int a = ZERO; a < 2; a++) {
                for (int b = ZERO; b < 2; b++) {
                    int ix = 2 * a + 4 * b, iy = a + 4 * b, iz = a + 2 * b;
                    if (edgeOn(c + ivec3(0, a, b), 0, k[ix], k[ix + 1])) boxTest(ro, ird, o + vec3(0.5, a, b), vec3(0.5, HB, HB), tBest, cBest, hBest);
                    if (edgeOn(c + ivec3(a, 0, b), 1, k[iy], k[iy + 2])) boxTest(ro, ird, o + vec3(a, 0.5, b), vec3(HB, 0.5, HB), tBest, cBest, hBest);
                    if (edgeOn(c + ivec3(a, b, 0), 2, k[iz], k[iz + 4])) boxTest(ro, ird, o + vec3(a, b, 0.5), vec3(HB, HB, 0.5), tBest, cBest, hBest);
                }
            }
            for (int j = ZERO; j < 8; j++)
                if (nodeOn(k[j])) boxTest(ro, ird, o + vec3(j & 1, (j >> 1) & 1, j >> 2), vec3(HB), tBest, cBest, hBest);
        }

        if (tBest <= tExit || tExit > tOut) break;

        int a = tMax.x < tMax.y && tMax.x < tMax.z ? 0 : tMax.y < tMax.z ? 1 : 2;
        int s = int(stp[a]);
        c[a] += s;
        tMax[a] += tDel[a];
        // the far side's corners become the near side's
        axis = nearCell ? a : -1;
        int bit = 1 << a;
        side = s > 0 ? bit : 0;
        if (nearCell) {
            for (int j = ZERO; j < 8; j++)
                if ((j & bit) != side) k[j] = k[j ^ bit];
        }
    }

    if (tBest > 1e8) return -1.0;

    vec3 q = (ro + rd * tBest - cBest) / hBest;
    vec3 aq = abs(q);
    if (aq.x > aq.y && aq.x > aq.z) nor = vec3(sign(q.x), 0.0, 0.0);
    else if (aq.y > aq.z) nor = vec3(0.0, sign(q.y), 0.0);
    else nor = vec3(0.0, 0.0, sign(q.z));
    return tBest;
}

// Broad soft light: wrapped Lambert with a gentle distance falloff (the
// wrap part is the sky), plus the unshadowed rim light.
vec3 lightAt(vec3 p, vec3 n) {
    vec3 l = lightPos - p;
    float d = length(l);
    l /= d;
    vec3 diff = 0.15 * SKY_COL + 0.85 * KEY_COL * max(dot(n, l), 0.0);
    vec3 c = 2.0 * diff / (1.0 + d * d / 600.0);
#if MOOD == 1
    vec3 r = rimPos - p;
    float dr = length(r);
    c += RIM_COL * 2.0 * 0.85 * max(dot(n, r / dr), 0.0) / (1.0 + dr * dr / 600.0);
#endif
    return c;
}

// p, n and rd in world space, lp is p in lattice space.
vec3 shadeLattice(vec3 p, vec3 n, vec3 rd, vec3 lp) {
    // Crease darkening near the joints: distance along the beam to the
    // nearest joint.
    vec3 d = abs(lp - round(lp));
    float along = max(max(d.x, d.y), d.z);
    float ao = mix(0.55, 1.0, smoothstep(HB, HB + 0.3, along));

    float fade = exp(-FADE * CELL * max(latSdf(lp.xz), 0.0));
    vec3 light = lightAt(p, n);

    vec3 h = normalize(normalize(lightPos - p) - rd);
    float spec = 0.08 * pow(max(dot(n, h), 0.0), 24.0);

    float albedo = 0.5;
    return (albedo * light * ao + spec * light) * fade;
}

// ---- lattice interface (world space) ---------------------------------------
// The floor and render() only talk to the lattice through these functions, so
// its placement, orientation and rule stay private to the lattice code.

// The lattice rests on the floor: its bottom beams sit on y = 0.
const float LAT_LIFT = HB;

// World to lattice space: the floor tile grid scaled to cells, lifted by
// half a beam. Lattice x and z are the tile grid's q.x and q.y.
vec3 toLattice(vec3 p) {
    vec2 q = rot(radians(TILE_ROT)) * (p.xz - vec2(CAM_X, 0.0)) / TILE + TILE_OFF;
    return vec3(q.x / CELL_TILES, p.y / CELL - LAT_LIFT, q.y / CELL_TILES);
}

// Nearest beam hit of a world ray (t > 0, world normal), or -1. The lattice
// ray is not normalised, so t is the same in both spaces.
float latticeHit(vec3 ro, vec3 rd, out vec3 nor) {
    mat2 R = rot(radians(TILE_ROT));
    vec2 dq = R * rd.xz;
    vec3 lrd = vec3(dq.x, rd.y, dq.y) / CELL;
    lrd += 1e-7 * step(abs(lrd), vec3(1e-7));   // keep 1/rd finite
    float t = traceLattice(toLattice(ro), lrd, nor);
    vec2 wn = nor.xz * R;                       // back to world xz
    nor = vec3(wn.x, nor.y, wn.y);
    return t;
}

// Signed horizontal distance of a world point to the lattice volume:
// > 0 inside it (depth below the open faces), < 0 in the open room.
float latSigned(vec3 p) {
    return latSdf(toLattice(p).xz) * CELL;
}

// Colour of a lattice hit at world point p.
vec3 latticeColor(vec3 p, vec3 n, vec3 rd) {
    return shadeLattice(p, n, rd, toLattice(p));
}

// ---- floor ------------------------------------------------------------------
// Honed marble slabs on a grout bed. Each slab is a box whose top is the plane
// y = 0, with top edges rounded to radius BEVEL; neighbours stand GAP apart
// and the grout fills the joints up to SLAB below the top (tile units). A
// camera ray hits the top plane first; near an edge it is sphere-traced on,
// over the rounded edge and into the joint, to the far wall or the grout.
//
// The floor sees the same light as lightAt(): a sphere light of radius
// LIGHT_R at lightPos (the 0.85 Lambert part) plus a uniform sky (the 0.15
// wrap part). Direct light is shadowed by real occlusion: by the slabs (a soft
// sphere-traced shadow) and by the lattice (a shadow ray per AA sample to a
// random point on the light, so the samples integrate the penumbra). The sky
// is occluded by the joint walls and by the lattice.

const float F0 = 0.05;           // calcite, n = 1.6: reflectance at normal incidence
const float GROUT = 0.32;        // grout albedo

// World point/direction -> tile space: x, z across the grid, y up, in tiles.
vec3 toTile(vec3 p) {
    vec2 q = rot(radians(TILE_ROT)) * (p.xz - vec2(CAM_X, 0.0)) / TILE + TILE_OFF;
    return vec3(q.x, p.y / TILE, q.y);
}
vec3 tileDir(vec3 v) { return vec3(rot(radians(TILE_ROT)) * v.xz, v.y).xzy; }
vec3 worldDir(vec3 v) { vec2 h = v.xz * rot(radians(TILE_ROT)); return vec3(h.x, v.y, h.y); }

// Distance (tiles) to the slabs and the grout bed. Every slab is centred in
// its cell, so the slab of the cell containing s is always the nearest one.
float slabDist(vec3 s) {
    vec3 c = vec3(fract(s.x) - 0.5, s.y + 0.5, fract(s.z) - 0.5);
    vec3 d = abs(c) - (vec3(0.5 - 0.5 * GAP, 0.5, 0.5 - 0.5 * GAP) - BEVEL);
    float slab = length(max(d, 0.0)) + min(max(d.x, max(d.y, d.z)), 0.0) - BEVEL;
    return min(slab, s.y + SLAB);
}

vec3 slabNormal(vec3 s) {
    vec3 c = vec3(fract(s.x) - 0.5, s.y + 0.5, fract(s.z) - 0.5);
    vec3 d = abs(c) - (vec3(0.5 - 0.5 * GAP, 0.5, 0.5 - 0.5 * GAP) - BEVEL);
    float slab = length(max(d, 0.0)) + min(max(d.x, max(d.y, d.z)), 0.0) - BEVEL;
    if (s.y + SLAB < slab) return vec3(0.0, 1.0, 0.0);
    return normalize(max(d, 1e-5 * step(d.yzx, d) * step(d.zxy, d)) * sign(c));
}

// Sphere-trace the slabs from the top-plane hit t; eps is the cone half-angle.
float traceSlabs(vec3 ro, vec3 rd, float t, float eps) {
    for (int i = ZERO; i < 48; i++) {
        float d = slabDist(toTile(ro + rd * t)) * TILE;
        if (d < eps * t) break;
        t += d;
    }
    return t;
}

// Soft shadow of the slabs towards the light (tile space). Nothing rises
// above the top plane, so the march ends there; k = 1 / tan(light radius).
float slabShadow(vec3 s, vec3 l, float k) {
    float res = 1.0, t = 0.003;
    for (int i = ZERO; i < 24; i++) {
        if (s.y + l.y * t > 0.0) break;
        float h = slabDist(s + l * t);
        res = min(res, k * h / t);
        if (res < 0.01) return 0.0;
        t += clamp(h, 0.003, 0.05);
    }
    return res * res * (3.0 - 2.0 * res);
}

// Cosine-weighted sky visibility (view factor) of a point in the slab
// geometry. Below the rounded edges the joint is a 2D slot GAP wide: by the
// crossed-strings rule a wall point at depth z sees 0.5 (1 - z / hypot(z, GAP))
// of it, a grout point the two edges' sines. Above, the tilted bevel loses
// the part of its hemisphere below the horizon: (1 + n.y) / 2.
float slabSky(vec3 s, vec3 n) {
    float z = -s.y - BEVEL;
    if (z <= 0.0) return 0.5 + 0.5 * n.y;
    if (n.y > 0.5) {
        vec2 f = abs(s.xz - round(s.xz));
        float a = 0.5 * GAP - min(f.x, f.y), b = GAP - a;
        z = SLAB - BEVEL;
        return 0.5 * (a / length(vec2(a, z)) + b / length(vec2(b, z)));
    }
    return 0.5 * (1.0 - z / length(vec2(z, GAP)));
}

// Sky visibility near the lattice. It is open, so it blocks the sky like a
// solid wall about LAT_SKY_H high: a floor point x in front of such a wall
// sees 1 - 0.5 (1 - x / hypot(x, h)) of the sky. Inside, the sky dies with
// depth at the rate the lattice's own light fades.
const float LAT_SKY_H = 2.0;
float latticeSky(vec3 p) {
    float sd = latSigned(p);
    if (sd > 0.0) return 0.5 * exp(-FADE * sd);
    return 0.5 + 0.5 * -sd / length(vec2(sd, LAT_SKY_H));
}

// Lattice shadow: one ray between p and a random point (xi) on the sphere
// light. It is cast from the light down to p, so the traversal ends just
// below the floor instead of running on through the lattice behind the light.
float latticeShadow(vec3 p, vec2 xi) {
    vec3 w = normalize(lightPos - p);
    vec3 u = normalize(cross(w, vec3(0.0, 0.0, 1.0)));
    vec3 v = cross(w, u);
    float a = TAU * xi.y;
    vec3 q = lightPos + LIGHT_R * sqrt(xi.x) * (u * cos(a) + v * sin(a));
    vec3 d = p - q;
    float dist = length(d);
    vec3 nor;
    float t = latticeHit(q, d / dist, nor);
    return t > 0.0 && t < dist ? 0.0 : 1.0;
}

// Marble albedo at tile-space point s. Every slab is cut from a different
// part of the block: its pattern gets its own rotation and offset. Veins
// follow an iso-line of domain-warped fbm, stretched along the bedding
// direction: a sharp core inside a soft halo, on a faintly clouded ground.
// Where the veins get narrower than the footprint fw (tiles) they fade to
// their mean cover, so far slabs keep the same average tone.
float marble(vec3 s, float fw) {
    vec2 id = floor(s.xz);
    vec2 h = hash22(id + 17.3);
    vec2 uv = rot(TAU * h.x) * (s.xz - id - 0.5) * vec2(0.7, 2.2) + 9.0 * h;
    float tone = ALBEDO * (1.0 + 0.05 * (hash12(id) - 0.5));
    float detail = 1.0 - smoothstep(0.01, 0.05, fw);
    float vein = 0.1, cloud = 0.5;                      // mean cover
    if (detail > 0.0) {
        vec2 w = vec2(fbm(uv, 4), fbm(uv + vec2(5.2, 1.3), 4));
        float e = abs(fbm(uv + 1.1 * w, 4) - 0.5);
        float v = 0.35 * exp(-e / 0.05) + 0.65 * exp(-e / 0.008);
        vein = mix(vein, v, detail);
        cloud = mix(cloud, w.y, detail);
    }
    return tone * (1.0 - VEIN * vein) * (0.95 + 0.1 * cloud);
}

// GGX specular BRDF of the sphere light at distance dist; its size widens
// the lobe (Karis).
float ggxLight(vec3 n, vec3 v, vec3 l, float dist, float rough) {
    float nl = max(dot(n, l), 0.0);
    float nv = max(dot(n, v), 1e-3);
    vec3 hv = normalize(l + v);
    float nh = max(dot(n, hv), 0.0);
    float a = rough * rough;
    float a2 = a * a;
    float aw = min(a + LIGHT_R / (2.0 * dist), 1.0);
    float dd = nh * nh * (a2 - 1.0) + 1.0;
    float D = a2 / (PI * dd * dd) * (a / aw) * (a / aw);
    float k = 0.5 * a;
    float G = nl / (nl * (1.0 - k) + k) * nv / (nv * (1.0 - k) + k);
    float F = F0 + (1.0 - F0) * pow(1.0 - max(dot(v, hv), 0.0), 5.0);
    return D * G * F / (4.0 * max(nl, 1e-3) * nv);
}

#if MOOD == 1
// Diffuse (albedo diff) and GGX light of the rim light at p, shadowed by
// the ball only (cube skip left out).
vec3 rimShade(vec3 p, vec3 n, vec3 v, float diff, float rough, int skip) {
    vec3 R = rimPos - p;
    float dist = length(R);
    vec3 l = R / dist;
    float nl = max(dot(n, l), 0.0);
    if (nl <= 0.0) return vec3(0.0);
    float I = 2.0 / (1.0 + dist * dist / 600.0);
    float vis = sphereLightVis(p + n * 1e-3, rimPos, RIM_R, skip, 1);
    return RIM_COL * (0.85 * I * nl * vis) * (diff + PI * ggxLight(n, v, l, dist, rough));
}
#endif

// Fresnel towards a uniform environment for a rough surface (Lagarde).
float envFresnel(float nv, float rough) {
    return F0 + (max(1.0 - rough, F0) - F0) * pow(1.0 - nv, 5.0);
}

// cone: angular size of the sample footprint
vec3 shadeFloor(vec3 ro, vec3 rd, float t0, float cone, out float tHit) {
    tHit = traceSlabs(ro, rd, t0, 0.5 * cone);
    vec3 p = ro + rd * tHit;
    vec3 s = toTile(p);
    vec3 ns = slabNormal(s);
    vec3 n = worldDir(ns);
    vec3 v = -rd;

    // footprint on the floor, stretched along the view direction
    float fw = cone * tHit / TILE / max(-rd.y, 0.02);
    float albedo = s.y < -SLAB + 1e-3 ? GROUT : marble(s, fw);

    vec3 L = lightPos - p;
    float dist = length(L);
    vec3 l = L / dist;
    float I = 2.0 / (1.0 + dist * dist / 600.0);     // as in lightAt()
    float nl = max(dot(n, l), 0.0);

    float shadow = 0.0;
    if (nl > 0.0) {
        shadow = slabShadow(s + ns * 0.002, tileDir(l), dist / LIGHT_R);
        vec2 xi = hash22(floor(rd.xy / cone * 2.0));   // per AA sample: seeded by the ray
        if (shadow > 0.0) shadow *= latticeShadow(p + n * 1e-3, xi);
        if (shadow > 0.0) shadow *= sphereLightVis(p + n * 1e-3, lightPos, LIGHT_R, -1, 0);
    }
    float sky = slabSky(s, ns) * latticeSky(p) * sphereSky(p);

    float spec = ggxLight(n, v, l, dist, ROUGH);
    // rough Fresnel towards the sky; the diffuse gets the rest
    float Fs = envFresnel(max(dot(n, v), 1e-3), ROUGH);
    float direct = 0.85 * I * nl * shadow;
    float ambient = 0.15 * I * sky;
    vec3 col = albedo * (1.0 - Fs) * (direct * KEY_COL + ambient * SKY_COL) + PI * spec * direct * KEY_COL + Fs * ambient * SKY_COL;
#if MOOD == 1
    col += rimShade(p, n, v, albedo * (1.0 - Fs), ROUGH, -1);
#endif
    return col;
}

// ---- cube ball --------------------------------------------------------------
// The cubes see the sphere light (shadowed by the other cubes), the sky above
// and the lit floor below, and mirror the two in their Fresnel reflection.
// Inside the shell the cubes close in: a face looking into the ball sees its
// dim hollow.

// Radiance of the floor (in units of the ambient terms) at the floor point
// q: its marble under the sky and the light, which the ball, taken as a
// porous sphere resting on the floor, partly hides.
vec3 floorGlow(vec3 q) {
    vec3 L = lightPos - q, B = sphO - q;
    float D = length(L), dB = length(B);
    float a = asin(min(LIGHT_R / D, 1.0)), b = asin(min(sphRad / dB, 1.0));
    float sep = acos(clamp(dot(L, B) / (D * dB), -1.0, 1.0));
    float cover = min(b * b / (a * a), 1.0) * (1.0 - smoothstep(abs(a - b), a + b, sep));
    float I = 2.0 / (1.0 + D * D / 600.0);
    vec3 c = ALBEDO * I * (0.85 * max(L.y / D, 0.0) * (1.0 - SPH_SOLID * cover) * KEY_COL + 0.15 * SKY_COL);
#if MOOD == 1
    vec3 R = rimPos - q;
    float dr = length(R);
    c += ALBEDO * RIM_COL * 2.0 * 0.85 * max(R.y / dr, 0.0) / (1.0 + dr * dr / 600.0);
#endif
    return c;
}

// Sky occlusion of a cube face by its neighbours (sphereOcclusion), once per
// pixel and face: a camera sample may pass through cubes, so the last few
// faces are kept.
#define AO_CACHE 4
int aoKey[AO_CACHE] = int[AO_CACHE](-1, -1, -1, -1);
float aoVal[AO_CACHE];
int aoNext = 0;
float cubeOpen(vec3 p, vec3 n, int id, int face) {
    int key = id * 8 + face;
    for (int k = ZERO; k < AO_CACHE; k++)
        if (aoKey[k] == key) return aoVal[k];
    float v = clamp(1.0 - sphereOcclusion(p, n, id), 0.0, 1.0);
    aoKey[aoNext] = key;
    aoVal[aoNext] = v;
    aoNext = (aoNext + 1) % AO_CACHE;
    return v;
}

// Ambient light of a cube face (sky above, floor below, where the face looks:
// the floor patch about as far out as the face is high) and the environment
// its mirror image shows. hollow: how dim the inside of the ball is.
void cubeAmbient(vec3 p, vec3 n, vec3 rd, int id, int face, float hollow, out vec3 ambient, out vec3 env) {
    float I = 2.0 / (1.0 + dot(lightPos - p, lightPos - p) / 600.0);
    float open = cubeOpen(p, n, id, face) * mix(hollow, 1.0, 0.5 + 0.5 * dot(n, normalize(p - sphO)));
    float up = 0.5 + 0.5 * n.y;
    vec3 glow = floorGlow(vec3(p.x + n.x * p.y, 0.0, p.z + n.z * p.y));
    ambient = (0.15 * I * up * SKY_COL + glow * (1.0 - up)) * open;
    vec3 r = reflect(rd, n);
    env = (r.y > 0.0 ? 0.15 * I * SKY_COL : floorGlow(vec3(p.x + r.x * p.y / max(-r.y, 0.05), 0.0, p.z + r.z * p.y / max(-r.y, 0.05)))) * open;
}

#if GLASS
// Milky glass: a smooth glass surface over a body that scatters the light.
// Light taken in anywhere leaves the cube diffused, so a face in shadow still
// glows, most near the lit faces and towards the eye when the light stands
// behind the cube. The milk replaces the view through the cube by
// 1 - exp(-GLASS_DENSITY T), T the path through the cube along the refracted
// ray, so the thin rounded edges stay clearer. Every cube is tinted in one
// of the palette colours, which colours its glow and the view through it.
const vec3 MILK_EXT = vec3(0.88, 1.0, 1.15);       // relative extinction

// Glass colour of cube id: a palette colour, GLASS_TINT saturated.
vec3 cubeTint(int id) {
    vec3 c = PALETTE[int(3.0 * hash12(vec2(float(id), 41.7)))];
    return mix(vec3(1.0), c / max(c.r, max(c.g, c.b)), GLASS_TINT);
}

// One sphere light (centre lc, radius lr, cache slot, colour) on a glass
// cube: the glow it gives the milk and its highlight on the surface.
void glassLight(vec3 p, vec3 n, vec3 rd, int id, vec3 lc, float lr, int slot, vec3 col, inout vec3 glow, inout vec3 spec) {
    vec3 L = lc - p;
    float dist = length(L);
    vec3 l = L / dist;
    float I = 2.0 / (1.0 + dist * dist / 600.0);     // as in lightAt()
    vec3 E = 0.85 * I * sphereLightVis(p + n * 1e-3, lc, lr, id, slot) * col;
    float nl = max(dot(n, l), 0.0);
    glow += E * (0.15 + 0.4 * nl + 0.6 * pow(max(dot(rd, l), 0.0), 3.0));
    spec += E * nl * PI * ggxLight(n, -rd, l, dist, CUBE_ROUGH);
}

// lp: hit point in cube space, id: the cube. pass: the share of what lies
// behind the cube that the view through it keeps.
vec3 shadeCube(vec3 p, vec3 n, vec3 rd, int id, vec3 lp, float cone, float t, out vec3 pass) {
    vec3 a = abs(lp);
    int ax = a.x > a.y && a.x > a.z ? 0 : a.y > a.z ? 1 : 2;
    int face = 2 * ax + int(lp[ax] > 0.0);

    // path through the cube along the refracted ray (cube space)
    vec3 c;
    mat3 m;
    cubePose(id, c, m);
    vec3 d = refract(ballDir(rd) * m, roundCubeNormal(lp), 1.0 / 1.5);
    d += 1e-7 * step(abs(d), vec3(1e-7));
    vec3 tf = (0.5 * sign(d) - lp) / d;
    float T = max(min(min(tf.x, tf.y), tf.z), 0.0);
    vec3 milk = 1.0 - exp(-GLASS_DENSITY * T * MILK_EXT);

    vec3 glow = vec3(0.0), spec = vec3(0.0);
    glassLight(p, n, rd, id, lightPos, LIGHT_R, 0, KEY_COL, glow, spec);
#if MOOD == 1
    glassLight(p, n, rd, id, rimPos, RIM_R, 1, RIM_COL, glow, spec);
#endif
    // light comes into the hollow through the cubes
    vec3 ambient, env;
    cubeAmbient(p, n, rd, id, face, 0.5, ambient, env);

    vec3 tint = cubeTint(id);
    float Fs = envFresnel(max(dot(n, -rd), 1e-3), CUBE_ROUGH);
    pass = (1.0 - Fs) * (1.0 - milk) * tint;
    return (1.0 - Fs) * milk * 0.92 * tint * (glow + ambient) + spec + Fs * env;
}
#else
// Polished marble: every face is cut from its own part of the block, at the
// floor's physical scale, under a tight GGX lobe.
// lp: hit point in cube space, id: the cube. pass: nothing shows through.
vec3 shadeCube(vec3 p, vec3 n, vec3 rd, int id, vec3 lp, float cone, float t, out vec3 pass) {
    pass = vec3(0.0);
    vec3 a = abs(lp);
    int ax = a.x > a.y && a.x > a.z ? 0 : a.y > a.z ? 1 : 2;
    vec2 uv = ax == 0 ? lp.yz : ax == 1 ? lp.zx : lp.xy;
    int face = 2 * ax + int(lp[ax] > 0.0);
    vec2 cell = vec2(float(id * 6 + face), 517.0);
    vec2 q = cell + 0.5 + uv * (SPH_SCALE / TILE);
    float albedo = marble(vec3(q.x, 0.0, q.y), cone * t / TILE) * (CUBE_ALBEDO / ALBEDO);

    vec3 v = -rd;
    vec3 L = lightPos - p;
    float dist = length(L);
    vec3 l = L / dist;
    float I = 2.0 / (1.0 + dist * dist / 600.0);     // as in lightAt()
    float nl = max(dot(n, l), 0.0);
    float shadow = nl > 0.0 ? sphereLightVis(p + n * 1e-3, lightPos, LIGHT_R, id, 0) : 0.0;

    vec3 ambient, env;
    cubeAmbient(p, n, rd, id, face, 0.2, ambient, env);
    float Fs = envFresnel(max(dot(n, v), 1e-3), CUBE_ROUGH);
    float direct = 0.85 * I * nl * shadow;
    float spec = ggxLight(n, v, l, dist, CUBE_ROUGH);
    vec3 col = albedo * (1.0 - Fs) * (direct * KEY_COL + ambient) + PI * spec * direct * KEY_COL + Fs * env;
#if MOOD == 1
    col += rimShade(p, n, v, albedo * (1.0 - Fs), CUBE_ROUGH, id);
#endif
    return col;
}
#endif

// The camera of the frame, set by cameraPrepare(): position, optical axis,
// the image axes (rolled) and the focus distance (eye-space depth).
vec3 camRo, camFwd, camRight, camUp;
float camFocus;

// Over SCENE_LOOP the camera goes once round an upright ellipse, CAM_DX
// wide either way and CAM_DY high, from its lowest point, the fitted camera:
// right on the low arc, back left on the high one. It turns with it so that
// the resting ball stays where it is in the picture, and it focuses on the
// ball's centre. Needs spherePrepare().
void cameraPrepare() {
    float a = TAU * iTime / SCENE_LOOP;
    vec3 ro0 = vec3(CAM_X, CAM_H, 0.0);
    camRo = ro0 + vec3(CAM_DX * sin(a), 0.5 * CAM_DY * (1.0 - cos(a)), 0.0);
    // turn by the change of the direction to the resting ball
    vec3 target = vec3(SPH_POS.x, sphRad, SPH_POS.y);
    vec3 d0 = target - ro0, d = target - camRo;
    float yaw = atan(d.x, d.z) - atan(d0.x, d0.z);
    float pa = radians(PITCH) + atan(d.y, length(d.xz)) - atan(d0.y, length(d0.xz));
    float ra = radians(ROLL);
    vec3 fwd = vec3(0.0, sin(pa), cos(pa));
    vec3 right = vec3(1.0, 0.0, 0.0);
    vec3 up = cross(fwd, right);
    vec2 cy = vec2(cos(yaw), sin(yaw));
    camFwd = vec3(fwd.x * cy.x + fwd.z * cy.y, fwd.y, fwd.z * cy.x - fwd.x * cy.y);
    up = vec3(up.x * cy.x + up.z * cy.y, up.y, up.z * cy.x - up.x * cy.y);
    right = vec3(cy.x, 0.0, -cy.y);
    camRight = right * cos(ra) + up * sin(ra);
    camUp = up * cos(ra) - right * sin(ra);
    camFocus = dot(sphO - camRo, camFwd);
}

// Camera ray through a pixel position; fwd is the optical axis.
void cameraRay(vec2 fragCoord, out vec3 ro, out vec3 rd, out vec3 fwd) {
    vec2 uv = (2.0 * fragCoord - iResolution.xy) / iResolution.y - vec2(SHIFT_X, SHIFT_Y);
    ro = camRo;
    fwd = camFwd;
    rd = normalize(uv.x * camRight + uv.y * camUp + FOCAL * fwd);
    rd += 1e-6 * step(abs(rd), vec3(1e-6)); // keep 1/rd finite
}

// Whether the last render() call hit the ball first.
bool hitBall;

// The first lattice trace of a pixel the ball covers in part: further camera
// samples there that slip between or through the cubes reuse it. The lattice behind
// the ball is far out of focus, which hides the aliasing, as for the
// single-sampled blurred lattice pixels in mainImage().
bool latCached = false;
float latCacheT;     // ray distance of the cached hit, 1e9 if it missed
vec3 latCacheCol;

// Colour and eye-space depth for one camera ray; spp is the number of samples
// per pixel axis, which sets the footprint each sample filters over.
// (mainImage() hands the depth on in units of the focus distance.) The ray
// passes on through glass cubes, unbent: the milk hides the small shift.
vec4 render(vec2 fragCoord, float spp) {
    vec3 ro, rd, fwd;
    cameraRay(fragCoord, ro, rd, fwd);
    float cone = 2.0 / (iResolution.y * FOCAL * spp);

    vec3 nor, lp;
    int id = -1;
    // (the floor further out than a few thousand units is lost behind the
    // lattice, and its shading breaks down there)
    float tF = rd.y < -1e-3 ? -ro.y / rd.y : 1e9;

    // The ball stands in front of the lattice: once nothing shows through the
    // cubes, the ray needs no lattice trace.
    vec3 col = vec3(0.0), pass = vec3(1.0);
    float depth = -1.0, tS = 0.0;
    hitBall = false;
    for (int k = ZERO; k < GLASS_LAYERS; k++) {
        tS = sphereHit(ro, rd, id, tS, nor, id, lp);
        if (tS < 0.0 || tS >= tF) break;
        if (k == 0) {
            hitBall = true;
            depth = tS * dot(rd, fwd);
        }
        vec3 through;
        col += pass * shadeCube(ro + rd * tS, nor, rd, id, lp, cone, tS, through);
        pass *= through;
        if (max(pass.r, max(pass.g, pass.b)) < 0.01) return vec4(col, depth);
    }

    float tL;
    vec3 bg = vec3(0.0);
    if (latCached) {
        tL = latCacheT;
        bg = latCacheCol;
    } else {
        tL = latticeHit(ro, rd, nor);
        if (tL < 0.0) tL = 1e9;
        else bg = latticeColor(ro + rd * tL, nor, rd);
        if (sphNCand > 0) {
            latCached = true;
            latCacheT = tL;
            latCacheCol = bg;
        }
    }
    float t = 1e3;
    if (tL < tF) t = tL;
    else if (tF < 1e9) bg = shadeFloor(ro, rd, tF, cone, t);
    return vec4(col + pass * bg, hitBall ? depth : t * dot(rd, fwd));
}

void mainImage(out vec4 fragColor, in vec2 fragCoord) {
    spherePrepare();
    cameraPrepare();
    lightsPrepare();
    {
        // the cubes the camera samples of this pixel can meet (they stay
        // within 0.71 pixel of the centre ray)
        vec3 ro, rd, fwd;
        cameraRay(fragCoord, ro, rd, fwd);
        sphereCandidates(ro, rd, 1.5 / (iResolution.y * FOCAL));
    }
#ifdef SPH_DEBUG
    // how full the candidate list is: green to red, white where it overflows
    float fill = float(sphNCand) / float(SPH_CAND);
    fragColor = vec4(fill >= 1.0 ? vec3(1.0) : vec3(fill, 1.0 - fill, 0.0) * step(0.5, float(sphNCand)), 1.0);
    return;
#endif

    // One call site of render(), so that the compiler inlines it only once.
    vec3 col = vec3(0.0);
    float depth = 0.0;
    int n = 0;
    for (int k = ZERO; k < AA * AA; k++) {
        vec4 s = render(fragCoord + (vec2(k % AA, k / AA) + 0.5) / float(AA) - 0.5, float(AA));
        col += s.rgb;
        n++;
        if (k > 0) continue;

        // The first sample also gives the one depth per pixel the DOF pass
        // needs. A blurred lattice pixel needs no more samples: its beams are
        // wide and smooth, and the bokeh hides the aliasing of their edges.
        // The floor keeps all samples even when blurred, because its far
        // joints are finer than a pixel and would alias into moire that no
        // blur removes.
        vec3 ro, rd, fwd;
        cameraRay(fragCoord, ro, rd, fwd);
        float zFloor = rd.y < -1e-3 ? -ro.y / rd.y * dot(rd, fwd) : 1e3;
        bool lattice = !hitBall && (s.a < 0.99 * zFloor || zFloor >= 1e3);
        depth = s.a / camFocus;
        if (lattice && cocRadius(depth, iResolution.y / 1254.0) > AA_COC) break;
    }
    fragColor = vec4(col / float(n), depth);
}
