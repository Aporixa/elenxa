package harness

import (
	"testing"

	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/permission"
	"github.com/Autumn-27/norma/tool"
)

func refusalTurn() []llm.StreamEvent {
	return []llm.StreamEvent{
		{Type: llm.SEMessageDelta, StopReason: StopReasonRefusal},
		{Type: llm.SEMessageStop},
	}
}

// A provider refusal must end the run with ReasonRefused after exactly one model
// call — not be reported as an empty "completed" turn, and not trigger any
// continuation (nudge / budget / retry) that would re-send the request.
func TestRefusalTerminatesWithoutRetry(t *testing.T) {
	m := &scriptedModel{turns: [][]llm.StreamEvent{
		refusalTurn(),
		textTurn("must not be reached"),
	}}
	in := QueryInput{Messages: []llm.Message{llm.UserText("go")}, Tools: tool.NewRegistry(),
		PermissionMode: permission.ModeBypass, MaxTurns: 10}
	term := drain(t, in, QueryDeps{CallModel: m.call})
	if term.Reason != ReasonRefused {
		t.Fatalf("reason = %q, want %q", term.Reason, ReasonRefused)
	}
	if m.calls != 1 {
		t.Fatalf("model called %d times, want 1 (refusal must not be retried)", m.calls)
	}
	if term.Err != nil {
		t.Fatalf("refusal is a terminal state, not an error: %v", term.Err)
	}
}

func TestRefusalTerminatesNonStreaming(t *testing.T) {
	m := &scriptedSyncModel{turns: []syncTurn{
		{msg: llm.Message{Role: llm.RoleAssistant}, stop: StopReasonRefusal},
		syncTextTurn("must not be reached"),
	}}
	in := QueryInput{Messages: []llm.Message{llm.UserText("go")}, Tools: tool.NewRegistry(),
		PermissionMode: permission.ModeBypass, MaxTurns: 10, NonStreaming: true}
	term := drain(t, in, QueryDeps{CallModelSync: m.call})
	if term.Reason != ReasonRefused || m.calls != 1 {
		t.Fatalf("reason=%q calls=%d, want refused after 1 call", term.Reason, m.calls)
	}
}
