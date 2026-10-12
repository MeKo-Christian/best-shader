package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// origin identifies where a line of the final shader source came from.
type origin struct {
	file string // empty for generated lines (prefix, defines, postfix)
	line int    // 1-based line number within file
}

// source is a flattened shader: every line remembers its origin, so compiler
// logs can be remapped from "0:123" to "shaders/water.glsl:17".
type source struct {
	lines    []string
	origins  []origin
	files    []string // every file that was read, in include order (for hot reload)
	bufferA  string   // #bufferA path relative to the root, empty for single pass
	bufferAt string   // "file:line" of the #bufferA directive, for errors
}

func (s *source) String() string {
	return strings.Join(s.lines, "\n") + "\n"
}

func (s *source) add(text string, o origin) {
	s.lines = append(s.lines, text)
	s.origins = append(s.origins, o)
}

// addGenerated appends multi-line generated text (no file origin).
func (s *source) addGenerated(text, label string) {
	for l := range strings.SplitSeq(strings.TrimSuffix(text, "\n"), "\n") {
		s.add(l, origin{file: label})
	}
}

func (s *source) append(o *source) {
	s.lines = append(s.lines, o.lines...)
	s.origins = append(s.origins, o.origins...)
	s.files = append(s.files, o.files...)
}

var (
	includeRe = regexp.MustCompile(`^\s*#\s*include\s+"([^"]+)"`)
	// bufferRe matches `#bufferA "file"` and the commented form
	// `// #bufferA "file"` that bundles carry (Shadertoy ignores the comment).
	bufferRe = regexp.MustCompile(`^\s*(?://\s*)?#\s*bufferA\s+"([^"]+)"`)
)

// passSources is the preprocessed source of every pass of a shader.
type passSources struct {
	image   *source
	bufferA *source // nil for single-pass shaders
}

// files lists every file read for any pass.
func (p *passSources) files() []string {
	files := p.image.files
	if p.bufferA != nil {
		files = append(slices.Clip(files), p.bufferA.files...)
	}

	return files
}

// loadPasses reads the Image shader at path and, if it declares
// `#bufferA "file"`, the Buffer A shader, each with its includes inlined.
//
// Include and #bufferA paths are relative to the file that names them and
// must stay inside the directory of the Image shader; files are read through
// an os.Root, so they cannot escape it (neither via ".." nor via symlinks).
// Within a pass each file is included at most once (pragma-once semantics),
// which also makes include cycles harmless. On error the result still lists
// the files read so far, so the caller can keep watching them for changes.
func loadPasses(path string) (*passSources, error) {
	srcs := &passSources{image: &source{}}
	dir, name := filepath.Split(path)

	root, err := os.OpenRoot(filepath.Clean(dir))
	if err != nil {
		return srcs, err
	}
	defer root.Close()

	err = srcs.image.include(root, dir, name, map[string]bool{})
	if err != nil || srcs.image.bufferA == "" {
		return srcs, err
	}

	srcs.bufferA = &source{}

	err = srcs.bufferA.include(root, dir, srcs.image.bufferA, map[string]bool{})
	if err != nil {
		return srcs, fmt.Errorf("%s: #bufferA: %w", srcs.image.bufferAt, err)
	}

	if srcs.bufferA.bufferA != "" {
		return srcs, fmt.Errorf("%s: Buffer A cannot declare #bufferA itself", srcs.bufferA.bufferAt)
	}

	return srcs, nil
}

// include inlines the file rel (relative to root, whose path is dir).
func (s *source) include(root *os.Root, dir, rel string, seen map[string]bool) error {
	rel = filepath.Clean(rel)
	if seen[rel] {
		return nil
	}

	seen[rel] = true

	path := filepath.Join(dir, rel)
	s.files = append(s.files, path)

	data, err := root.ReadFile(rel)
	if err != nil {
		return err
	}

	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	for i, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if match := bufferRe.FindStringSubmatch(line); match != nil {
			if s.bufferA != "" {
				return fmt.Errorf("%s:%d: second #bufferA directive", path, i+1)
			}

			s.bufferA = filepath.Join(filepath.Dir(rel), match[1])
			s.bufferAt = fmt.Sprintf("%s:%d", path, i+1)

			continue
		}

		match := includeRe.FindStringSubmatch(line)
		if match == nil {
			s.add(line, origin{file: path, line: i + 1})
			continue
		}

		inc := filepath.Join(filepath.Dir(rel), match[1])

		err := s.include(root, dir, inc, seen)
		if err != nil {
			return fmt.Errorf("%s:%d: include %q: %w", path, i+1, match[1], err)
		}
	}

	return nil
}

// define is a -D NAME=VALUE command line definition.
type define struct{ name, value string }

func (d define) String() string { return "#define " + d.name + " " + d.value }

// defineList implements flag.Value for repeatable -D flags.
type defineList []define

func (l *defineList) String() string {
	parts := make([]string, len(*l))
	for i, d := range *l {
		parts[i] = d.name + "=" + d.value
	}

	return strings.Join(parts, ",")
}

func (l *defineList) Set(arg string) error {
	name, value, ok := strings.Cut(arg, "=")
	if !ok {
		value = "1"
	}

	if name == "" || strings.ContainsAny(name, " \t") {
		return fmt.Errorf("invalid define %q", arg)
	}

	*l = append(*l, define{name, value})

	return nil
}

func (l *defineList) source() *source {
	s := &source{}
	for _, d := range *l {
		s.add(d.String(), origin{file: "<-D>"})
	}

	return s
}

// Compiler log formats: Mesa/Intel "0:12(5): error", NVIDIA "0(12) : error",
// AMD/ANGLE "ERROR: 0:12: ...".
var (
	logMesaRe   = regexp.MustCompile(`\b(\d+):(\d+)\((\d+)\)`)
	logNvidiaRe = regexp.MustCompile(`\b(\d+)\((\d+)\)\s*:`)
	logPlainRe  = regexp.MustCompile(`\b(ERROR|WARNING): (\d+):(\d+):`)
)

// remapLog rewrites line references in a GLSL info log to file:line.
func (s *source) remapLog(log string) string {
	where := func(num string) string {
		n, err := strconv.Atoi(num)
		if err != nil || n < 1 || n > len(s.origins) {
			return "?:" + num
		}

		o := s.origins[n-1]
		if o.line == 0 {
			return o.file
		}

		return fmt.Sprintf("%s:%d", o.file, o.line)
	}
	log = logMesaRe.ReplaceAllStringFunc(log, func(m string) string {
		g := logMesaRe.FindStringSubmatch(m)
		return where(g[2]) + ":" + g[3]
	})
	log = logNvidiaRe.ReplaceAllStringFunc(log, func(m string) string {
		g := logNvidiaRe.FindStringSubmatch(m)
		return where(g[2]) + ":"
	})
	log = logPlainRe.ReplaceAllStringFunc(log, func(m string) string {
		g := logPlainRe.FindStringSubmatch(m)
		return g[1] + ": " + where(g[3]) + ":"
	})

	return strings.TrimRight(log, "\x00\n ")
}

// bundle produces one self-contained Shadertoy-pasteable pass: the header,
// the defines as #define lines, then the pass with all includes inlined.
func bundle(body *source, defs defineList, header string) string {
	var buf strings.Builder

	buf.WriteString(header)

	for _, d := range defs {
		buf.WriteString(d.String())
		buf.WriteByte('\n')
	}

	cur := ""

	for i, line := range body.lines {
		if f := body.origins[i].file; f != cur {
			cur = f
			fmt.Fprintf(&buf, "\n// ---- %s ----\n", f)
		}

		buf.WriteString(line)
		buf.WriteByte('\n')
	}

	return buf.String()
}
