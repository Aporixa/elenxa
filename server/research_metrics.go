package server

import (
	"net/http"
	"strconv"

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
	assets := s.m.Assets()
	if assets == nil {
		writeErr(w, http.StatusServiceUnavailable, "asset store is not enabled")
		return
	}
	scope, err := assets.ListTaskScope(taskID)
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
	}
	writeJSON(w, http.StatusOK, researchmetrics.Analyze(metricCalls, metricScope))
}
