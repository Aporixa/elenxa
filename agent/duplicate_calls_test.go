package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Autumn-27/norma/llm"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

type countingTool struct{ calls int }

func (t *countingTool) Name() string                           { return "probe" }
func (t *countingTool) Description() string                    { return "probe" }
func (t *countingTool) Prompt() string                         { return "" }
func (t *countingTool) InputSchema() map[string]any            { return map[string]any{"type": "object"} }
func (t *countingTool) IsReadOnly(json.RawMessage) bool        { return false }
func (t *countingTool) IsConcurrencySafe(json.RawMessage) bool { return false }
func (t *countingTool) CheckPermissions(context.Context, json.RawMessage, permission.Context) permission.Decision {
	return permission.Allowed()
}
func (t *countingTool) Call(context.Context, json.RawMessage, *actool.ToolContext) (actool.Result, error) {
	t.calls++
	return actool.Text("ran"), nil
}

func TestDuplicateCallGuardWarnsThenBlocks(t *testing.T) {
	base := &countingTool{}
	tool := guardDuplicateCalls(base, &duplicateCallGuard{})
	inputs := []json.RawMessage{
		json.RawMessage(`{"b":2,"a":1}`),
		json.RawMessage(`{"a":1,"b":2}`),
		json.RawMessage(`{"a":1,"b":2}`),
	}
	for i, input := range inputs {
		result, err := tool.Call(context.Background(), input, nil)
		if err != nil {
			t.Fatalf("call %d: %v", i+1, err)
		}
		switch i {
		case 0:
			if result.IsError {
				t.Fatal("first call was blocked")
			}
		case 1:
			if result.IsError || len(result.Content) != 2 || result.Content[1].Type != llm.BlockText {
				t.Fatalf("second call did not carry warning: %+v", result)
			}
		case 2:
			if !result.IsError {
				t.Fatal("third identical call was not blocked")
			}
		}
	}
	if base.calls != 2 {
		t.Fatalf("underlying tool called %d times, want 2", base.calls)
	}
}

func TestDuplicateCallGuardResetsAfterDifferentCall(t *testing.T) {
	base := &countingTool{}
	guard := &duplicateCallGuard{}
	tool := guardDuplicateCalls(base, guard)
	for _, input := range []string{`{"n":1}`, `{"n":1}`, `{"n":2}`, `{"n":1}`} {
		result, err := tool.Call(context.Background(), json.RawMessage(input), nil)
		if err != nil || result.IsError {
			t.Fatalf("input %s unexpectedly blocked: result=%+v err=%v", input, result, err)
		}
	}
	if base.calls != 4 {
		t.Fatalf("underlying tool called %d times, want 4", base.calls)
	}
}
