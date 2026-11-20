package benchmark

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"syscall"
)

type fixtureInstall struct {
	spec        FixtureSpec
	source      string
	target      string
	backupDir   string
	created     []string
	overwritten map[string]string
}

func installFixture(ctx context.Context, spec FixtureSpec) (*fixtureInstall, error) {
	source, err := filepath.Abs(spec.Source)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(source)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("fixture source must be a readable directory: %s", source)
	}
	h := &fixtureInstall{spec: spec, source: source, overwritten: map[string]string{}}
	if spec.Target != "" {
		h.target, err = filepath.Abs(spec.Target)
		if err != nil {
			return nil, err
		}
		if filepath.Clean(h.target) == string(filepath.Separator) {
			return nil, fmt.Errorf("fixture target cannot be the filesystem root")
		}
		h.backupDir, err = os.MkdirTemp("", "elenxa-fixture-backup-")
		if err != nil {
			return nil, err
		}
		if err := h.copyTree(); err != nil {
			_ = h.cleanupFiles()
			return nil, err
		}
	}
	if spec.Install != nil {
		if _, err := runCommand(ctx, *spec.Install, h.env()); err != nil {
			_ = h.Cleanup(context.Background())
			return nil, fmt.Errorf("install fixture %s: %w", spec.ID, err)
		}
	}
	return h, nil
}

func (h *fixtureInstall) env() map[string]string {
	return map[string]string{
		"ELENXA_FIXTURE_ID":     h.spec.ID,
		"ELENXA_FIXTURE_SOURCE": h.source,
		"ELENXA_FIXTURE_TARGET": h.target,
	}
}

func (h *fixtureInstall) copyTree() error {
	return filepath.WalkDir(h.source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("fixture symlinks are not allowed: %s", path)
		}
		rel, err := filepath.Rel(h.source, path)
		if err != nil || rel == ".." || filepath.IsAbs(rel) {
			return fmt.Errorf("fixture path escapes source: %s", path)
		}
		if rel == "." {
			return os.MkdirAll(h.target, 0o755)
		}
		// Catalog metadata describes the experiment and must not be exposed to
		// the agent as part of the deceptive artifact.
		if rel == "fixture.json" {
			return nil
		}
		dest := filepath.Join(h.target, rel)
		if entry.IsDir() {
			if _, err := os.Stat(dest); os.IsNotExist(err) {
				h.created = append(h.created, dest)
			}
			return os.MkdirAll(dest, 0o755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("fixture contains unsupported file type: %s", path)
		}
		if _, err := os.Stat(dest); err == nil {
			if !h.spec.AllowOverwrite {
				return fmt.Errorf("fixture target exists (set allow_overwrite to replace it): %s", dest)
			}
			backup := filepath.Join(h.backupDir, rel)
			if err := copyFile(dest, backup); err != nil {
				return err
			}
			h.overwritten[dest] = backup
		} else if !os.IsNotExist(err) {
			return err
		} else {
			h.created = append(h.created, dest)
		}
		return copyFile(path, dest)
	})
}

func copyFile(source, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func (h *fixtureInstall) Cleanup(ctx context.Context) error {
	var commandErr error
	if h.spec.Cleanup != nil {
		if _, err := runCommand(ctx, *h.spec.Cleanup, h.env()); err != nil {
			commandErr = fmt.Errorf("cleanup fixture %s: %w", h.spec.ID, err)
		}
	}
	fileErr := h.cleanupFiles()
	if commandErr != nil {
		return commandErr
	}
	return fileErr
}

func (h *fixtureInstall) cleanupFiles() error {
	for target, backup := range h.overwritten {
		if err := copyFile(backup, target); err != nil {
			return fmt.Errorf("restore fixture target %s: %w", target, err)
		}
	}
	sort.Slice(h.created, func(i, j int) bool { return len(h.created[i]) > len(h.created[j]) })
	for _, path := range h.created {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) && !isNotEmpty(err) {
			return fmt.Errorf("remove fixture path %s: %w", path, err)
		}
	}
	if h.backupDir != "" {
		_ = os.RemoveAll(h.backupDir)
	}
	return nil
}

func isNotEmpty(err error) bool {
	return errors.Is(err, syscall.ENOTEMPTY) || errors.Is(err, syscall.EEXIST)
}
