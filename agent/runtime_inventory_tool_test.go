package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRuntimeInventoryReportsFoundAndMissing(t *testing.T) {
	t.Parallel()
	res, err := runtimeInventoryTool().Call(context.Background(), json.RawMessage(`{"category":"core"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("inventory failed: %s", res.Flatten())
	}
	var got struct {
		Category string            `json:"category"`
		Found    map[string]string `json:"found"`
		Missing  []string          `json:"missing"`
	}
	if err := json.Unmarshal([]byte(res.Flatten()), &got); err != nil {
		t.Fatalf("decode inventory: %v (%q)", err, res.Flatten())
	}
	if got.Category != "core" || len(got.Found)+len(got.Missing) == 0 {
		t.Fatalf("unexpected inventory: %+v", got)
	}
}

func TestRuntimeInventoryRejectsUnknownCategory(t *testing.T) {
	t.Parallel()
	res, err := runtimeInventoryTool().Call(context.Background(), json.RawMessage(`{"category":"nope"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatalf("unknown category accepted: %s", res.Flatten())
	}
}

func TestRuntimeInventoryPromptIsExplicit(t *testing.T) {
	t.Parallel()
	prompt := runtimeInventoryPrompt()
	if !strings.Contains(prompt, "Installed:") || !strings.Contains(prompt, "Not installed:") || !strings.Contains(prompt, "Do not invent") {
		t.Fatalf("runtime inventory prompt is incomplete: %q", prompt)
	}
}
