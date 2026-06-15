package opentargets

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mockServer returns an httptest.Server whose handler checks the GraphQL
// query body keyword and replies with an appropriate mock response.
func mockServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}

		var req struct {
			Query     string                 `json:"query"`
			Variables map[string]interface{} `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		q := req.Query

		switch {
		case strings.Contains(q, "search"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"search": map[string]interface{}{
						"total": 2,
						"hits": []map[string]interface{}{
							{"id": "ENSG00000141510", "name": "TP53"},
							{"id": "ENSG00000012048", "name": "BRCA1"},
						},
					},
				},
			})
		case strings.Contains(q, "associatedDiseases"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"target": map[string]interface{}{
						"associatedDiseases": map[string]interface{}{
							"count": 1,
							"rows": []map[string]interface{}{
								{
									"disease": map[string]interface{}{
										"id":   "EFO_0000311",
										"name": "cancer",
									},
									"score": 0.85,
								},
							},
						},
					},
				},
			})
		case strings.Contains(q, "approvedSymbol"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"target": map[string]interface{}{
						"id":             "ENSG00000141510",
						"approvedSymbol": "TP53",
						"approvedName":   "tumor protein p53",
						"biotype":        "protein_coding",
					},
				},
			})
		case strings.Contains(q, "associatedTargets"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"disease": map[string]interface{}{
						"associatedTargets": map[string]interface{}{
							"count": 1,
							"rows": []map[string]interface{}{
								{
									"target": map[string]interface{}{
										"id":             "ENSG00000141510",
										"approvedSymbol": "TP53",
									},
									"score": 0.9,
								},
							},
						},
					},
				},
			})
		case strings.Contains(q, "description"):
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"data": map[string]interface{}{
					"disease": map[string]interface{}{
						"id":          "EFO_0000311",
						"name":        "cancer",
						"description": "A disease characterized by uncontrolled cell division.",
					},
				},
			})
		default:
			http.Error(w, "unknown query", http.StatusBadRequest)
		}
	}))
}

func newTestClient(srv *httptest.Server) *Client {
	c := NewClient()
	c.BaseURL = srv.URL
	c.Rate = 0
	return c
}

func TestSearchTargets(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()

	c := newTestClient(srv)
	results, total, err := c.SearchTargets(context.Background(), "cancer", 10)
	if err != nil {
		t.Fatalf("SearchTargets: %v", err)
	}
	if total != 2 {
		t.Errorf("total = %d, want 2", total)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].ID != "ENSG00000141510" {
		t.Errorf("results[0].ID = %q, want ENSG00000141510", results[0].ID)
	}
	if results[0].Name != "TP53" {
		t.Errorf("results[0].Name = %q, want TP53", results[0].Name)
	}
}

func TestGetTarget(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()

	c := newTestClient(srv)
	tgt, err := c.GetTarget(context.Background(), "ENSG00000141510")
	if err != nil {
		t.Fatalf("GetTarget: %v", err)
	}
	if tgt.ID != "ENSG00000141510" {
		t.Errorf("ID = %q, want ENSG00000141510", tgt.ID)
	}
	if tgt.Symbol != "TP53" {
		t.Errorf("Symbol = %q, want TP53", tgt.Symbol)
	}
	if tgt.Name != "tumor protein p53" {
		t.Errorf("Name = %q, want tumor protein p53", tgt.Name)
	}
	if tgt.Biotype != "protein_coding" {
		t.Errorf("Biotype = %q, want protein_coding", tgt.Biotype)
	}
}

func TestGetDisease(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()

	c := newTestClient(srv)
	d, err := c.GetDisease(context.Background(), "EFO_0000311")
	if err != nil {
		t.Fatalf("GetDisease: %v", err)
	}
	if d.ID != "EFO_0000311" {
		t.Errorf("ID = %q, want EFO_0000311", d.ID)
	}
	if d.Name != "cancer" {
		t.Errorf("Name = %q, want cancer", d.Name)
	}
	if d.Description == "" {
		t.Error("Description is empty")
	}
}

func TestTargetDiseases(t *testing.T) {
	srv := mockServer(t)
	defer srv.Close()

	c := newTestClient(srv)
	assocs, err := c.TargetDiseases(context.Background(), "ENSG00000141510", 10)
	if err != nil {
		t.Fatalf("TargetDiseases: %v", err)
	}
	if len(assocs) != 1 {
		t.Fatalf("len(assocs) = %d, want 1", len(assocs))
	}
	if assocs[0].DiseaseID != "EFO_0000311" {
		t.Errorf("DiseaseID = %q, want EFO_0000311", assocs[0].DiseaseID)
	}
	if assocs[0].DiseaseName != "cancer" {
		t.Errorf("DiseaseName = %q, want cancer", assocs[0].DiseaseName)
	}
	if assocs[0].Score != 0.85 {
		t.Errorf("Score = %f, want 0.85", assocs[0].Score)
	}
}

func TestDoRetriesOn503(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{
				"target": map[string]interface{}{
					"id":             "ENSG00000141510",
					"approvedSymbol": "TP53",
					"approvedName":   "tumor protein p53",
					"biotype":        "protein_coding",
				},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(srv)
	c.Retries = 5
	tgt, err := c.GetTarget(context.Background(), "ENSG00000141510")
	if err != nil {
		t.Fatalf("GetTarget after retries: %v", err)
	}
	if tgt.Symbol != "TP53" {
		t.Errorf("Symbol = %q, want TP53", tgt.Symbol)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
}
