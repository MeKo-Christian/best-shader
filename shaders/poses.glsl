// poses.glsl - Buffer A: the pose of every cube of the ball, evaluated once
// per frame (sphere_pose.glsl), for the scene pass to read with texelFetch.
// Only a few texels in the bottom rows are used:
//   row 0, column i < 6 SPH_FACE^2:  centre of cube i (ball space, cube edges)
//   row 1, 2:                         its x and y axes (z = cross(x, y))
//   row 3, column k < 54:             bounding sphere of cluster k (centre, radius)
//   row 4, column 0:                  height of the ball centre above the floor
//                                     at rest (squashed, if on the floor) and
//                                     with the bounce (world)
// A cluster is a block of cells of a cube face, 3 x 3 blocks per face; the
// scene tests a cluster's sphere before its cubes.

#include "common.glsl"
#include "sphere_pose.glsl"

const float RHO = 0.8660254;  // circumradius of a cube

// First cell row/column of block b of a face.
int blockStart(int b) {
    return b == 0 ? 0 : b == 1 ? (SPH_FACE + 1) / 3 : b == 2 ? (2 * SPH_FACE + 3) / 3 : SPH_FACE;
}

void mainImage(out vec4 fragColor, in vec2 fragCoord) {
    ivec2 p = ivec2(fragCoord);
    fragColor = vec4(0.0);
    const int n = 6 * SPH_FACE * SPH_FACE;
    if (p.y > 4 || p.x >= n) return;
    sphereTime();
    // only the bottom face can reach the floor
    if (p.y < 3 ? p.x / (SPH_FACE * SPH_FACE) == 3 : p.y == 3 ? p.x / 9 == 3 : true) sphereFloor();
    vec3 c;
    mat3 m;

    if (p.y < 3) {
        cubeFormula(p.x, c, m);
        fragColor = vec4(p.y == 0 ? c : m[p.y - 1], 0.0);
    } else if (p.y == 3 && p.x < 54) {
        int f = p.x / 9, bj = (p.x / 3) % 3, bi = p.x % 3;
        vec3 sum = vec3(0.0), lo = vec3(1e9), hi = vec3(-1e9);
        for (int j = blockStart(bj); j < blockStart(bj + 1); j++) {
            for (int i = blockStart(bi); i < blockStart(bi + 1); i++) {
                cubeFormula((f * SPH_FACE + j) * SPH_FACE + i, c, m);
                lo = min(lo, c);
                hi = max(hi, c);
            }
        }
        // the middle of the box around the centres, then the farthest centre
        vec3 mid = 0.5 * (lo + hi);
        float r = 0.0;
        for (int j = blockStart(bj); j < blockStart(bj + 1); j++) {
            for (int i = blockStart(bi); i < blockStart(bi + 1); i++) {
                cubeFormula((f * SPH_FACE + j) * SPH_FACE + i, c, m);
                r = max(r, length(c - mid));
            }
        }
        fragColor = vec4(mid, r + RHO);
    } else if (p.y == 4 && p.x == 0) {
        // on the floor the squashed ball rests on its lowest point; in the
        // air its centre of mass follows the arc (the swing of the shape
        // never takes the bottom below the floor)
        float rest = sphereLowest() * SPH_SCALE;
        fragColor = vec4(rest, rest + max(sphAir, 0.0), 0.0, 0.0);
    }
}
