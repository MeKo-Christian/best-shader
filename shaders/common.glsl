// common.glsl - shared constants and helpers.
//
// Colour convention: all shading happens in linear RGB. main.glsl encodes to
// sRGB at the very end, so reference hex colours have to go through
// hexColor()/srgbToLinear() before they are used.

#define PI  3.14159265359
#define TAU 6.28318530718

// ---- math -------------------------------------------------------------------
mat2 rot(float a) {
    float c = cos(a), s = sin(a);
    return mat2(c, s, -s, c);
}

// ---- hashes (Dave Hoskins, "Hash without Sine", MIT) ------------------------
float hash11(float p) {
    p = fract(p * 0.1031);
    p *= p + 33.33;
    p *= p + p;
    return fract(p);
}

float hash12(vec2 p) {
    vec3 p3 = fract(vec3(p.xyx) * 0.1031);
    p3 += dot(p3, p3.yzx + 33.33);
    return fract((p3.x + p3.y) * p3.z);
}

vec2 hash22(vec2 p) {
    vec3 p3 = fract(vec3(p.xyx) * vec3(0.1031, 0.1030, 0.0973));
    p3 += dot(p3, p3.yzx + 33.33);
    return fract((p3.xx + p3.yz) * p3.zy);
}

vec3 hash33(vec3 p3) {
    p3 = fract(p3 * vec3(0.1031, 0.1030, 0.0973));
    p3 += dot(p3, p3.yxz + 33.33);
    return fract((p3.xxy + p3.yxx) * p3.zyx);
}

// ---- noise ------------------------------------------------------------------
// Value noise in [0, 1].
float noise(vec2 x) {
    vec2 i = floor(x);
    vec2 f = fract(x);
    vec2 u = f * f * (3.0 - 2.0 * f);
    float a = hash12(i);
    float b = hash12(i + vec2(1.0, 0.0));
    float c = hash12(i + vec2(0.0, 1.0));
    float d = hash12(i + vec2(1.0, 1.0));
    return mix(mix(a, b, u.x), mix(c, d, u.x), u.y);
}

// Fractal value noise in roughly [0, 1].
float fbm(vec2 p, int octaves) {
    float v = 0.0, amp = 0.5, norm = 0.0;
    for (int i = 0; i < octaves; i++) {
        v += amp * noise(p);
        norm += amp;
        p = rot(0.6) * p * 2.03 + 17.1;
        amp *= 0.5;
    }
    return v / norm;
}

// ---- colour -----------------------------------------------------------------
vec3 srgbToLinear(vec3 c) { return pow(c, vec3(2.2)); }
vec3 linearToSrgb(vec3 c) { return pow(max(c, 0.0), vec3(1.0 / 2.2)); }

// hexColor(0x3d8a99) -> linear RGB
vec3 hexColor(int h) {
    return srgbToLinear(vec3((h >> 16) & 255, (h >> 8) & 255, h & 255) / 255.0);
}
