package db

import "testing"

func TestResearchInteractiveShellDefaults(t *testing.T) {
	want := map[string]bool{"mainagent": true, "worker": true, "planner": false}
	for _, agent := range builtinAgents {
		expected, ok := want[agent.key]
		if !ok {
			continue
		}
		if agent.interactiveShell != expected {
			t.Errorf("%s interactive shell = %v, want %v", agent.key, agent.interactiveShell, expected)
		}
		delete(want, agent.key)
	}
	for key := range want {
		t.Errorf("builtin agent %q not found", key)
	}
}
