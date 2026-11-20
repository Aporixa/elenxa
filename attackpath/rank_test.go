package attackpath

import "testing"

func TestRankUsesEvidenceScopeCostAndPriorFailure(t *testing.T) {
	ranked := Rank([]Candidate{
		{ID: 2, Summary: "repeat", BasePriority: 8, PriorFailures: 2, EstimatedCost: "high"},
		{ID: 1, Summary: "grounded", BasePriority: 6, ObservedParents: 2, FindingParents: 1, ScopeAnchored: true, EstimatedCost: "low"},
	})
	if len(ranked) != 2 || ranked[0].ID != 1 {
		t.Fatalf("ranked=%+v", ranked)
	}
	if ranked[0].Components["finding_evidence"] != 10 || ranked[1].Components["prior_failures"] != -30 {
		t.Fatalf("components=%+v", ranked)
	}
}

func TestRankTieBreaksByPriorityThenID(t *testing.T) {
	ranked := Rank([]Candidate{{ID: 9, BasePriority: 5}, {ID: 3, BasePriority: 5}})
	if ranked[0].ID != 3 {
		t.Fatalf("ranked=%+v", ranked)
	}
}
