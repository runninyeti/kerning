package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type ratesResponse struct {
	Source       Source     `json:"source"`
	Procedure    *Procedure `json:"procedure"`
	Lens         *Lens      `json:"lens"`
	Groups       []Group    `json:"groups"`
	Totals       []Total    `json:"totals"`
	Rows         []Row      `json:"rows"`
	Total        int        `json:"total"`
	NetworkTotal int        `json:"network_total"`
	Limit        int        `json:"limit"`
	Offset       int        `json:"offset"`
}

type Lens struct {
	EIN     string `json:"ein,omitempty"`
	NPI     string `json:"npi,omitempty"`
	Matched []EIN  `json:"matched"`
}

// rates returns every rate for a procedure code. ein or npi narrows the rows to
// one provider; groups and totals always describe the whole network.
func (s *Snapshot) rates(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	code := strings.ToUpper(strings.TrimSpace(q.Get("code")))
	if code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}
	p := s.Procedures[code]
	if p == nil {
		writeError(w, http.StatusNotFound, "no in-network rates for code "+code)
		return
	}
	limit, offset, err := page(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	lens, providers, err := s.lens(q.Get("ein"), q.Get("npi"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	class, pos := q.Get("class"), q.Get("pos")
	if class != "" && class != "professional" && class != "institutional" {
		writeError(w, http.StatusBadRequest, "class must be professional or institutional")
		return
	}

	groups := slices.Clone(p.Groups)
	rows := []Row{}
	for _, row := range p.Rows {
		if lens != nil && !providers[row.Provider] {
			continue
		}
		if lens != nil && row.Base {
			i := slices.IndexFunc(groups, func(g Group) bool { return g.Key == row.Group })
			groups[i].Marks = append(groups[i].Marks, Mark{Rate: row.Rate, Percentile: row.Percentile})
		}
		if class != "" && row.Class != class || pos != "" && row.POS != pos {
			continue
		}
		rows = append(rows, row)
	}

	// Rows are stored by rate with tie-breaks, so a stable sort keeps pages consistent.
	switch q.Get("sort") {
	case "", "rate":
	case "-rate":
		slices.SortStableFunc(rows, func(a, b Row) int { return cmp.Compare(b.Rate, a.Rate) })
	case "provider":
		slices.SortStableFunc(rows, func(a, b Row) int { return strings.Compare(a.Provider.EINs[0].Name, b.Provider.EINs[0].Name) })
	default:
		writeError(w, http.StatusBadRequest, "sort must be rate, -rate or provider")
		return
	}

	total := len(rows)
	offset = min(offset, total)
	writeJSON(w, ratesResponse{
		Source:       s.Source,
		Procedure:    p,
		Lens:         lens,
		Groups:       groups,
		Totals:       p.Totals,
		Rows:         rows[offset:min(offset+limit, total)],
		Total:        total,
		NetworkTotal: len(p.Rows),
		Limit:        limit,
		Offset:       offset,
	})
}

// procedures suggests codes by code prefix, name or description, most widely contracted first.
func (s *Snapshot) procedures(w http.ResponseWriter, r *http.Request) {
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if q == "" {
		writeError(w, http.StatusBadRequest, "q is required")
		return
	}
	matches := []*Procedure{}
	for _, p := range s.byProviders {
		if strings.HasPrefix(strings.ToLower(p.Code), q) ||
			strings.Contains(strings.ToLower(p.Name), q) ||
			strings.Contains(strings.ToLower(p.Description), q) {
			matches = append(matches, p)
		}
		if len(matches) == 10 {
			break
		}
	}
	writeJSON(w, matches)
}

func (s *Snapshot) lens(ein, npi string) (*Lens, map[*Provider]bool, error) {
	if ein == "" && npi == "" {
		return nil, nil, nil
	}
	if ein != "" && npi != "" {
		return nil, nil, errors.New("use ein or npi, not both")
	}

	lens := &Lens{Matched: []EIN{}}
	providers := map[*Provider]bool{}
	if ein != "" {
		digits := strings.ReplaceAll(ein, "-", "")
		if len(digits) != 9 || strings.Trim(digits, "0123456789") != "" {
			return nil, nil, errors.New("ein must be 9 digits, like 12-3456789")
		}
		lens.EIN = digits[:2] + "-" + digits[2:]
		for _, p := range s.ByEIN[lens.EIN] {
			providers[p] = true
			for _, e := range p.EINs {
				if e.EIN == lens.EIN && !slices.ContainsFunc(lens.Matched, func(m EIN) bool { return m.EIN == e.EIN }) {
					lens.Matched = append(lens.Matched, e)
				}
			}
		}
		return lens, providers, nil
	}

	n, err := strconv.Atoi(npi)
	if err != nil || len(npi) != 10 || strings.Trim(npi, "0123456789") != "" {
		return nil, nil, errors.New("npi must be 10 digits")
	}
	lens.NPI = npi
	for _, p := range s.ByNPI[n] {
		providers[p] = true
		for _, e := range p.EINs {
			if slices.Contains(e.NPIs, n) && !slices.ContainsFunc(lens.Matched, func(m EIN) bool { return m.EIN == e.EIN }) {
				lens.Matched = append(lens.Matched, e)
			}
		}
	}
	return lens, providers, nil
}

// export writes what the UI needs as static files, so it can be hosted without
// this server: every procedure with all of its rows, the procedure list, and
// which EINs and NPIs are in the network. web/src/api.js filters and pages them.
func (s *Snapshot) export(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "rates"), 0o755); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(dir, "procedures.json"), s.byProviders); err != nil {
		return err
	}
	for code, p := range s.Procedures {
		res := ratesResponse{
			Source:       s.Source,
			Procedure:    p,
			Groups:       p.Groups,
			Totals:       p.Totals,
			Rows:         p.Rows,
			Total:        len(p.Rows),
			NetworkTotal: len(p.Rows),
			Limit:        len(p.Rows),
		}
		if err := writeFile(filepath.Join(dir, "rates", code+".json"), res); err != nil {
			return err
		}
	}

	network := struct {
		EINs map[string]string `json:"eins"`
		NPIs map[int][]string  `json:"npis"`
	}{map[string]string{}, map[int][]string{}}
	for ein, providers := range s.ByEIN {
		for _, e := range providers[0].EINs {
			if e.EIN == ein {
				network.EINs[ein] = e.Name
			}
		}
	}
	for npi, providers := range s.ByNPI {
		for _, p := range providers {
			for _, e := range p.EINs {
				if slices.Contains(e.NPIs, npi) && !slices.Contains(network.NPIs[npi], e.EIN) {
					network.NPIs[npi] = append(network.NPIs[npi], e.EIN)
				}
			}
		}
	}
	return writeFile(filepath.Join(dir, "network.json"), network)
}

func writeFile(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(v)
}

func page(q url.Values) (limit, offset int, err error) {
	limit = 25
	if v := q.Get("limit"); v != "" {
		limit, err = strconv.Atoi(v)
		if err != nil || limit < 1 || limit > 100 {
			return 0, 0, errors.New("limit must be between 1 and 100")
		}
	}
	if v := q.Get("offset"); v != "" {
		offset, err = strconv.Atoi(v)
		if err != nil || offset < 0 {
			return 0, 0, errors.New("offset must be 0 or more")
		}
	}
	return limit, offset, nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
