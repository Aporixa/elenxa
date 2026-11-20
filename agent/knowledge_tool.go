package agent

import (
	"context"
	"encoding/json"

	"github.com/Aporixa/elenxa/knowledge"
	"github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

func researchKnowledgeTool() actool.CoreTool {
	return actool.Build(actool.Spec{
		Name:        "search_research_knowledge",
		Description: "Search Elenxa's versioned local technique and lab-control catalog. Returns a small provenance-bearing slice; makes no network or model calls. Use before pursuing an unfamiliar AI-target, deception, oracle, or repeated network-failure path.",
		Schema: obj(map[string]any{
			"query":       str("Terms describing the target class, symptom, technique, or control question"),
			"max_results": map[string]any{"type": "integer", "minimum": 1, "maximum": 5, "description": "Maximum records (default 3)"},
			"max_chars":   map[string]any{"type": "integer", "minimum": 500, "maximum": 8000, "description": "Maximum encoded match characters (default 4000)"},
		}, "query"),
		ReadOnly:   func(json.RawMessage) bool { return true },
		Concurrent: func(json.RawMessage) bool { return true },
		Permissions: func(context.Context, json.RawMessage, permission.Context) permission.Decision {
			return permission.Allowed()
		},
		Run: func(_ context.Context, input json.RawMessage, _ *actool.ToolContext) (actool.Result, error) {
			var request struct {
				Query      string `json:"query"`
				MaxResults int    `json:"max_results"`
				MaxChars   int    `json:"max_chars"`
			}
			if err := json.Unmarshal(input, &request); err != nil {
				return actool.Errorf("invalid knowledge query: " + err.Error()), nil
			}
			catalog, err := knowledge.Builtin()
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			result, err := catalog.Search(request.Query, request.MaxResults, request.MaxChars)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			raw, err := json.Marshal(result)
			if err != nil {
				return actool.Errorf("encode knowledge result: " + err.Error()), nil
			}
			return actool.Text(string(raw)), nil
		},
	})
}
