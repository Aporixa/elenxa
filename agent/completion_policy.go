package agent

import (
	"fmt"
	"strings"
	"time"
)

// openEndedOptimizationGoal identifies goals whose success criterion is to keep
// improving a result rather than to reach a finite, independently verifiable
// state. Such a goal cannot be proven complete merely because the planner has no
// current lead; for a timed task, the budget boundary is the stopping condition.
func openEndedOptimizationGoal(goal string) bool {
	g := strings.ToLower(strings.Join(strings.Fields(goal), " "))
	if g == "" {
		return false
	}
	if strings.Contains(g, "as many") && strings.Contains(g, "as possible") {
		return true
	}
	if strings.Contains(g, "as much") && strings.Contains(g, "as possible") {
		return true
	}
	for _, phrase := range []string{
		"maximize findings", "maximise findings",
		"maximize vulnerabilities", "maximise vulnerabilities",
		"maximum number of findings", "maximum number of vulnerabilities",
		"until the time budget", "until time expires", "for the full duration",
	} {
		if strings.Contains(g, phrase) {
			return true
		}
	}
	return false
}

// completionBlockedUntilBudget prevents a normal planner round from declaring a
// timed, open-ended optimization complete before the task deadline. The deadline
// coordinator's final planner round is explicitly allowed to close the goal.
func completionBlockedUntilBudget(goal string, clock TaskClock, now time.Time) (bool, string) {
	if clock.Final || clock.DeadlineUnix <= 0 || !openEndedOptimizationGoal(goal) {
		return false, ""
	}
	deadline := time.Unix(clock.DeadlineUnix, 0)
	if !now.Before(deadline) {
		return false, ""
	}
	remaining := deadline.Sub(now).Round(time.Second)
	return true, fmt.Sprintf("completion blocked: this is an open-ended optimization goal with %s remaining in its configured task budget; lack of a current lead is not proof of completion. Keep independent worker capacity filled, revisit incompletely tested surfaces, or wait for the deadline coordinator's final judgment", remaining)
}
