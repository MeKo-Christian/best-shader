package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/go-gl/gl/v4.1-core/gl"
)

// Pass indices, used for timing and log messages.
const (
	passBufferA = iota
	passImage
	numPasses
)

var passNames = [numPasses]string{"Buffer A", "Image"}

// pipeline is a compiled shader: the Image program plus, for multipass
// shaders, the Buffer A program that feeds the Image pass's iChannel0.
type pipeline struct {
	image   *program
	bufferA *program // nil for single-pass shaders
}

func (p *pipeline) delete() {
	if p == nil {
		return
	}

	p.image.delete()
	p.bufferA.delete()
}

// buildPipeline compiles every pass of srcs with the same defines. If any
// pass fails, nothing is kept.
func buildPipeline(srcs *passSources, defs defineList) (*pipeline, error) {
	image, err := buildProgram(srcs.image, defs)
	if err != nil {
		return nil, err
	}

	pipe := &pipeline{image: image}

	if srcs.bufferA != nil {
		pipe.bufferA, err = buildProgram(srcs.bufferA, defs)
		if err != nil {
			pipe.delete()
			return nil, fmt.Errorf("%s pass: %w", passNames[passBufferA], err)
		}
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

// bufferFormat is the Buffer A render target: half float, so values above 1
// and smooth gradients survive, sampled bilinearly and clamped at the edges
// like a Shadertoy buffer with filter "linear" and wrap "clamp".
var bufferFormat = texFormat{internal: gl.RGBA16F, pixelType: gl.HALF_FLOAT, filter: gl.LINEAR}

// renderer draws frames of a pipeline into the bound framebuffer. It owns the
// Buffer A target and reallocates it whenever the render size changes.
//
// Buffer A has no inputs today. Self-feedback would make bufferA a pair of
// targets that swap after every frame (ping-pong): the pass reads the previous
// frame from one while writing the other, and the Image pass reads the one just
// written. Bind the read side to the Buffer A program's channel unit in draw.
type renderer struct {
	screen  *quad
	bufferA *fbo
	timer   *passTimer // nil unless benchmarking
}

func newRenderer() *renderer {
	return &renderer{screen: newQuad()}
}

func (r *renderer) delete() {
	if r.bufferA != nil {
		r.bufferA.delete()
	}

	r.timer.delete()
	r.screen.delete()
}

// draw renders one frame of pipe into framebuffer out (0 = the window).
// Buffer A (if any) is rendered first into its own target, then the Image
// pass reads it on iChannel0. A nil pipeline just clears.
func (r *renderer) draw(pipe *pipeline, uni frameUniforms, out uint32) error {
	gl.BindFramebuffer(gl.FRAMEBUFFER, out)

	if pipe == nil {
		r.screen.draw(nil, uni)
		return nil
	}

	if pipe.bufferA == nil || uni.width <= 0 || uni.height <= 0 {
		r.timer.begin(passImage)
		r.screen.draw(pipe.image, uni)
		r.timer.end()

		return nil
	}

	err := r.ensureBuffer(uni.width, uni.height)
	if err != nil {
		return err
	}

	// Buffer A: nothing bound, so its iChannels read black and the pass never
	// samples the texture it renders into.
	gl.BindFramebuffer(gl.FRAMEBUFFER, r.bufferA.id)
	gl.ActiveTexture(gl.TEXTURE0)
	gl.BindTexture(gl.TEXTURE_2D, 0)
	r.timer.begin(passBufferA)
	r.screen.draw(pipe.bufferA, uni)
	r.timer.end()

	// Image: Buffer A on iChannel0.
	gl.BindFramebuffer(gl.FRAMEBUFFER, out)
	gl.BindTexture(gl.TEXTURE_2D, r.bufferA.tex)

	imageUni := uni
	imageUni.channelRes[0] = [3]float32{float32(r.bufferA.width), float32(r.bufferA.height), 1}

	r.timer.begin(passImage)
	r.screen.draw(pipe.image, imageUni)
	r.timer.end()

	// Unbind again, so a single-pass shader loaded later reads black.
	gl.BindTexture(gl.TEXTURE_2D, 0)

	return nil
}

// ensureBuffer (re)allocates the Buffer A target at w x h.
func (r *renderer) ensureBuffer(w, h int) error {
	if r.bufferA != nil && r.bufferA.width == w && r.bufferA.height == h {
		return nil
	}

	if r.bufferA != nil {
		r.bufferA.delete()
		r.bufferA = nil
	}

	target, err := newFBO(w, h, bufferFormat)
	if err != nil {
		return fmt.Errorf("%s target: %w", passNames[passBufferA], err)
	}

	r.bufferA = target

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
			parts = append(parts, fmt.Sprintf("%s %.2f ms", passNames[pass], el.Seconds()*1000/float64(t.frames)))
		}
	}

	return "GPU time per pass: " + strings.Join(parts, ", ")
}
