package tool

import (
	"os"
	"runtime"
	"strings"
)

// Child-process environment hardening (local patch).
//
// Upstream norma started every child process (Bash, background tasks, PTY
// sessions, ripgrep, MCP stdio servers) with the FULL parent environment, which
// leaks the host's API keys and other secrets to anything the agent runs. Every
// spawn site now builds its environment with ChildEnv: a small allowlist copied
// from os.Environ(), plus the caller's explicit "KEY=VALUE" entries layered on top
// (later entries win, as os/exec keeps the last duplicate). Anything a child needs
// beyond the allowlist must be passed explicitly by the host.

// childEnvExact are variable names copied verbatim from the parent environment.
var childEnvExact = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true,
	"LANG": true, "TERM": true, "TZ": true, "TMPDIR": true,
}

// childEnvWindows are additionally required for processes to start on Windows.
var childEnvWindows = map[string]bool{
	"SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true, "PATHEXT": true,
	"TEMP": true, "TMP": true, "USERPROFILE": true,
}

// childEnvProxy are proxy variables, matched case-insensitively.
var childEnvProxy = map[string]bool{
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true, "ALL_PROXY": true,
}

// childEnvAllowed reports whether a parent variable may pass to a child.
func childEnvAllowed(key string) bool {
	upper := strings.ToUpper(key)
	if childEnvProxy[upper] {
		return true
	}
	if runtime.GOOS == "windows" {
		if childEnvExact[upper] || childEnvWindows[upper] {
			return true
		}
	} else if childEnvExact[key] {
		return true
	}
	if strings.HasPrefix(key, "LC_") {
		return true
	}
	return false
}

// BaseEnv returns the allowlisted subset of the current process environment:
// PATH, HOME, USER, LOGNAME, SHELL, LANG, LC_*, TERM, TZ, TMPDIR and the proxy
// variables HTTP_PROXY / HTTPS_PROXY / NO_PROXY / ALL_PROXY (any case), plus the
// handful of system variables Windows needs to start a process. Never nil.
func BaseEnv() []string {
	parent := os.Environ()
	out := make([]string, 0, 16)
	for _, kv := range parent {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			continue
		}
		if childEnvAllowed(k) {
			out = append(out, kv)
		}
	}
	return out
}

// ChildEnv returns the environment for a spawned child process: BaseEnv() with
// extra ("KEY=VALUE" entries supplied explicitly by the host) appended so they
// override allowlisted values. The result is never nil, so assigning it to
// exec.Cmd.Env always replaces — never inherits — the parent environment.
func ChildEnv(extra []string) []string {
	return append(BaseEnv(), extra...)
}
