// Package attackpath deterministically ranks candidate exploration paths. It
// never executes a path or changes graph state.
package attackpath

import "sort"

type Candidate struct {
	ID              int64  `json:"id"`
	Summary         string `json:"summary"`
	BasePriority    int    `json:"base_priority"`
	ObservedParents int    `json:"observed_parents"`
	FindingParents  int    `json:"finding_parents"`
	InferredParents int    `json:"inferred_parents"`
	ScopeAnchored   bool   `json:"scope_anchored"`
	PriorFailures   int    `json:"prior_failures"`
	EstimatedCost   string `json:"estimated_cost,omitempty"`
}

type Ranked struct {
	Candidate
	Score      int            `json:"score"`
	Components map[string]int `json:"components"`
}

func Rank(candidates []Candidate) []Ranked {
	out := make([]Ranked, 0, len(candidates))
	for _, candidate := range candidates {
		components := map[string]int{
			"base_priority":     clamp(candidate.BasePriority, 0, 10) * 10,
			"observed_evidence": candidate.ObservedParents * 6,
			"finding_evidence":  candidate.FindingParents * 10,
			"inferred_evidence": candidate.InferredParents * 2,
			"scope_anchor":      0,
			"prior_failures":    -candidate.PriorFailures * 15,
			"estimated_cost":    costPenalty(candidate.EstimatedCost),
		}
		if candidate.ScopeAnchored {
			components["scope_anchor"] = 5
		}
		score := 0
		for _, value := range components {
			score += value
		}
		out = append(out, Ranked{Candidate: candidate, Score: score, Components: components})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].BasePriority != out[j].BasePriority {
			return out[i].BasePriority > out[j].BasePriority
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func costPenalty(cost string) int {
	switch cost {
	case "low":
		return 0
	case "medium":
		return -5
	case "high":
		return -15
	default:
		return 0
	}
}

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
