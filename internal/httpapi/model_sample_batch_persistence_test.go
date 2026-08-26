package httpapi

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"

	"curtainwall.example/assembly-gate/internal/domain"
	"curtainwall.example/assembly-gate/internal/evidence"
	"curtainwall.example/assembly-gate/internal/instrument"
	"curtainwall.example/assembly-gate/internal/store"
)

func TestModel_HeatSampleBatchValidationIsAtomicAndPersistent(t *testing.T) {
	cases := []struct {
		name          string
		rejectedTimes []int64
	}{
		{name: "cross_batch_time_regression", rejectedTimes: []int64{15, 25, 35}},
		{name: "cross_batch_duplicate_time", rejectedTimes: []int64{30, 40, 50}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "gate.db")
			db, err := store.NewSQLite(path, instrument.NewPayloadAdapter())
			if err != nil {
				t.Fatal(err)
			}
			srv := New(db, t.TempDir())

			snapshot := validSnapshot()
			snapshot.Rack = domain.RackPlan{
				FurnaceRun: "RUN-1",
				Positions:  []domain.RackPosition{{ID: "R1", Level: 1}},
			}
			rawSnapshot, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			response := doJSON(t, srv, http.MethodPost, "/api/designs/lock", string(rawSnapshot))
			if response.Code != http.StatusCreated {
				t.Fatalf("lock status = %d body=%s", response.Code, response.Body.String())
			}
			var task store.Task
			if err := json.Unmarshal(response.Body.Bytes(), &task); err != nil {
				t.Fatal(err)
			}

			postSamples := func(samples []evidence.SamplePoint) *domain.Error {
				t.Helper()
				req := store.SampleRequest{
					Stage: "heat_soak", RuleDigest: task.Snapshot.RuleDigest,
					Generation: task.Generation, Samples: samples,
				}
				raw, err := json.Marshal(req)
				if err != nil {
					t.Fatal(err)
				}
				response = doJSON(t, srv, http.MethodPost, "/api/tasks/"+task.ID+"/samples", string(raw))
				if response.Code == http.StatusOK {
					return nil
				}
				var apiErr domain.Error
				if err := json.Unmarshal(response.Body.Bytes(), &apiErr); err != nil {
					t.Fatalf("decode rejection: %v body=%s", err, response.Body.String())
				}
				return &apiErr
			}
			getCoverage := func() evidence.CoverageMatrix {
				t.Helper()
				response = doJSON(t, srv, http.MethodGet, "/api/tasks/"+task.ID+"/coverage", "")
				if response.Code != http.StatusOK {
					t.Fatalf("coverage status = %d body=%s", response.Code, response.Body.String())
				}
				var matrix evidence.CoverageMatrix
				if err := json.Unmarshal(response.Body.Bytes(), &matrix); err != nil {
					t.Fatal(err)
				}
				return matrix
			}

			initial := []evidence.SamplePoint{
				{LogicalTime: 10, Value: 100, RackPosition: "R1", Segment: "ramp_up"},
				{LogicalTime: 20, Value: 200, RackPosition: "R1", Segment: "hold"},
				{LogicalTime: 30, Value: 150, RackPosition: "R1", Segment: "ramp_down"},
			}
			if apiErr := postSamples(initial); apiErr != nil {
				t.Fatalf("initial batch rejected: %+v", apiErr)
			}
			beforeRejectedBatch := getCoverage()

			rejected := make([]evidence.SamplePoint, len(tc.rejectedTimes))
			segments := []string{"ramp_up", "hold", "ramp_down"}
			for i, logicalTime := range tc.rejectedTimes {
				rejected[i] = evidence.SamplePoint{
					LogicalTime: logicalTime, Value: int64(300 + i),
					RackPosition: "R1", Segment: segments[i],
				}
			}
			apiErr := postSamples(rejected)
			if apiErr == nil {
				t.Fatal("invalid cross-batch sequence was accepted")
			}
			if response.Code != http.StatusUnprocessableEntity || apiErr.Code != domain.CodeSampleGap {
				t.Fatalf("rejection = status %d error %+v, want 422 SAMPLE_GAP", response.Code, apiErr)
			}
			if after := getCoverage(); !reflect.DeepEqual(after, beforeRejectedBatch) {
				t.Fatalf("rejected batch changed HeatSamples coverage: before=%+v after=%+v", beforeRejectedBatch, after)
			}

			later := []evidence.SamplePoint{
				{LogicalTime: 40, Value: 140, RackPosition: "R1", Segment: "ramp_down"},
				{LogicalTime: 50, Value: 130, RackPosition: "R1", Segment: "ramp_down"},
				{LogicalTime: 60, Value: 120, RackPosition: "R1", Segment: "ramp_down"},
			}
			if apiErr := postSamples(later); apiErr != nil {
				t.Fatalf("later valid batch rejected: %+v", apiErr)
			}
			beforeRestart := getCoverage()
			if len(beforeRestart.Cells) != 3 || beforeRestart.Cells[2].SampleCount != 4 {
				t.Fatalf("valid append not reflected in coverage: %+v", beforeRestart)
			}

			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			db, err = store.NewSQLite(path, instrument.NewPayloadAdapter())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			srv = New(db, t.TempDir())
			if afterRestart := getCoverage(); !reflect.DeepEqual(afterRestart, beforeRestart) {
				t.Fatalf("coverage changed after restart: before=%+v after=%+v", beforeRestart, afterRestart)
			}
		})
	}
}
