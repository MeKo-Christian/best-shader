// lattice.glsl - where the cubic beam lattice is and which beams exist.
//
// Everything here is in lattice space: one unit is one cell, joints sit on
// integer coordinates and y points up. scene.glsl maps world rays into this
// space, rotated with the floor tiles, so the joints stand on tile joints.
//
// Region. The lattice fills the space behind its open faces, the planes
// x = LAT_XC and z = LAT_ZF. Horizontally it is unbounded; the tracer stops
// once the depth fade has turned it black. The camera looks along about
// (-0.73, 0, 0.68), i.e. 45 degrees between the -x and the +z axis.
//
// Beam rule. No beam may end in mid-air: wherever a beam ends, another beam
// leaves the same joint along another axis, or the floor carries it.
//   - Whole grid lines are missing at random. Lines lying in an open face are
//     always kept, so the faces have no line ends.
//   - Every joint keeps some of its half-edges (the beams leaving it): with
//     JOINT=0 both halves of one axis whose line passes straight through, so
//     every end is a T-junction or a cross; with JOINT=1 one half on each of
//     two axes, so ends may also be L-corners. Kept half-edges are never cut.
//   - Any other segment is cut at random. Each cut end thus lands on a joint
//     that keeps a beam on another axis, and the rule treats x, y and z alike.
//   In the faces, JOINT=0 only allows straight, one cell wide openings;
//   JOINT=1 also gives the L-shaped and stepped openings of the reference.
//
// Defines:
//   -D LAT_VIEW=0     0: inside corner, the lattice fills x < LAT_XC or
//                        z > LAT_ZF and we look into the corner
//                     1: outside corner, the lattice fills x < LAT_XC and
//                        z > LAT_ZF, a block with its edge towards us
//                     2: single face, the lattice fills x < LAT_XC, a wall
//                        running diagonally through the frame
//   -D LAT_XC, LAT_ZF open faces in cells (defaults depend on LAT_VIEW)
//   -D JOINT=1        0: cuts end only at T-junctions or crosses,
//                     1: cuts may also end at L-corners
//   -D LINE_P=0.1     probability that a whole grid line is missing
//   -D SEG_P=0.7      probability that a segment which may go is missing
//   -D KEEP_Y=0.4     probability that a joint keeps a vertical beam first;
//                     x and z share the rest equally
//   -D KEEP_BLOCK=1.0 kept half-edges are chosen per block of this many joints
//   -D SEED=0         shifts the rule pattern

#ifndef LAT_VIEW
#define LAT_VIEW 0
#endif
#ifndef SEED
#define SEED 0
#endif
#ifndef LINE_P
#define LINE_P 0.1
#endif
#ifndef SEG_P
#define SEG_P 0.7
#endif
#ifndef KEEP_Y
#define KEEP_Y 0.4
#endif
#ifndef JOINT
#define JOINT 1
#endif
#ifndef KEEP_BLOCK
#define KEEP_BLOCK 1.0
#endif

#if LAT_VIEW == 0
#define LAT_XC_DEF -15
#define LAT_ZF_DEF 13
#elif LAT_VIEW == 1
#define LAT_XC_DEF -11
#define LAT_ZF_DEF 10
#else
#define LAT_XC_DEF -14
#define LAT_ZF_DEF 0
#endif
#ifndef LAT_XC
#define LAT_XC LAT_XC_DEF
#endif
#ifndef LAT_ZF
#define LAT_ZF LAT_ZF_DEF
#endif
#define LAT_YMAX 40

// Signed horizontal distance of the point p (lattice xz) to the region:
// > 0 inside, the depth below the nearest open face.
float latSdf(vec2 p) {
    vec2 d = vec2(float(LAT_XC) - p.x, p.y - float(LAT_ZF));
#if LAT_VIEW == 0
    return max(d.x, d.y) > 0.0 ? length(max(d, 0.0)) : max(d.x, d.y);
#elif LAT_VIEW == 1
    return min(d.x, d.y) > 0.0 ? min(d.x, d.y) : -length(max(-d, 0.0));
#else
    return d.x;
#endif
}

// Cells span c .. c + 1; the faces lie on integer planes.
bool inRegion(ivec3 c) {
    return c.y >= 0 && c.y < LAT_YMAX && latSdf(vec2(c.xz) + 0.5) > 0.0;
}

// Edges are addressed by their lower joint n and axis (0 = x, 1 = y, 2 = z).
ivec3 axisVec(int axis) { return ivec3(axis == 0, axis == 1, axis == 2); }

// The four cells around the edge; an edge exists only next to the region.
bool edgeInRegion(ivec3 n, int axis) {
    ivec3 u = axisVec((axis + 1) % 3), v = axisVec((axis + 2) % 3);
    return inRegion(n) || inRegion(n - u) || inRegion(n - v) || inRegion(n - u - v);
}

// Whether the line along axis lies in an open face.
bool inFace(ivec3 n, int axis) {
#if LAT_VIEW == 0
    bool fx = n.x == LAT_XC && (axis == 2 || (axis == 1 && n.z <= LAT_ZF));
    bool fz = n.z == LAT_ZF && (axis == 0 || (axis == 1 && n.x >= LAT_XC));
#elif LAT_VIEW == 1
    bool fx = n.x == LAT_XC && axis != 0;
    bool fz = n.z == LAT_ZF && axis != 2;
#else
    bool fx = n.x == LAT_XC && axis != 0;
    bool fz = false;
#endif
    return fx || fz;
}

// A whole line, identified by its axis and the joint coordinates across it.
bool lineAlive(ivec3 n, int axis) {
    if (inFace(n, axis)) return true;
    vec2 q = axis == 0 ? vec2(n.yz) : axis == 1 ? vec2(n.xz) : vec2(n.xy);
    return hash12(q + vec2(31.7 * float(axis) + 7.3 * float(SEED), 3.9 * float(axis) + 0.5)) >= LINE_P;
}

// The half-edges of joint n that exist, as bits 2 * axis (towards +) and
// 2 * axis + 1 (towards -). Joints a cell deep inside need no region tests.
int halfEdges(ivec3 n) {
    bool deep = n.y > 0 && n.y < LAT_YMAX && latSdf(vec2(n.xz)) >= 1.0;
    int m = 0;
    for (int a = 0; a < 3; a++) {
        if (!lineAlive(n, a)) continue;
        if (deep || edgeInRegion(n, a)) m |= 1 << (2 * a);
        if (deep || edgeInRegion(n - axisVec(a), a)) m |= 2 << (2 * a);
    }
    return m;
}

// The state of joint n: bits 0-5 the kept half-edges (never cut), bits 6-11
// the existing half-edges (see halfEdges). No kept bits: no beam reaches n.
int joint(ivec3 n) {
    int e = halfEdges(n);
    vec3 h = hash33(floor(vec3(n) / KEEP_BLOCK) + vec3(5.3 + float(SEED), 1.9, 7.7));
    int a = h.x < KEEP_Y ? 1 : h.y < 0.5 ? 0 : 2;
    int k = 0;
#if JOINT == 0
    // one axis that passes straight through; the floor continues the
    // vertical line at the bottom joints
    int p = n.y == 0 ? e | ((e & 4) << 1) : e;
    for (int i = 0; i < 3 && k == 0; i++) {
        if (((p >> (2 * a)) & 3) == 3) k = 3 << (2 * a);
        else a = (a + 1) % 3;
    }
#else
    // one half-edge on a preferred axis, then one on another axis
    int s = int(h.z < 0.5), first = -1;
    for (int i = 0; i < 3 && first < 0; i++) {
        if (((e >> (2 * a + s)) & 1) != 0) first = 2 * a + s;
        else if (((e >> (2 * a + 1 - s)) & 1) != 0) first = 2 * a + 1 - s;
        else a = (a + 1) % 3;
    }
    if (first >= 0) {
        float h2 = fract(h.z * 13.7);
        int b = (a + 1 + int(h2 < 0.5)) % 3, s2 = int(fract(h2 * 7.3) < 0.5);
        for (int i = 0; i < 2 && k == 0; i++) {
            if (((e >> (2 * b + s2)) & 1) != 0) k = (1 << first) | (1 << (2 * b + s2));
            else if (((e >> (2 * b + 1 - s2)) & 1) != 0) k = (1 << first) | (1 << (2 * b + 1 - s2));
            else b = 3 - a - b;
        }
        if (k == 0) k = 3 << (2 * a); // only one axis reaches n: keep it whole
    }
#endif
    return k | (e << 6);
}

// The edge from joint n along axis, given the states j0 of n and j1 of the
// joint at its other end.
bool edgeOn(ivec3 n, int axis, int j0, int j1) {
    if (((j0 >> (6 + 2 * axis)) & 1) == 0) return false;
    if (((j0 >> (2 * axis)) & 1) != 0 || ((j1 >> (2 * axis + 1)) & 1) != 0) return true;
    return hash33(vec3(n) + vec3(0.37 * float(axis) + float(SEED), 3.1, 5.7)).x >= SEG_P;
}

// A joint exists exactly where it keeps a beam.
bool nodeOn(int j) { return (j & 63) != 0; }
