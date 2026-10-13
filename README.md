# Best Shader

**▶ [Try it online](https://meko-christian.github.io/best-shader/)**: runs in any
browser with WebGL2 (`Space` pauses, `F` toggles fullscreen).

A tiled marble floor in front of a rule-based beam lattice, and on the floor a
ball of milky glass cubes, faintly tinted in the palette (`-D GLASS=0`:
polished marble), that bounces slowly,
squashing like a rubber ball on every landing: as a regular cubed sphere it
touches the floor three times, the third time deeper, winding up, then jumps
twice as high: on the way up its cubes turn irregular about the centre, most
at the top, and on the way down they return, regular again as it lands
(8.5 s). The scene loops over two bounce loops (17 s): the camera goes round
an ellipse, right low and back left high, turning to keep the ball in place
and focusing on its centre, and the orange, red and purple lights pass their
colours round (`-D MOOD=0`: white).
Rendered by a Shadertoy-compatible Go runner; `imgstat` compares renders with
the reference images.

Every cube pose is a closed formula of the cube's index and the time, with no
tables. Each cube owns a cell of a cubed sphere, the cone from the ball centre
over four grid vertices. The vertices scatter and swirl as functions of the
vertex alone, so neighbouring cells always share their walls. A cube stands
where it just fits its cone, so no two cubes can touch. In the irregular state
the cubes may sink a little deeper, which packs the ball as densely as the
regular one at the price of rare overlaps below 0.02 cube edges. Each cube
heads straight for its own irregular radius, so the change is one smooth move
per cube.

The centre moves like a ball's, whatever the cubes do: all arcs share one
gravity (the big jump, twice as high, lasts sqrt(2) times as long), and on the
floor the ball is a spring that matches the landing and take-off speeds. The
squash moves cubes radially, spreads the bottom cells so that the cubes there
can come in onto the floor, and turns those the floor presses flat; the cones
still hold.
`cmd/spheregen` mirrors the formula, checks it over the whole loop and writes
its constants to `shaders/sphere_data.glsl`. Buffer A evaluates the formula
once per frame, and the scene pass reads the poses with `texelFetch`.

The runner and tooling are shared with
[imperfection](https://github.com/CWBudde/imperfection) and
[cosmic-abstraction](https://github.com/CWBudde/cosmic-abstraction); see the
imperfection README for the full flag, multipass, `#include` and `imgstat`
documentation. Here the runner chains up to four buffers (`#bufferA` to
`#bufferD` in the Image file, run in letter order). Each pass reads the one
before it on iChannel0, and the buffers are 32-bit float like Shadertoy's.

## Usage

Requires Go and [just](https://github.com/casey/just); the runner needs
GLFW (`libgl1-mesa-dev xorg-dev` on Debian/Ubuntu).

```sh
just run                  # window with hot reload (Space pauses, P saves a PNG)
just shot 0,15            # offscreen frames into out/
just bench                # frame time at 1280x720 and 2x
just bundle               # Shadertoy export into out/shadertoy.glsl
just clip                 # one loop of the scene (17 s) at 60 fps into out/clip.mp4
just sphere               # check the ball's formula, write shaders/sphere_data.glsl
just web                  # the Pages site in out/web, served on :8000
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

## Web version

Live at <https://meko-christian.github.io/best-shader/>. On every push to `main`,
the Pages workflow bundles the shaders and publishes them with the small WebGL2
player in `web/`, which runs the same chain of passes as the Go runner (Buffer A,
Buffer B, then the Image pass). It needs `EXT_color_buffer_float` for the float
buffers. Buffer A renders into a fixed small target, since it only holds the cube
poses. The canvas is the largest centred square that fits the window; it renders
at most 1080 lines and lowers the resolution further when frames take longer
than about 45 ms. `?scale=0.5` fixes the render scale, `?t=20` sets the start
time. Keys: `Space` pauses, `F` or a double click toggles fullscreen.

## Layout

| Path                                                                 | Content                                                  |
| -------------------------------------------------------------------- | -------------------------------------------------------- |
| `shaders/main.glsl`                                                  | Image pass: depth of field, vignette, sRGB               |
| `shaders/poses.glsl`                                                 | Buffer A: every cube's pose, once per frame              |
| `shaders/scene.glsl`                                                 | Buffer B: camera, floor, lattice tracing, shading        |
| `shaders/lattice.glsl`                                               | where the lattice is and which beams exist               |
| `shaders/sphere_pose.glsl`                                           | cube ball: the motion formula                            |
| `shaders/sphere.glsl`                                                | cube ball: ray queries on the poses of Buffer A          |
| `shaders/sphere_data.glsl`                                           | generated by `cmd/spheregen`, do not edit                |
| `shaders/common.glsl`                                                | lens, hashes, noise, fbm, colour helpers                 |
| `cmd/spheregen/`                                                     | checks the ball's formula, derives its constants         |
| `main.go`, `render.go`, `pipeline.go`, `preprocess.go`, `compare.go` | the Shadertoy-compatible runner                          |
| `cmd/imgstat/`                                                       | image analysis and style scoring tool                    |
| `web/`                                                               | WebGL2 player for the bundled passes (GitHub Pages)      |
| `.github/workflows/pages.yml`                                        | bundles the shaders and publishes `web/` to GitHub Pages |
