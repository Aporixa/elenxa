package agent

import (
	"os"
	"runtime"
	"strings"
)

// Child-process environment for agent-run commands.
//
// Agent child processes (Bash, shell sessions, background tasks, custom tools) must
// NOT inherit the backend's full environment: it carries LLM API keys, the
// PostgreSQL DSN/password, JWT material and similar secrets that a prompt-injected
// agent could read with a single `env`. SafeEnv builds the environment from a small
// allowlist of the parent process plus the variables Elenxa deliberately injects
// (proxy / MITM-CA settings). The result is complete — Norma runs children with
// exactly this environment when it is non-nil.

// safeEnvExact are variable names copied verbatim from the parent environment.
var safeEnvExact = map[string]bool{
	"PATH": true, "HOME": true, "USER": true, "LOGNAME": true, "SHELL": true,
	"LANG": true, "TERM": true, "TZ": true, "TMPDIR": true,
}

// safeEnvWindows are additionally required for processes to start on Windows.
var safeEnvWindows = map[string]bool{
	"SYSTEMROOT": true, "WINDIR": true, "COMSPEC": true, "PATHEXT": true,
	"TEMP": true, "TMP": true, "USERPROFILE": true,
}

// safeEnvProxy are proxy variables, matched case-insensitively.
var safeEnvProxy = map[string]bool{
	"HTTP_PROXY": true, "HTTPS_PROXY": true, "NO_PROXY": true, "ALL_PROXY": true,
}

// safeEnvAllowed reports whether a parent variable may pass to an agent child.
func safeEnvAllowed(key string) bool {
	upper := strings.ToUpper(key)
	if safeEnvProxy[upper] {
		return true
	}
	if runtime.GOOS == "windows" {
		if safeEnvExact[upper] || safeEnvWindows[upper] {
			return true
		}
	} else if safeEnvExact[key] {
		return true
	}
	return strings.HasPrefix(key, "LC_")
}

// SafeEnv returns the allowlisted subset of the current process environment
// (PATH, HOME, USER, LOGNAME, SHELL, LANG, LC_*, TERM, TZ, TMPDIR, and
// HTTP(S)_PROXY / NO_PROXY / ALL_PROXY in any case) with extra "KEY=VALUE" entries
// layered on top; an extra entry replaces any inherited variable with the same
// key. Never nil, so assigning it to exec.Cmd.Env always replaces — never
// inherits — the parent environment.
func SafeEnv(extra ...string) []string {
	override := make(map[string]bool, len(extra))
	for _, kv := range extra {
		if k, _, ok := strings.Cut(kv, "="); ok && k != "" {
			override[envKey(k)] = true
		}
	}
	out := make([]string, 0, 16+len(extra))
	for _, kv := range os.Environ() {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || k == "" || !safeEnvAllowed(k) || override[envKey(k)] {
			continue
		}
		out = append(out, kv)
	}
	return append(out, extra...)
}

// envKey normalises a variable name for override matching (case-insensitive on
// Windows, exact elsewhere).
func envKey(k string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(k)
	}
	return k
}
