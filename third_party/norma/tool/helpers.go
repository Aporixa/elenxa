package tool

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

const defaultMaxOutput = 30000

func maxOut(tc *ToolContext) int {
	if tc != nil && tc.MaxOutputChars > 0 {
		return tc.MaxOutputChars
	}
	return defaultMaxOutput
}

var spillSeq atomic.Int64

// Capture bounds a tool's textual output for the model. When the output exceeds
// the limit (ToolContext.MaxOutputChars, default 30000) and the host configured
// an OutputDir, the FULL output is written to a file there and a tight head + a
// pointer to that file is returned — so the model can read slices on demand
// (grep/sed) instead of losing the overflow. Without an OutputDir it falls back
// to a head+tail truncation (the overflow is discarded). Tools that produce
// potentially large output should return Capture(tc, output) rather than the raw
// string.
func Capture(tc *ToolContext, s string) string {
	max := maxOut(tc)
	if len(s) <= max {
		return s
	}
	if tc != nil && tc.OutputDir != "" {
		if path, err := spillOutput(tc.OutputDir, s); err == nil {
			ref := path
			if tc.WorkingDir != "" {
				if rel, e := filepath.Rel(tc.WorkingDir, path); e == nil && !strings.HasPrefix(rel, "..") {
					ref = rel
				}
			}
			lines := strings.Count(s, "\n") + 1
			return s[:max] + fmt.Sprintf(
				"\n\n... <persisted-output>[Output too large: full %d bytes / %d lines.Full output saved to  %s </persisted-output>",
				len(s), lines, ref)
		}
	}
	return truncate(s, max)
}

// alreadyCaptured reports whether s already carries a marker Capture leaves
// behind — a spill pointer or a head+tail truncation notice. It is how a global
// post-tool net recognises output a tool already ran through Capture itself.
//
// A false negative (legitimate text that happens to contain a marker) only means
// the guard fails to re-cap already-large output; it never corrupts. The markers
// are distinctive enough that this is vanishingly rare.
func alreadyCaptured(s string) bool {
	return strings.Contains(s, "<persisted-output>") || strings.Contains(s, "characters truncated]")
}

// CaptureOnce is Capture with an idempotency guard: output a tool already ran
// through Capture (so it carries a spill pointer or truncation notice) is
// returned unchanged. This lets a single post-tool net cap every tool's output
// without re-truncating or re-spilling what built-in / MCP / custom tools
// already handled themselves. Output within the limit is a no-op either way,
// since Capture returns it unchanged.
func CaptureOnce(tc *ToolContext, s string) string {
	if alreadyCaptured(s) {
		return s
	}
	return Capture(tc, s)
}

func spillOutput(dir, s string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("output-%d-%d.txt", time.Now().Unix(), spillSeq.Add(1))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// resolvePath joins a relative path against the working directory and confines
// the result to the allowed roots (local patch; upstream did no confinement).
//
// Allowed roots are the WorkingDir, the OutputDir (spilled tool output), the
// session's background-task output dir and any ToolContext.AllowedRoots. Both the
// target and every root are resolved through symlinks first — for a path that
// does not exist yet, the longest existing prefix is resolved (dangling symlinks
// are followed by hand) — and containment is checked on the resolved paths with
// a separator-aware prefix test, so neither `..` nor a symlink can escape. The
// returned path is the resolved one, so the caller operates on exactly what was
// checked. With no WorkingDir there is no root to confine to and the path is
// returned cleaned but unconfined (hosts always set WorkingDir).
func resolvePath(tc *ToolContext, p string) (string, error) {
	if tc == nil || tc.WorkingDir == "" {
		if p == "" {
			return p, nil
		}
		return filepath.Clean(p), nil
	}
	path := p
	if !filepath.IsAbs(path) {
		path = filepath.Join(tc.WorkingDir, path)
	}
	path, err := filepath.Abs(path) // also Cleans: collapses `..`
	if err != nil {
		return "", fmt.Errorf("Error: invalid path %s: %v", p, err)
	}
	real, err := evalExisting(path, 0)
	if err != nil {
		return "", fmt.Errorf("Error: cannot resolve path %s: %v", p, err)
	}
	for _, r := range confineRoots(tc) {
		if pathWithin(r, real) {
			return real, nil
		}
	}
	return "", fmt.Errorf("Error: path %s is outside the allowed working directory %s", p, tc.WorkingDir)
}

// confineRoots returns the symlink-resolved absolute allowed roots for tc.
func confineRoots(tc *ToolContext) []string {
	cands := []string{tc.WorkingDir, tc.OutputDir}
	if tc.Tasks != nil {
		cands = append(cands, tc.Tasks.dir)
	}
	cands = append(cands, tc.AllowedRoots...)
	var roots []string
	for _, c := range cands {
		if c == "" {
			continue
		}
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		real, err := evalExisting(abs, 0)
		if err != nil {
			continue
		}
		roots = append(roots, real)
	}
	return roots
}

// evalExisting resolves symlinks in an absolute, cleaned path. When the path
// (or a suffix of it) does not exist, the longest existing prefix is resolved
// and the missing components are re-appended. A dangling symlink is followed
// manually to its (possibly non-existent) target so it cannot be used to write
// outside a root.
func evalExisting(path string, depth int) (string, error) {
	if depth > 64 {
		return "", errors.New("too many levels of symbolic links")
	}
	real, err := filepath.EvalSymlinks(path)
	if err == nil {
		return real, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	parent, base := filepath.Dir(path), filepath.Base(path)
	if parent == path { // filesystem root
		return path, nil
	}
	if fi, lerr := os.Lstat(path); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
		target, rerr := os.Readlink(path)
		if rerr != nil {
			return "", rerr
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(parent, target)
		}
		return evalExisting(filepath.Clean(target), depth+1)
	}
	rp, err := evalExisting(parent, depth+1)
	if err != nil {
		return "", err
	}
	return filepath.Join(rp, base), nil
}

// pathWithin reports whether p equals root or lies beneath it, using a
// separator-aware prefix test ("/work" does not contain "/workshop").
func pathWithin(root, p string) bool {
	if p == root {
		return true
	}
	prefix := root
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	return strings.HasPrefix(p, prefix)
}

// truncate trims s to max characters, keeping head and tail.
func truncate(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	half := max / 2
	return s[:half] + fmt.Sprintf("\n\n... [%d characters truncated] ...\n\n", len(s)-max) + s[len(s)-half:]
}
