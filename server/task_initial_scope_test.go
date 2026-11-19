package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestCreateTaskInstallsInitialScopeBeforeLaunch(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := New(ctx, m, t.TempDir(), t.TempDir(), t.TempDir())
	s.archiveWG.Wait()
	<-s.side.done

	body := `{"description":"atomic scope test","goal":"stay scoped","scope":[{"kind":"ip","value":"127.0.0.1","reason":"test target"}]}`
	req := httptest.NewRequest("POST", "/api/tasks", strings.NewReader(body))
	recorder := httptest.NewRecorder()
	s.createTask(recorder, req)
	if recorder.Code != 201 {
		t.Fatalf("create task status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	taskID, err := strconv.ParseInt(response.ID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := m.Assets().ListTaskScopeWithSources(taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Kind != "ip" || rows[0].Net != "127.0.0.1/32" || rows[0].Reason != "test target" {
		t.Fatalf("initial scope = %#v", rows)
	}
}
