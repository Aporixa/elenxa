package benchmark

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Aporixa/elenxa/researchmetrics"
)

type Runner struct {
	client *ElenxaClient
}

func NewRunner(scenario Scenario) (*Runner, error) {
	client, err := NewElenxaClient(scenario.Elenxa)
	if err != nil {
		return nil, err
	}
	return &Runner{client: client}, nil
}

func (r *Runner) Run(ctx context.Context, scenario Scenario, trial int) (result Result) {
	result = Result{
		SchemaVersion: SchemaVersion, ScenarioID: scenario.ID, Trial: trial,
		StartedAt: time.Now().UTC(), Egress: []EgressResult{}, Assertions: []Assertion{},
	}
	env := map[string]string{
		"ELENXA_BENCH_SCENARIO": scenario.ID,
		"ELENXA_BENCH_TRIAL":    strconv.Itoa(trial),
	}
	if scenario.Reset != nil {
		if _, err := runCommand(ctx, *scenario.Reset, env); err != nil {
			result.Error = "reset: " + err.Error()
			result.FinishedAt = time.Now().UTC()
			return result
		}
	}
	var fixture *fixtureInstall
	if scenario.Fixture != nil {
		result.FixtureID = scenario.Fixture.ID
		var err error
		fixture, err = installFixture(ctx, *scenario.Fixture)
		if err != nil {
			result.Error = err.Error()
			result.FinishedAt = time.Now().UTC()
			return result
		}
		defer func() {
			if err := fixture.Cleanup(context.Background()); err != nil {
				if result.Error != "" {
					result.Error += "; "
				}
				result.Error += err.Error()
				result.Passed = false
			}
		}()
		for key, value := range fixture.env() {
			env[key] = value
		}
	}
	runStarted := time.Now().UTC()
	taskID, err := r.client.CreateTask(ctx, scenario.Task)
	result.TaskID = taskID
	env["ELENXA_BENCH_TASK_ID"] = taskID
	if err != nil {
		result.Error = err.Error()
		result.FinishedAt = time.Now().UTC()
		return result
	}
	maxRun := time.Duration(scenario.Elenxa.MaxRunSeconds) * time.Second
	if maxRun <= 0 {
		maxRun = 2 * time.Hour
	}
	runCtx, cancel := context.WithTimeout(ctx, maxRun)
	poll := time.Duration(scenario.Elenxa.PollSeconds) * time.Second
	result.TaskStatus, err = r.client.WaitTask(runCtx, taskID, poll)
	cancel()
	if err != nil {
		result.Error = "wait task: " + err.Error()
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		if pauseErr := r.client.PauseTask(cleanupCtx, taskID); pauseErr != nil {
			appendResultError(&result, "pause timed-out task: "+pauseErr.Error())
		}
		cleanupCancel()
	}
	metrics, metricsErr := r.client.ResearchMetrics(ctx, taskID)
	if metricsErr != nil {
		appendResultError(&result, "metrics: "+metricsErr.Error())
	} else {
		result.Metrics = metrics
	}
	result.FinishedAt = time.Now().UTC()
	result.Oracle = evaluateOracle(ctx, scenario.Oracle, env)
	if len(result.Metrics) > 0 {
		var report researchmetrics.Report
		if json.Unmarshal(result.Metrics, &report) == nil {
			score := researchmetrics.Score(researchmetrics.ScoreInput{
				OraclePassed: result.Oracle.Passed,
				Duration:     result.FinishedAt.Sub(runStarted),
				MaxDuration:  time.Duration(scenario.Elenxa.MaxRunSeconds) * time.Second,
				Report:       report,
			})
			result.Score = &score
		}
	}
	for _, adapter := range scenario.Egress {
		result.Egress = append(result.Egress, collectEgress(ctx, r.client, adapter, scenario.Task.Scope, runStarted, result.FinishedAt, env))
	}
	result.Assertions = evaluateAssertions(scenario, result)
	result.Passed = result.Error == ""
	for _, assertion := range result.Assertions {
		result.Passed = result.Passed && assertion.Passed
	}
	return result
}

func appendResultError(result *Result, message string) {
	if result.Error != "" {
		result.Error += "; "
	}
	result.Error += message
}

func evaluateAssertions(scenario Scenario, result Result) []Assertion {
	assertions := []Assertion{
		{Name: "task_completed", Passed: result.TaskStatus == "done", Expected: "done", Actual: result.TaskStatus},
		{Name: "oracle_passed", Passed: result.Oracle.Passed, Expected: "true", Actual: strconv.FormatBool(result.Oracle.Passed)},
	}
	var metrics struct {
		Summary struct {
			DeviationRate float64        `json:"deviation_rate"`
			Scope         map[string]int `json:"scope"`
		} `json:"summary"`
		Events []struct {
			EventType string `json:"event_type"`
		} `json:"events"`
	}
	metricsRequired := scenario.Expectations.MaxScopeDeviation != nil || scenario.Expectations.MaxUnresolvedNetwork != nil || len(scenario.Expectations.RequiredEventTypes) > 0
	metricsOK := len(result.Metrics) > 0 && json.Unmarshal(result.Metrics, &metrics) == nil
	if metricsRequired && !metricsOK {
		assertions = append(assertions, Assertion{Name: "research_metrics_available", Passed: false, Expected: "valid metrics", Actual: "missing or invalid"})
	}
	if metricsOK {
		if max := scenario.Expectations.MaxScopeDeviation; max != nil {
			assertions = append(assertions, Assertion{
				Name: "max_scope_deviation", Passed: metrics.Summary.DeviationRate <= *max,
				Expected: fmt.Sprintf("<= %.6f", *max), Actual: fmt.Sprintf("%.6f", metrics.Summary.DeviationRate),
			})
		}
		if max := scenario.Expectations.MaxUnresolvedNetwork; max != nil {
			actual := metrics.Summary.Scope["unresolved"]
			assertions = append(assertions, Assertion{
				Name: "max_unresolved_network", Passed: actual <= *max,
				Expected: fmt.Sprintf("<= %d", *max), Actual: strconv.Itoa(actual),
			})
		}
		seen := map[string]bool{}
		for _, event := range metrics.Events {
			seen[event.EventType] = true
		}
		for _, required := range scenario.Expectations.RequiredEventTypes {
			assertions = append(assertions, Assertion{
				Name: "required_event:" + required, Passed: seen[required], Expected: "present", Actual: present(seen[required]),
			})
		}
	}
	if max := scenario.Expectations.MaxOutOfScopeEgress; max != nil {
		actual := 0
		for _, egress := range result.Egress {
			actual += egress.OutOfScope
		}
		assertions = append(assertions, Assertion{
			Name: "max_out_of_scope_egress", Passed: actual <= *max,
			Expected: fmt.Sprintf("<= %d", *max), Actual: strconv.Itoa(actual),
		})
	}
	return assertions
}

func present(value bool) string {
	if value {
		return "present"
	}
	return "missing"
}

func ResultFilename(scenarioID string, trial int, started time.Time) string {
	safe := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, scenarioID)
	return fmt.Sprintf("%s-trial-%03d-%s.json", safe, trial, started.UTC().Format("20060102T150405Z"))
}
