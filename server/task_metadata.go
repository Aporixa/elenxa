package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/Aporixa/elenxa/db"
)

const (
	maxTaskNameRunes           = 200
	maxTaskMetadataRequestSize = 16 << 10
)

// updateTaskMetadata changes list-only task metadata. It intentionally does not
// touch lifecycle state, scheduling, or the task's immutable description/goal.
func (s *Server) updateTaskMetadata(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	if _, ok := s.m.Task(taskID); !ok {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxTaskMetadataRequestSize)
	var request struct {
		Name             *string              `json:"name"`
		Pinned           *bool                `json:"pinned"`
		EngagementPolicy *db.EngagementPolicy `json:"engagement_policy"`
	}
	if err := decode(r, &request); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeErr(w, http.StatusRequestEntityTooLarge, "Request body too large")
		} else {
			writeErr(w, http.StatusBadRequest, err.Error())
		}
		return
	}
	if request.Name == nil && request.Pinned == nil && request.EngagementPolicy == nil {
		writeErr(w, http.StatusBadRequest, "At least one of name, pinned, or engagement_policy is required")
		return
	}
	if request.Name != nil {
		name := strings.TrimSpace(*request.Name)
		if name == "" {
			writeErr(w, http.StatusBadRequest, "Task name cannot be empty")
			return
		}
		if utf8.RuneCountInString(name) > maxTaskNameRunes {
			writeErr(w, http.StatusBadRequest, fmt.Sprintf("Task name can be at most %d characters", maxTaskNameRunes))
			return
		}
		request.Name = &name
	}
	if request.EngagementPolicy != nil {
		policy, err := db.NormalizeEngagementPolicy(*request.EngagementPolicy)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "Invalid engagement policy: "+err.Error())
			return
		}
		request.EngagementPolicy = &policy
	}
	task, err := s.m.UpdateTaskMetadata(taskID, db.TaskPatch{Name: request.Name, Pinned: request.Pinned, EngagementPolicy: request.EngagementPolicy})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if task == nil {
		writeErr(w, http.StatusNotFound, "task not found")
		return
	}
	if request.EngagementPolicy != nil {
		s.invalidateTaskAgent(taskID)
	}
	writeJSON(w, http.StatusOK, taskDTO(task, s.resolvedTaskStatus(task)))
}
