package agent

import (
	"strings"
	"testing"

	"github.com/Aporixa/elenxa/db"
)

func TestGraphScopeRowsAreBounded(t *testing.T) {
	rows := make([]db.TaskScope, 75)
	for i := range rows {
		rows[i] = db.TaskScope{ID: int64(i + 1), TaskID: 1, Kind: "keyword", Value: strings.Repeat("x", 400), Source: "agent"}
	}
	rendered, truncated := (&ToolSet{taskID: 1}).graphScopeRows(rows)
	if !truncated || len(rendered) != 50 {
		t.Fatalf("rows=%d truncated=%v", len(rendered), truncated)
	}
	if value := rendered[0]["value"].(string); len([]rune(value)) > 257 {
		t.Fatalf("scope value was not bounded: %d runes", len([]rune(value)))
	}
}
