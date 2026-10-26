package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-gl/gl/v4.1-core/gl"
)

// Pass indices, used for timing and log messages: buffer k is pass k, the
// Image pass comes last.
const (
	passImage = maxBuffers
	numPasses = maxBuffers + 1
)

func passName(pass int) string {
	if pass == passImage {
		return "Image"
	}

	return "Buffer " + bufferLetter(pass)
}

// pipeline is a compiled shader: the Image program plus, for multipass
// shaders, the buffer programs that run before it.
type pipeline struct {
	image   *program
	buffers []*program // Buffer A, B, ... in render order
}

func (p *pipeline) delete() {
	if p == nil {
		return
	}

	p.image.delete()

	for _, b := range p.buffers {
		b.delete()
	}
}

// buildPipeline compiles every pass of srcs with the same defines. If any
// pass fails, nothing is kept.
func buildPipeline(srcs *passSources, defs defineList) (*pipeline, error) {
	image, err := buildProgram(srcs.image, defs)
	if err != nil {
		return nil, err
	}

	pipe := &pipeline{image: image}

	for k, src := range srcs.buffers {
		prog, err := buildProgram(src, defs)
		if err != nil {
			pipe.delete()
			return nil, fmt.Errorf("%s pass: %w", passName(k), err)
		}

		pipe.buffers = append(pipe.buffers, prog)
	}

	return pipe, nil
}

// loadPipeline preprocesses and compiles every pass of opt.shader. The file
// list covers every file read, also on error, so hot reload can watch them.
func loadPipeline(opt *options) (*pipeline, []string, error) {
	srcs, err := loadPasses(opt.shader)
	files := srcs.files()

	if err != nil {
		return nil, files, err
	}

	pipe, err := buildPipeline(srcs, opt.defines)

	return pipe, files, err
}

// bufferFormat is the target of every buffer pass: 32-bit float like
// Shadertoy's buffers, so values above 1, smooth gradients and data that
// needs full precision survive; sampled bilinearly and clamped at the edges
// like a Shadertoy buffer with filter "linear" and wrap "clamp".
var bufferFormat = texFormat{internal: gl.RGBA32F, pixelType: gl.FLOAT, filter: gl.LINEAR}

// renderer draws frames of a pipeline into the bound framebuffer. It owns the
// buffer targets and reallocates them whenever the render size changes.
//
// The passes form a chain: each buffer reads the one before it on iChannel0
// (Buffer A reads nothing), and the Image pass reads the last buffer. No pass
// reads its own target, so there is no feedback yet; that would make a buffer
// a pair of targets that swap after every frame (ping-pong).
type renderer struct {
	screen  *quad
	buffers []*fbo
	timer   *passTimer // nil unless benchmarking
}

func newRenderer() *renderer {
	return &renderer{screen: newQuad()}
}

func (r *renderer) delete() {
	for _, b := range r.buffers {
		b.delete()
	}

	r.timer.delete()
	r.screen.delete()
}

// draw renders one frame of pipe into framebuffer out (0 = the window). The
// buffers (if any) render first, each into its own target, then the Image
// pass. A nil pipeline just clears.
func (r *renderer) draw(pipe *pipeline, uni frameUniforms, out uint32) error {
	gl.BindFramebuffer(gl.FRAMEBUFFER, out)

	if pipe == nil {
		r.screen.draw(nil, uni)
		return nil
	}

	if len(pipe.buffers) == 0 || uni.width <= 0 || uni.height <= 0 {
		r.timer.begin(passImage)
		r.screen.draw(pipe.image, uni)
		r.timer.end()

		return nil
	}

	err := r.ensureBuffers(len(pipe.buffers), uni.width, uni.height)
	if err != nil {
		return err
	}

	// Buffer A gets nothing bound, so its iChannels read black; every later
	// pass reads the previous target, never the one it renders into.
	gl.ActiveTexture(gl.TEXTURE0)

	prev := uint32(0)
	passUni := uni

	for slot, prog := range pipe.buffers {
		gl.BindFramebuffer(gl.FRAMEBUFFER, r.buffers[slot].id)
		gl.BindTexture(gl.TEXTURE_2D, prev)
		r.timer.begin(slot)
		r.screen.draw(prog, passUni)
		r.timer.end()

		prev = r.buffers[slot].tex
		passUni.channelRes[0] = [3]float32{float32(r.buffers[slot].width), float32(r.buffers[slot].height), 1}
	}

	gl.BindFramebuffer(gl.FRAMEBUFFER, out)
	gl.BindTexture(gl.TEXTURE_2D, prev)
	r.timer.begin(passImage)
	r.screen.draw(pipe.image, passUni)
	r.timer.end()

	// Unbind again, so a single-pass shader loaded later reads black.
	gl.BindTexture(gl.TEXTURE_2D, 0)

	return nil
}

// ensureBuffers (re)allocates count buffer targets at w x h.
func (r *renderer) ensureBuffers(count, w, h int) error {
	if len(r.buffers) == count && r.buffers[0].width == w && r.buffers[0].height == h {
		return nil
	}

	for _, b := range r.buffers {
		b.delete()
	}

	r.buffers = nil

	for k := range count {
		target, err := newFBO(w, h, bufferFormat)
		if err != nil {
			return fmt.Errorf("%s target: %w", passName(k), err)
		}

		r.buffers = append(r.buffers, target)
	}

	return nil
}

// passTimer accumulates the GPU time of each pass with timer queries.
type passTimer struct {
	queries [numPasses]uint32
	used    [numPasses]bool // a query was issued since the last collect
	elapsed [numPasses]time.Duration
	frames  int
}

func newPassTimer() *passTimer {
	t := &passTimer{}
	gl.GenQueries(numPasses, &t.queries[0])

	return t
}

func (t *passTimer) delete() {
	if t != nil {
		gl.DeleteQueries(numPasses, &t.queries[0])
	}
}

func (t *passTimer) begin(pass int) {
	if t == nil {
		return
	}

	gl.BeginQuery(gl.TIME_ELAPSED, t.queries[pass])
	t.used[pass] = true
}

func (t *passTimer) end() {
	if t != nil {
		gl.EndQuery(gl.TIME_ELAPSED)
	}
}

// collect adds the results of the frame just drawn; it waits for the GPU.
func (t *passTimer) collect() {
	for pass, query := range t.queries {
		if !t.used[pass] {
			continue
		}

		var ns uint64
		gl.GetQueryObjectui64v(query, gl.QUERY_RESULT, &ns)
		t.elapsed[pass] += time.Duration(ns)
		t.used[pass] = false
	}

	t.frames++
}

// summary formats the mean GPU time per frame of every pass that ran.
func (t *passTimer) summary() string {
	if t.frames == 0 {
		return ""
	}

	var parts []string

	for pass, el := range t.elapsed {
		if el > 0 {
			parts = append(parts, fmt.Sprintf("%s %.2f ms", passName(pass), el.Seconds()*1000/float64(t.frames)))
		}
	}

	return "GPU time per pass: " + strings.Join(parts, ", ")
}
