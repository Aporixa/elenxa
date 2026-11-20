package server

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/Aporixa/elenxa/db"
	"github.com/Aporixa/elenxa/researchmetrics"
)

func (s *Server) taskResearchMetrics(w http.ResponseWriter, r *http.Request) {
	task, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	taskID, err := strconv.ParseInt(task.ID, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid task id")
		return
	}
	calls, err := s.m.pg.ResearchToolCalls(taskID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	controls, err := s.m.pg.ResearchControlEvents(taskID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	assets := s.m.Assets()
	if assets == nil {
		writeErr(w, http.StatusServiceUnavailable, "asset store is not enabled")
		return
	}
	scope, err := assets.ListTaskScopeWithSources(taskID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	metricCalls := make([]researchmetrics.ToolCall, 0, len(calls))
	for _, call := range calls {
		metricCalls = append(metricCalls, researchmetrics.ToolCall{
			ActivityID: call.ActivityID, NodeID: call.NodeID, Worker: call.Worker,
			Tool: call.Tool, ToolUseID: call.ToolUseID, Input: call.Input,
			AttemptedAt: call.AttemptedAt, Result: call.Result, IsError: call.IsError, FinishedAt: call.FinishedAt,
		})
	}
	metricScope := make([]researchmetrics.ScopeEntry, 0, len(scope))
	for _, entry := range scope {
		metricScope = append(metricScope, researchmetrics.ScopeEntry{
			ID: entry.ID, Kind: entry.Kind, Domain: entry.Domain, Net: entry.Net, Source: entry.Source, Reason: entry.Reason,
		})
		if entry.Kind != "company" || entry.CompanyID == nil {
			continue
		}
		rules, scopeErr := s.m.pg.Companies().GetScope(*entry.CompanyID)
		if scopeErr != nil {
			writeErr(w, http.StatusInternalServerError, scopeErr.Error())
			return
		}
		for _, rule := range rules {
			kind := rule.Kind
			if kind == "domain" {
				kind = "root_domain"
			}
			metricScope = append(metricScope, researchmetrics.ScopeEntry{
				ID: rule.ID, Kind: kind, Domain: rule.Domain, Net: rule.Net,
				Source: "company", Reason: rule.Reason,
			})
		}
	}
	metricControls := make([]researchmetrics.ControlEvent, 0, len(controls))
	for _, control := range controls {
		metricControls = append(metricControls, researchmetrics.ControlEvent{
			ActivityID: control.ActivityID, Worker: control.Worker, Metadata: control.Metadata,
		})
	}
	report := researchmetrics.AnalyzeWithControls(metricCalls, metricScope, metricControls)
	report.Context, err = s.researchBenchmarkContext(task, taskID, assets)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) researchBenchmarkContext(task *Task, taskID int64, assets interface {
	ListTaskInterceptRules(int64) ([]db.AssetInterceptRule, error)
}) (researchmetrics.BenchmarkContext, error) {
	state := task.llmStateSnapshot()
	ctx := researchmetrics.BenchmarkContext{
		Task: researchmetrics.TaskContext{
			ID: task.ID, Description: task.Description, Goal: task.Goal,
			BuildVersion: BuildVersion, LLMProfileIDs: state.ProfileIDs, ActiveLLMProfileID: state.ActiveID,
		},
		Models: []researchmetrics.ModelContext{}, Agents: []researchmetrics.AgentContext{},
		Constraints: []researchmetrics.Constraint{},
		Controls: researchmetrics.ControlsContext{
			ToolRules: []researchmetrics.ToolControl{}, GlobalAssetRules: []researchmetrics.AssetControl{},
			TaskAssetRules: []researchmetrics.AssetControl{},
		},
	}
	for _, role := range []string{"mainagent", "planner", "worker"} {
		resolved, err := s.resolveTaskRoleLLM(task, role)
		if err != nil {
			return ctx, fmt.Errorf("resolve %s model: %w", role, err)
		}
		ctx.Models = append(ctx.Models, researchmetrics.ModelContext{
			Role: role, ProfileID: resolved.ProfileID, Name: resolved.Name, Format: resolved.Format,
			Model: resolved.Model, Source: resolved.Source, Available: resolved.Available, Reason: resolved.Reason,
		})
		agent, err := s.m.pg.GetAgentByKey(role)
		if err != nil {
			return ctx, fmt.Errorf("load %s agent: %w", role, err)
		}
		if agent == nil {
			continue
		}
		skills, err := s.m.pg.AgentSkillNames(agent.ID)
		if err != nil {
			return ctx, fmt.Errorf("load %s skills: %w", role, err)
		}
		ctx.Agents = append(ctx.Agents, researchmetrics.AgentContext{
			Key: role, Enabled: agent.Enabled, InteractiveShell: agent.InteractiveShell, Skills: skills,
		})
	}
	constraints, err := task.Store.ListConstraints()
	if err != nil {
		return ctx, fmt.Errorf("load constraints: %w", err)
	}
	for _, constraint := range constraints {
		ctx.Constraints = append(ctx.Constraints, researchmetrics.Constraint{
			Kind: constraint.Kind, Text: constraint.Text, Origin: constraint.Origin,
		})
	}
	toolRules, err := s.m.pg.ListInterceptRules()
	if err != nil {
		return ctx, fmt.Errorf("load tool controls: %w", err)
	}
	for _, rule := range toolRules {
		if rule.Enabled {
			ctx.Controls.ToolRules = append(ctx.Controls.ToolRules, researchmetrics.ToolControl{
				Name: rule.Name, Priority: rule.Priority, MatchTarget: rule.MatchTarget, MatchType: rule.MatchType,
				Pattern: rule.Pattern, Action: rule.Action, Message: rule.Message,
			})
		}
	}
	globalAssetRules, err := s.m.pg.ListAssetInterceptRules()
	if err != nil {
		return ctx, fmt.Errorf("load global asset controls: %w", err)
	}
	for _, rule := range globalAssetRules {
		if rule.Enabled {
			ctx.Controls.GlobalAssetRules = append(ctx.Controls.GlobalAssetRules, researchmetrics.AssetControl{
				Kind: rule.Kind, Pattern: rule.Pattern, Note: rule.Note,
			})
		}
	}
	taskAssetRules, err := assets.ListTaskInterceptRules(taskID)
	if err != nil {
		return ctx, fmt.Errorf("load task asset controls: %w", err)
	}
	for _, rule := range taskAssetRules {
		if rule.Enabled {
			ctx.Controls.TaskAssetRules = append(ctx.Controls.TaskAssetRules, researchmetrics.AssetControl{
				Action: rule.Action, Kind: rule.Kind, Pattern: rule.Pattern, Note: rule.Note,
			})
		}
	}
	return ctx, nil
}
