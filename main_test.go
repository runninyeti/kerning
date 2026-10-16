package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// testdata/in-network.json is trimmed from the 2026-09-28 Fidelis exchange file.
// The last item (99213 with a numeric code and no rates) is made up: the real
// file has no malformed items.
func fixture(t *testing.T) *Snapshot {
	m, err := parse("testdata/in-network.json")
	if err != nil {
		t.Fatal(err)
	}
	return build(m)
}

func TestBuild(t *testing.T) {
	s := fixture(t)
	colonoscopy := s.Procedures["45380"]
	egd := s.Procedures["43239"]

	tests := []struct {
		name      string
		got, want any
	}{
		{"malformed item is skipped", s.Counts.SkippedItems, 1},
		{"placeholder NPIs are dropped", s.Counts.BadNPIs, 2},
		{"repeated revenue code payload is deduped", s.Counts.Duplicates, 3},
		{"price at two places of service is split", len(colonoscopy.Rows), 6},
		{"revenue code row is routed to its CPT", colonoscopy.Rows[1].Source.Code, "490"},
		{"routed row is a facility fee", colonoscopy.Rows[1].Class, "institutional"},
		{"CPT only seen on revenue code rows has no name", egd.Name, ""},
		{"NPI under two EINs maps to both", len(s.ByNPI[1760657183]), 2},
		{"office group has one base rate per provider", len(colonoscopy.Groups[0].Values), 2},
		{"office median", colonoscopy.Groups[0].Median, (424.18 + 735.24) / 2},
		{"surgery center total adds doctor and facility medians", colonoscopy.Totals[0].Estimate, 154.03 + 247.85},
		{"no totals encodes as [] not null", egd.Totals != nil, true},
		{"quantile interpolates", quantile([]float64{10, 20, 30, 40}, 0.5), 25.0},
		{"percentile counts rates strictly below", percentile([]float64{10, 20, 20, 30}, 20), 25},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s: got %v, want %v", tt.name, tt.got, tt.want)
		}
	}
}

func TestRates(t *testing.T) {
	s := fixture(t)

	tests := []struct {
		url    string
		status int
		rows   int
	}{
		{"/api/rates?code=45380", http.StatusOK, 6},
		{"/api/rates?code=45380&npi=1760657183", http.StatusOK, 4},
		{"/api/rates?code=45380&ein=46-0857425", http.StatusOK, 1},
		{"/api/rates?code=45380&class=professional&limit=2", http.StatusOK, 2},
		{"/api/rates?code=45380&ein=123", http.StatusBadRequest, 0},
		{"/api/rates?code=45380&class=doctor", http.StatusBadRequest, 0},
		{"/api/rates?code=99999", http.StatusNotFound, 0},
		{"/api/rates", http.StatusBadRequest, 0},
	}
	for _, tt := range tests {
		w := httptest.NewRecorder()
		s.rates(w, httptest.NewRequest("GET", tt.url, nil))

		var res ratesResponse
		json.NewDecoder(w.Body).Decode(&res)
		if w.Code != tt.status || len(res.Rows) != tt.rows {
			t.Errorf("%s: got %d with %d rows, want %d with %d", tt.url, w.Code, len(res.Rows), tt.status, tt.rows)
		}
	}
}
