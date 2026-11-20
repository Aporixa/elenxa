package agent

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Aporixa/elenxa/attackpath"
	"github.com/Aporixa/elenxa/db"
	actool "github.com/Autumn-27/norma/tool"
)

func (t *ToolSet) attackPathRanking(limit int) ([]attackpath.Ranked, error) {
	if limit <= 0 {
		limit = 5
	}
	if limit > 20 {
		limit = 20
	}
	openCount, err := t.ts.CountOpenIntents()
	if err != nil {
		return nil, err
	}
	if openCount == 0 {
		return []attackpath.Ranked{}, nil
	}
	open, err := t.ts.Frontier(openCount)
	if err != nil {
		return nil, err
	}
	openIDs := make([]int64, 0, len(open))
	for _, node := range open {
		openIDs = append(openIDs, node.ID)
	}
	var edges []db.Edge
	seenEdges := map[db.Edge]bool{}
	for start := 0; start < len(openIDs); start += 1000 {
		end := min(start+1000, len(openIDs))
		chunk, edgeErr := t.ts.EdgesTouching(openIDs[start:end])
		if edgeErr != nil {
			return nil, edgeErr
		}
		for _, edge := range chunk {
			if !seenEdges[edge] {
				edges = append(edges, edge)
				seenEdges[edge] = true
			}
		}
	}
	parents := map[int64][]int64{}
	for _, edge := range edges {
		if edge.Rel == db.RelDerivedFrom || edge.Rel == db.RelSpawns {
			parents[edge.To] = append(parents[edge.To], edge.From)
		}
	}
	failures := map[string]int{}
	before := int64(0)
	for {
		intents, more, pageErr := t.ts.ListByKindPage(db.KindIntent, before, 500)
		if pageErr != nil {
			return nil, pageErr
		}
		for _, node := range intents {
			before = node.ID
			if node.State != "blocked" && node.State != "exhausted" {
				continue
			}
			var payload struct {
				Summary string `json:"summary"`
			}
			_ = json.Unmarshal(node.Payload, &payload)
			if key := normalizeIntentSummary(payload.Summary); key != "" {
				failures[key]++
			}
		}
		if !more || len(intents) == 0 {
			break
		}
	}
	candidates := make([]attackpath.Candidate, 0, len(open))
	for _, node := range open {
		var payload struct {
			Summary       string            `json:"summary"`
			AssetIDs      []json.RawMessage `json:"asset_ids"`
			EstimatedCost string            `json:"estimated_cost"`
		}
		_ = json.Unmarshal(node.Payload, &payload)
		candidate := attackpath.Candidate{
			ID: node.ID, Summary: payload.Summary, BasePriority: node.Priority,
			ScopeAnchored: len(payload.AssetIDs) > 0, PriorFailures: failures[normalizeIntentSummary(payload.Summary)],
			EstimatedCost: payload.EstimatedCost,
		}
		for _, parentID := range parents[node.ID] {
			parent, _ := t.ts.GetNodeWithSources(parentID)
			if parent == nil {
				continue
			}
			if parent.Kind == db.KindFinding {
				candidate.FindingParents++
				continue
			}
			if parent.Kind != db.KindFact {
				continue
			}
			var evidence struct {
				Confidence string `json:"confidence"`
			}
			_ = json.Unmarshal(parent.Payload, &evidence)
			if evidence.Confidence == "inferred" {
				candidate.InferredParents++
			} else if evidence.Confidence == "observed" {
				candidate.ObservedParents++
			}
		}
		candidates = append(candidates, candidate)
	}
	ranked := attackpath.Rank(candidates)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked, nil
}

func normalizeIntentSummary(summary string) string {
	return strings.Join(strings.Fields(strings.ToLower(summary)), " ")
}

func (t *ToolSet) rankAttackPaths() actool.CoreTool {
	return t.readExpTool("rank_attack_paths", "Rank open exploration paths deterministically from stored priority, confirmed parent evidence, scope anchors, explicit estimated cost, and prior identical blocked/exhausted intents. This is advisory and does not execute or reorder work.",
		obj(map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 20, "description": "Maximum ranked candidates (default 5)"}}),
		func(_ context.Context, input json.RawMessage) (actool.Result, error) {
			var request struct {
				Limit int `json:"limit"`
			}
			_ = json.Unmarshal(input, &request)
			ranked, err := t.attackPathRanking(request.Limit)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			return jsonResult(map[string]any{
				"method": "priority_evidence_scope_cost_prior_failure_v1", "ranked": ranked,
				"limits": []string{"Exact normalized summaries identify prior repeated failures; semantic duplicates are not inferred.", "Only explicit estimated_cost values affect cost scoring."},
			})
		})
}
