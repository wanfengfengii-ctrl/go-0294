package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestModel_VerdictRequiresProcessSampleEvidence(t *testing.T) {
	tests := []struct {
		name            string
		submitHeat      bool
		submitAutoclave bool
		wantMessage     string
	}{
		{
			name:            "missing heat-soak coverage is rejected without a terminal write",
			submitHeat:      false,
			submitAutoclave: true,
			wantMessage:     "heat-soak coverage not fully covered for current generation",
		},
		{
			name:            "missing autoclave samples are rejected without a terminal write",
			submitHeat:      true,
			submitAutoclave: false,
			wantMessage:     "autoclave continuous prefix not closed for current generation",
		},
		{
			name:            "complete heat-soak and autoclave evidence still admits",
			submitHeat:      true,
			submitAutoclave: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newTestServer(t)
			request := func(method, path string, payload any) (int, []byte) {
				t.Helper()
				body := ""
				if payload != nil {
					raw, err := json.Marshal(payload)
					if err != nil {
						t.Fatalf("marshal request for %s: %v", path, err)
					}
					body = string(raw)
				}
				w := doJSON(t, srv, method, path, body)
				return w.Code, w.Body.Bytes()
			}
			mustStatus := func(method, path string, payload any, want int) []byte {
				t.Helper()
				status, body := request(method, path, payload)
				if status != want {
					t.Fatalf("%s %s status = %d, want %d; body=%s", method, path, status, want, body)
				}
				return body
			}

			snapshot := map[string]any{
				"project": "Tower-A", "facade_zone": "F1", "plate_number": "P-001",
				"version": 1, "thickness_um": 12000, "width_um": 100010, "height_um": 200010,
				"edge_margin_um": 5, "edge_scheme": "flat-polish",
				"geometry": map[string]any{"outline": []map[string]int64{
					{"x": 5, "y": 5}, {"x": 100005, "y": 5},
					{"x": 100005, "y": 200005}, {"x": 5, "y": 200005},
				}},
				"furnace_lot": "LOT-7", "film_batch": "FILM-9", "film_opening_um2": 1000000,
				"thresholds": map[string]int64{"surface_stress": 1000, "bow": 1000000, "bubble_rate": 1000},
				"rack": map[string]any{
					"furnace_run": "RUN-1",
					"positions":   []map[string]any{{"id": "R1", "level": 1}},
					"adjacency":   []any{},
				},
				"inspection": map[string]any{
					"grid": []string{"G1"}, "sampling": map[string]string{"G1": "P-001"}, "destructive": 1,
				},
			}
			lockBody := mustStatus(http.MethodPost, "/api/designs/lock", snapshot, http.StatusCreated)
			var task struct {
				ID         string `json:"id"`
				Generation int    `json:"generation"`
				Snapshot   struct {
					RuleDigest string `json:"rule_digest"`
				} `json:"snapshot"`
			}
			if err := json.Unmarshal(lockBody, &task); err != nil {
				t.Fatalf("decode locked task: %v", err)
			}
			taskPath := "/api/tasks/" + task.ID
			operation := func(id, stage string, logicalTime int64, extra map[string]any) {
				t.Helper()
				payload := map[string]any{
					"operation_id": id, "rule_digest": task.Snapshot.RuleDigest,
					"generation": task.Generation, "logical_time": logicalTime,
					"operator": "operator-1", "stage": stage,
				}
				for key, value := range extra {
					payload[key] = value
				}
				mustStatus(http.MethodPost, taskPath+"/operations", payload, http.StatusOK)
			}
			instrument := func(device, payload string, logicalTime int64) {
				t.Helper()
				mustStatus(http.MethodPost, taskPath+"/instrument-calls", map[string]any{
					"device": device, "payload": payload, "rule_digest": task.Snapshot.RuleDigest,
					"generation": task.Generation, "logical_time": logicalTime, "operator": "operator-1",
				}, http.StatusOK)
			}
			samples := func(stage string) {
				t.Helper()
				var points []map[string]any
				if stage == "heat_soak" {
					points = []map[string]any{
						{"logical_time": 5, "value": 100, "rack_position": "R1", "segment": "ramp_up"},
						{"logical_time": 6, "value": 200, "rack_position": "R1", "segment": "hold"},
						{"logical_time": 7, "value": 150, "rack_position": "R1", "segment": "ramp_down"},
					}
				} else {
					points = []map[string]any{
						{"logical_time": 11, "value": 100, "segment": "preheat"},
						{"logical_time": 12, "value": 200, "segment": "pressurize"},
						{"logical_time": 13, "value": 200, "segment": "hold"},
						{"logical_time": 14, "value": 100, "segment": "depressurize"},
						{"logical_time": 15, "value": 50, "segment": "cool"},
					}
				}
				mustStatus(http.MethodPost, taskPath+"/samples", map[string]any{
					"stage": stage, "rule_digest": task.Snapshot.RuleDigest,
					"generation": task.Generation, "samples": points,
				}, http.StatusOK)
			}

			operation("op-edge", "edge_confirm", 1, nil)
			operation("op-temper", "temper", 2, nil)
			instrument("stress_meter", `{"force":5000,"area":1000}`, 3)
			operation("op-heat", "heat_soak", 4, map[string]any{
				"resource_key": "rack-1", "lease_start": 4, "lease_end": 100,
			})
			if tc.submitHeat {
				samples("heat_soak")
			}
			operation("op-lamination", "lamination", 8, map[string]any{
				"resource_key": "table-1", "lease_start": 8, "lease_end": 100,
				"film_entry": map[string]any{"kind": "issue", "amount_um2": 300000},
			})
			operation("op-pre-press", "pre_press", 9, map[string]any{
				"film_entry": map[string]any{"kind": "cut", "amount_um2": 300000},
			})
			operation("op-autoclave", "autoclave", 10, map[string]any{
				"resource_key": "autoclave-1", "lease_start": 10, "lease_end": 100,
			})
			if tc.submitAutoclave {
				samples("autoclave")
			}
			instrument("optical_scanner", `{"deviation":3,"span":1000}`, 16)
			instrument("destructive_rig", `{"passed":true}`, 17)
			for _, reviewer := range []string{"alice", "bob"} {
				mustStatus(http.MethodPost, taskPath+"/reviews", map[string]any{
					"reviewer": reviewer, "qualified": true, "generation": task.Generation,
				}, http.StatusOK)
			}

			verdictPayload := map[string]any{"verdict": "admit", "generation": task.Generation}
			status, verdictBody := request(http.MethodPost, taskPath+"/verdicts", verdictPayload)
			if tc.wantMessage != "" {
				if status != http.StatusUnprocessableEntity {
					t.Fatalf("verdict status = %d, want 422; body=%s", status, verdictBody)
				}
				var rejection struct {
					Code       string `json:"code"`
					Message    string `json:"message"`
					Credential string `json:"credential"`
				}
				if err := json.Unmarshal(verdictBody, &rejection); err != nil {
					t.Fatalf("decode verdict rejection: %v", err)
				}
				if rejection.Code != "SAMPLE_GAP" || rejection.Message != tc.wantMessage {
					t.Fatalf("unstable verdict rejection: code=%q message=%q", rejection.Code, rejection.Message)
				}
				if rejection.Credential != "" {
					t.Fatalf("rejected verdict generated credential %q", rejection.Credential)
				}
				statusAgain, bodyAgain := request(http.MethodPost, taskPath+"/verdicts", verdictPayload)
				if statusAgain != status || string(bodyAgain) != string(verdictBody) {
					t.Fatalf("repeated rejection was not stable: first=(%d,%s) second=(%d,%s)", status, verdictBody, statusAgain, bodyAgain)
				}
				var current struct {
					Completed  []string `json:"completed"`
					Verdict    string   `json:"verdict"`
					Credential string   `json:"credential"`
				}
				currentBody := mustStatus(http.MethodGet, taskPath, nil, http.StatusOK)
				if err := json.Unmarshal(currentBody, &current); err != nil {
					t.Fatalf("decode task after rejected verdict: %v", err)
				}
				if current.Verdict != "" || current.Credential != "" {
					t.Fatalf("rejected verdict mutated terminal state: %+v", current)
				}
				for _, stage := range current.Completed {
					if stage == "final" {
						t.Fatal("rejected verdict closed the final stage")
					}
				}
				if !tc.submitHeat {
					samples("heat_soak")
				}
				if !tc.submitAutoclave {
					samples("autoclave")
				}
				verdictBody = mustStatus(http.MethodPost, taskPath+"/verdicts", verdictPayload, http.StatusOK)
			} else if status != http.StatusOK {
				t.Fatalf("complete-evidence verdict status = %d, want 200; body=%s", status, verdictBody)
			}

			var admitted struct {
				Credential string `json:"credential"`
			}
			if err := json.Unmarshal(verdictBody, &admitted); err != nil {
				t.Fatalf("decode admitted verdict: %v", err)
			}
			if !strings.HasPrefix(admitted.Credential, "CRED-") {
				t.Fatalf("admission did not mint a credential: %s", verdictBody)
			}
		})
	}
}
