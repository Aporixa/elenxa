package agent

import "sync/atomic"

// skillRoots holds the skill root directory (set once by the server at wiring
// time). Norma confines the file tools (Read/Write/Edit/LS/Glob/Grep) to an
// agent's WorkingDir plus AllowedRoots; skills must stay readable because a
// SKILL.md routinely points at its own reference.md / scripts/ files.
var skillRoots atomic.Pointer[[]string]

// SetSkillRoots records the directories every agent may read skills from.
func SetSkillRoots(dirs ...string) {
	d := append([]string(nil), dirs...)
	skillRoots.Store(&d)
}

// allowedRoots returns the skill roots plus any extra directories.
func allowedRoots(extra ...string) []string {
	var out []string
	if p := skillRoots.Load(); p != nil {
		out = append(out, (*p)...)
	}
	for _, e := range extra {
		if e != "" {
			out = append(out, e)
		}
	}
	return out
}
