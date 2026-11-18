package agent

import (
	"context"

	"github.com/Aporixa/elenxa/db"
)

// FindingRecorder is injected by the host; agents never synthesize or copy
// evidence bodies themselves. Its implementation owns the atomic write.
type FindingRecorder interface {
	Record(context.Context, db.RecordFindingInput, []db.TrafficRef) (*db.RecordedFinding, error)
}

// Tool-use guidance is appended without replacing the user's editable prompt.
// It does not require capture or claim that unavailable traffic tools exist.
const findingTrafficGuidance = "\n\n**Finding traffic evidence (optional)**: when calling report_finding to report a finding, if there are HTTP requests/responses you have viewed and confirmed to support the finding's conclusion, you can bind their real IDs in reproduction order via traffic_refs; domain and time are only for filtering candidates and do not imply a link. For TCP and other non-HTTP findings, nothing captured or no exact match, omit it or pass []; keep other verifiable evidence such as command output and logs in evidence, and preferably state why nothing was bound. Do not guess IDs, and do not re-probe just to capture packets."

func (t *ToolSet) SetFindingRecorder(r FindingRecorder)   { t.findingRecorder = r }
func (w *Worker) SetFindingRecorder(r FindingRecorder)    { w.findingRecorder = r }
func (p *Planner) SetFindingRecorder(r FindingRecorder)   { p.findingRecorder = r }
func (m *MainAgent) SetFindingRecorder(r FindingRecorder) { m.findingRecorder = r }
