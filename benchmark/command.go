package benchmark

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"time"
)

func runCommand(ctx context.Context, spec CommandSpec, extraEnv map[string]string) ([]byte, error) {
	if spec.Executable == "" {
		return nil, fmt.Errorf("command executable is required")
	}
	timeout := time.Duration(spec.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, spec.Executable, spec.Args...)
	env := append([]string(nil), os.Environ()...)
	keys := make([]string, 0, len(spec.Env)+len(extraEnv))
	combined := map[string]string{}
	for key, value := range spec.Env {
		combined[key] = value
	}
	for key, value := range extraEnv {
		combined[key] = value
	}
	for key := range combined {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+combined[key])
	}
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if runCtx.Err() != nil {
			return stdout.Bytes(), fmt.Errorf("command timed out: %w", runCtx.Err())
		}
		return stdout.Bytes(), fmt.Errorf("command failed: %w: %s", err, stderr.String())
	}
	return stdout.Bytes(), nil
}
