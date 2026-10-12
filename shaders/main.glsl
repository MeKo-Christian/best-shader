// Best Shader - tiled floor in front of a rule-based cubic lattice.
//
// Buffer A (poses.glsl) evaluates the pose of every cube of the ball once per
// frame; Buffer B (scene.glsl) reads them and renders the sharp scene with
// eye-space depth in alpha.
// This pass applies the camera depth of field as a scatter-as-gather bokeh
// (after Dennis Gustafsson, "Bokeh depth of field in a single pass"): samples
// on a golden-angle spiral contribute when their own circle of confusion
// reaches the centre pixel, so the in-focus floor does not bleed.
//
// All shading happens in linear RGB; linearToSrgb() encodes at the very end.
//
// Defines (scene and lattice defines are listed in scene.glsl/lattice.glsl):
//   -D FOCUS=4.5      focus distance (eye-space depth), the front of the ball
//   -D APERTURE=6.0   blur radius in pixels (at 1254 px height) at infinity
//   -D EXPOSURE=1.0   overall brightness

// #bufferA "poses.glsl"
// #bufferB "scene.glsl"

#ifndef EXPOSURE
#define EXPOSURE 1.0
#endif

#include "common.glsl"

#define GOLDEN_ANGLE 2.39996323
#define RAD_SCALE 0.6

vec3 depthOfField(vec2 fragCoord, float scale) {
    vec2 px = 1.0 / iResolution.xy;
    vec2 uv = fragCoord * px;
    vec4 centre = texture(iChannel0, uv);
    float centreSize = cocRadius(centre.a, scale);
    float maxSize = APERTURE * scale;

    vec3 col = centre.rgb;
    float tot = 1.0;
    float radius = RAD_SCALE;
    for (float ang = 0.0; radius < maxSize; ang += GOLDEN_ANGLE) {
        vec4 s = texture(iChannel0, uv + vec2(cos(ang), sin(ang)) * px * radius);
        float sampleSize = cocRadius(s.a, scale);
        if (s.a > centre.a) sampleSize = clamp(sampleSize, 0.0, centreSize * 2.0);
        float m = smoothstep(radius - 0.5, radius + 0.5, sampleSize);
        col += mix(col / tot, s.rgb, m);
        tot += 1.0;
        radius += RAD_SCALE / radius;
    }
    return col / tot;
}

void mainImage(out vec4 fragColor, in vec2 fragCoord) {
    float scale = iResolution.y / 1254.0;
    vec3 col = depthOfField(fragCoord, scale);

    vec2 c = fragCoord / iResolution.xy - 0.5;
    col *= 1.0 - 0.5 * dot(c, c);                                     // vignette
    col *= EXPOSURE;
    col = linearToSrgb(col);
    col += (hash12(fragCoord) - 0.5) / 255.0;                         // dither

    fragColor = vec4(col, 1.0);
}
