# Best Shader

A starting point for a new GLSL shader: a Shadertoy-compatible Go runner,
the `imgstat` image analysis tool and a placeholder shader in
`shaders/main.glsl`.

The runner and tooling are shared with
[imperfection](https://github.com/CWBudde/imperfection) and
[cosmic-abstraction](https://github.com/CWBudde/cosmic-abstraction); see the
imperfection README for the full flag, multipass (`#bufferA`), `#include` and
`imgstat` documentation.

## Usage

Requires Go and [just](https://github.com/casey/just); the runner needs
GLFW (`libgl1-mesa-dev xorg-dev` on Debian/Ubuntu).

```sh
just run                  # window with hot reload (Space pauses, P saves a PNG)
just shot 0,15            # offscreen frames into out/
just bench                # frame time at 1280x720 and 2x
just bundle               # Shadertoy export into out/shadertoy.glsl
```

To recreate an image, put it at `reference/reference.png` (not versioned)
and set `size` in the justfile to its dimensions. Then:

```sh
just compare              # out/shot.png + out/shot_cmp.png against the reference
just score                # imgstat style score (lower = better)
just diff                 # dE heatmap and orientation difference
just stat-ref             # reference statistics into docs/reference-stats.txt
```

Go needs `-buildvcs=false` here (stray `/mnt/projekte/Code/.git`); the
justfile sets it.

## Layout

| Path                                                                 | Content                                          |
| -------------------------------------------------------------------- | ------------------------------------------------ |
| `shaders/main.glsl`                                                  | entry point (Shadertoy `mainImage`), placeholder |
| `shaders/common.glsl`                                                | hashes, noise, fbm, colour helpers (linear RGB)  |
| `main.go`, `render.go`, `pipeline.go`, `preprocess.go`, `compare.go` | the Shadertoy-compatible runner                  |
| `cmd/imgstat/`                                                       | image analysis and style scoring tool            |
