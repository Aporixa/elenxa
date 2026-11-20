package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Aporixa/elenxa/evidenceimport"
	acperm "github.com/Autumn-27/norma/permission"
	actool "github.com/Autumn-27/norma/tool"
)

const maxImportedObservations = 500

func (t *ToolSet) importScannerResults() actool.CoreTool {
	return actool.Build(actool.Spec{
		Name:        "import_scanner_results",
		Description: "Parse a task-local SARIF, Nmap XML, or Nuclei JSONL file into normalized provenance-bearing observations and write them as facts. The importer is deterministic; imported observations are not confirmed vulnerabilities.",
		Schema: obj(map[string]any{
			"format": str("sarif | nmap-xml | nuclei-jsonl"),
			"path":   str("Relative path under this run's working directory"),
		}, "format", "path"),
		Permissions: func(context.Context, json.RawMessage, acperm.Context) acperm.Decision { return acperm.Allowed() },
		Run: func(_ context.Context, input json.RawMessage, tc *actool.ToolContext) (actool.Result, error) {
			if t.ts == nil {
				return actool.Errorf("import_scanner_results requires a task context"), nil
			}
			var request struct{ Format, Path string }
			if err := json.Unmarshal(input, &request); err != nil {
				return actool.Errorf("invalid scanner import: " + err.Error()), nil
			}
			file, err := openTaskLocalFile(tc, request.Path)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			defer file.Close()
			parsed, err := evidenceimport.Parse(request.Format, request.Path, file)
			if err != nil {
				return actool.Errorf(err.Error()), nil
			}
			if len(parsed.Observations) > maxImportedObservations {
				return actool.Errorf(fmt.Sprintf("scanner result contains %d observations; maximum is %d", len(parsed.Observations), maxImportedObservations)), nil
			}
			ids := make([]int64, 0, len(parsed.Observations))
			for _, observation := range parsed.Observations {
				detail, _ := json.Marshal(observation)
				id, err := t.recordOneFact(factItem{
					Summary: fmt.Sprintf("[%s:%s] %s", parsed.Provenance.Format, observation.ID, observation.Title),
					Detail:  string(detail), Evidence: observation.Evidence, Confidence: observation.Confidence,
				}, t.ownerNode)
				if err != nil {
					return actool.Errorf("write imported observation: " + err.Error()), nil
				}
				ids = append(ids, id)
			}
			return jsonResult(map[string]any{"format": parsed.Provenance.Format, "source": request.Path, "digest": parsed.Provenance.Digest, "observations": len(ids), "fact_ids": ids})
		},
	})
}

func openTaskLocalFile(tc *actool.ToolContext, requested string) (*os.File, error) {
	if tc == nil || strings.TrimSpace(tc.WorkingDir) == "" {
		return nil, fmt.Errorf("task working directory is unavailable")
	}
	requested = strings.TrimSpace(requested)
	if requested == "" || filepath.IsAbs(requested) {
		return nil, fmt.Errorf("path must be a non-empty relative task-local path")
	}
	root, err := filepath.Abs(tc.WorkingDir)
	if err != nil {
		return nil, err
	}
	cleaned := filepath.Clean(requested)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("path escapes the task working directory")
	}
	file, err := openConfinedFile(root, cleaned)
	if err != nil {
		return nil, fmt.Errorf("open task-local scanner result: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("path must name a regular file")
	}
	if info.Size() > evidenceimport.MaxInputBytes {
		file.Close()
		return nil, fmt.Errorf("scanner result exceeds %d bytes", evidenceimport.MaxInputBytes)
	}
	return file, nil
}
