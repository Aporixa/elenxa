package db

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// ResearchToolCall is a model-proposed tool call paired with its eventual
// result. The activity ledger is authoritative: tool_use is written before
// execution, so permission and hook denials remain visible as attempts.
type ResearchToolCall struct {
	ActivityID  int64      `json:"activity_id"`
	NodeID      *int64     `json:"node_id,omitempty"`
	Worker      string     `json:"worker,omitempty"`
	Tool        string     `json:"tool"`
	ToolUseID   string     `json:"tool_use_id"`
	Input       string     `json:"input"`
	AttemptedAt time.Time  `json:"attempted_at"`
	Result      string     `json:"result,omitempty"`
	IsError     bool       `json:"is_error"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

// ResearchControlEvent is a host-recorded benchmark event that did not come
// from a model tool call (for example, a provider refusal).
type ResearchControlEvent struct {
	ActivityID int64           `json:"activity_id"`
	Worker     string          `json:"worker,omitempty"`
	Metadata   json.RawMessage `json:"metadata"`
	CreatedAt  time.Time       `json:"created_at"`
}

func (d *DB) ResearchControlEvents(taskID int64) ([]ResearchControlEvent, error) {
	rows, err := d.Query(`
SELECT a.id, COALESCE(a.worker,''), a.metadata, a.created_at
FROM activity a
JOIN tasks t ON t.exploration_id=a.exploration_id
WHERE t.id=$1 AND t.deleted_at IS NULL AND a.kind='control'
ORDER BY a.id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ResearchControlEvent{}
	for rows.Next() {
		var event ResearchControlEvent
		if err := rows.Scan(&event.ActivityID, &event.Worker, &event.Metadata, &event.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, rows.Err()
}

// ResearchToolCalls returns every proposed tool call for a task, including
// calls rejected before the underlying tool ran. Duplicate tool_use IDs are
// isolated by worker and node because separate agent sessions may reuse IDs.
func (d *DB) ResearchToolCalls(taskID int64) ([]ResearchToolCall, error) {
	var explorationID int64
	if err := d.QueryRow(`SELECT exploration_id FROM tasks WHERE id=$1 AND deleted_at IS NULL`, taskID).Scan(&explorationID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	rows, err := d.Query(`
SELECT u.id, u.node_id, COALESCE(u.worker,''), COALESCE(u.tool,''), COALESCE(u.tool_use_id,''),
       COALESCE(u.detail,''), u.created_at,
       COALESCE(r.detail,''), COALESCE(r.is_error,false), r.created_at
FROM activity u
LEFT JOIN LATERAL (
    SELECT detail, is_error, created_at
    FROM activity r
    WHERE r.exploration_id=u.exploration_id
      AND r.kind='tool_result'
      AND u.tool_use_id<>''
      AND r.tool_use_id=u.tool_use_id
      AND COALESCE(r.worker,'')=COALESCE(u.worker,'')
      AND r.node_id IS NOT DISTINCT FROM u.node_id
      AND r.main_seg IS NOT DISTINCT FROM u.main_seg
      AND r.id>u.id
      AND NOT EXISTS (
          SELECT 1 FROM activity next_u
          WHERE next_u.exploration_id=u.exploration_id
            AND next_u.kind='tool_use'
            AND next_u.tool_use_id=u.tool_use_id
            AND COALESCE(next_u.worker,'')=COALESCE(u.worker,'')
            AND next_u.node_id IS NOT DISTINCT FROM u.node_id
            AND next_u.main_seg IS NOT DISTINCT FROM u.main_seg
            AND next_u.id>u.id AND next_u.id<r.id
      )
    ORDER BY r.id
    LIMIT 1
) r ON true
WHERE u.exploration_id=$1 AND u.kind='tool_use'
ORDER BY u.id`, explorationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ResearchToolCall{}
	for rows.Next() {
		var call ResearchToolCall
		var finished sql.NullTime
		if err := rows.Scan(&call.ActivityID, &call.NodeID, &call.Worker, &call.Tool, &call.ToolUseID,
			&call.Input, &call.AttemptedAt, &call.Result, &call.IsError, &finished); err != nil {
			return nil, err
		}
		if finished.Valid {
			call.FinishedAt = &finished.Time
		}
		out = append(out, call)
	}
	return out, rows.Err()
}
