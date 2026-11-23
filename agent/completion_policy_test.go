package agent

import (
	"strings"
	"testing"
	"time"
)

func TestOpenEndedOptimizationGoal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		goal string
		want bool
	}{
		{"Produce findings for as many distinct vulnerabilities as possible", true},
		{"Maximize findings during the assessment", true},
		{"Continue for the full duration", true},
		{"Capture 120 challenge flags", false},
		{"Reach 90% endpoint coverage", false},
		{"Obtain administrator access", false},
		{"Complete the task as quickly as possible", false},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.goal, func(t *testing.T) {
			t.Parallel()
			if got := openEndedOptimizationGoal(tt.goal); got != tt.want {
				t.Fatalf("openEndedOptimizationGoal()=%v, want %v", got, tt.want)
			}
		})
	}
}

func TestCompletionBlockedUntilBudget(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_800_000_000, 0)
	clock := TaskClock{DeadlineUnix: now.Add(45 * time.Minute).Unix()}
	blocked, reason := completionBlockedUntilBudget("Find as many confirmed vulnerabilities as possible", clock, now)
	if !blocked || !strings.Contains(reason, "45m") {
		t.Fatalf("want budget guard with remaining time, got blocked=%v reason=%q", blocked, reason)
	}

	for name, candidate := range map[string]struct {
		goal  string
		clock TaskClock
		now   time.Time
	}{
		"concrete goal":    {"Capture 10 flags", clock, now},
		"no deadline":      {"Find as many as possible", TaskClock{}, now},
		"final round":      {"Find as many as possible", TaskClock{DeadlineUnix: clock.DeadlineUnix, Final: true}, now},
		"deadline reached": {"Find as many as possible", clock, now.Add(46 * time.Minute)},
	} {
		if blocked, _ := completionBlockedUntilBudget(candidate.goal, candidate.clock, candidate.now); blocked {
			t.Errorf("%s unexpectedly blocked", name)
		}
	}
}
