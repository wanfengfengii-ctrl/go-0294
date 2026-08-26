package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"curtainwall.example/assembly-gate/internal/domain"
	"curtainwall.example/assembly-gate/internal/store"
)

func validSnapshot() domain.DesignSnapshot {
	return domain.DesignSnapshot{
		Project: "Tower-A", FacadeZone: "F1", PlateNumber: "P-001",
		Version: 1, ThicknessUM: 12000, WidthUM: 100010, HeightUM: 200010,
		EdgeMarginUM: 5, EdgeScheme: "flat-polish",
		Geometry: domain.Polygon{Outline: domain.Ring{
			{X: 5, Y: 5}, {X: 100005, Y: 5}, {X: 100005, Y: 200005}, {X: 5, Y: 200005},
		}},
		FurnaceLot: "LOT-7", FilmBatch: "FILM-9", FilmOpeningUM2: 1000000,
		Thresholds: map[string]int64{"surface_stress": 1000},
	}
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	return New(store.NewMemory(), t.TempDir())
}

func doJSON(t *testing.T, srv *Server, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, r)
	return w
}

func TestHealthEndpoint(t *testing.T) {
	srv := newTestServer(t)
	w := doJSON(t, srv, http.MethodGet, "/api/health", "")
	if w.Code != http.StatusOK {
		t.Fatalf("health status = %d", w.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Fatalf("unexpected health body: %v", body)
	}
}

func TestLockAndListTasks(t *testing.T) {
	srv := newTestServer(t)
	raw, _ := json.Marshal(validSnapshot())
	w := doJSON(t, srv, http.MethodPost, "/api/designs/lock", string(raw))
	if w.Code != http.StatusCreated {
		t.Fatalf("lock status = %d body=%s", w.Code, w.Body.String())
	}
	var task store.Task
	if err := json.Unmarshal(w.Body.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	if task.ID == "" || task.Snapshot.RuleDigest == "" {
		t.Fatalf("incomplete task: %+v", task)
	}
	w = doJSON(t, srv, http.MethodGet, "/api/tasks", "")
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d", w.Code)
	}
	var tasks []store.Task
	if err := json.Unmarshal(w.Body.Bytes(), &tasks); err != nil {
		t.Fatal(err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
}

func TestDuplicateLockReturnsConflict(t *testing.T) {
	srv := newTestServer(t)
	raw, _ := json.Marshal(validSnapshot())
	if w := doJSON(t, srv, http.MethodPost, "/api/designs/lock", string(raw)); w.Code != http.StatusCreated {
		t.Fatalf("first lock failed: %s", w.Body.String())
	}
	w := doJSON(t, srv, http.MethodPost, "/api/designs/lock", string(raw))
	if w.Code != http.StatusConflict {
		t.Fatalf("duplicate lock status = %d, want 409", w.Code)
	}
	var e domain.Error
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if e.Code != domain.CodeIdentityDuplicate {
		t.Fatalf("unexpected error code: %s", e.Code)
	}
	if len(e.Reasons) == 0 {
		t.Fatal("expected sorted reasons")
	}
}
