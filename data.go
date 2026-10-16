package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
)

// The source files, decoding only the fields we use.

type mrfIndex struct {
	Structures []struct {
		Plans []struct {
			Name string `json:"plan_name"`
		} `json:"reporting_plans"`
		Files []struct {
			URL string `json:"location"`
		} `json:"in_network_files"`
	} `json:"reporting_structure"`
}

type mrfRef struct {
	ID     int `json:"provider_group_id"`
	Groups []struct {
		NPIs []int `json:"npi"`
		TIN  struct {
			Value string `json:"value"`
			Name  string `json:"business_name"`
		} `json:"tin"`
	} `json:"provider_groups"`
}

type mrfItem struct {
	Type        string    `json:"billing_code_type"`
	Code        string    `json:"billing_code"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Rates       []mrfRate `json:"negotiated_rates"`
}

type mrfRate struct {
	Refs   []int      `json:"provider_references"`
	Prices []mrfPrice `json:"negotiated_prices"`
}

type mrfPrice struct {
	Rate      float64  `json:"negotiated_rate"`
	Class     string   `json:"billing_class"`
	POS       []string `json:"service_code"`
	Modifiers []string `json:"billing_code_modifier"`
	Info      string   `json:"additional_information"`
}

type mrfFile struct {
	Updated string
	Refs    []mrfRef
	Items   []mrfItem
	Skipped int
}

// What we serve.

type Snapshot struct {
	Source      Source
	Counts      Counts
	Procedures  map[string]*Procedure
	ByEIN       map[string][]*Provider
	ByNPI       map[int][]*Provider
	byProviders []*Procedure
}

type Source struct {
	Index   string `json:"index"`
	File    string `json:"file"`
	Updated string `json:"updated"`
	Plans   int    `json:"plans"`
}

type Counts struct {
	Items, SkippedItems, Rows, Duplicates, SkippedRows, BadNPIs int
}

type Provider struct {
	Ref  int   `json:"ref"`
	EINs []EIN `json:"eins"`
}

type EIN struct {
	EIN  string `json:"ein"`
	Name string `json:"name"`
	NPIs []int  `json:"npis"`
}

type Procedure struct {
	Code        string  `json:"code"`
	Type        string  `json:"type"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Providers   int     `json:"providers"`
	Groups      []Group `json:"-"`
	Totals      []Total `json:"-"`
	Rows        []Row   `json:"-"`
}

type Row struct {
	Provider   *Provider `json:"provider"`
	Group      string    `json:"group"`
	Class      string    `json:"class"`
	POS        string    `json:"pos"`
	Rate       float64   `json:"rate"`
	Modifiers  []string  `json:"modifiers"`
	Base       bool      `json:"base"`
	Percentile int       `json:"percentile"`
	Source     RowSource `json:"source"`
}

type RowSource struct {
	Type string   `json:"billing_code_type"`
	Code string   `json:"billing_code"`
	Info string   `json:"additional_information,omitempty"`
	POS  []string `json:"service_code"`
}

// A Group is every provider's base rate for one billing class at one place of service.
type Group struct {
	Key    string    `json:"key"`
	Class  string    `json:"class"`
	POS    string    `json:"pos"`
	Label  string    `json:"pos_label"`
	Values []float64 `json:"values"`
	Min    float64   `json:"min"`
	P25    float64   `json:"p25"`
	Median float64   `json:"median"`
	P75    float64   `json:"p75"`
	P90    float64   `json:"p90"`
	Max    float64   `json:"max"`
	Marks  []Mark    `json:"lens_marks,omitempty"`
}

type Mark struct {
	Rate       float64 `json:"rate"`
	Percentile int     `json:"percentile"`
}

type Total struct {
	POS           string  `json:"pos"`
	Label         string  `json:"pos_label"`
	Professional  float64 `json:"professional"`
	Institutional float64 `json:"institutional"`
	Estimate      float64 `json:"estimate"`
}

var posLabels = map[string]string{
	"02": "Telehealth",
	"10": "Telehealth (home)",
	"11": "Office",
	"12": "Home",
	"19": "Hospital outpatient (off campus)",
	"20": "Urgent care",
	"22": "Hospital outpatient",
	"23": "Emergency room",
	"24": "Surgery center",
	"49": "Independent clinic",
	"50": "Community health center",
	"72": "Rural health clinic",
	"81": "Independent lab",
}

func load(indexURL string) (*Snapshot, error) {
	indexPath, err := download(indexURL)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(indexPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var index mrfIndex
	if err := json.NewDecoder(f).Decode(&index); err != nil {
		return nil, fmt.Errorf("index: %w", err)
	}
	if len(index.Structures) == 0 || len(index.Structures[0].Files) == 0 {
		return nil, errors.New("index has no in-network files")
	}

	// The first structure is the Ambetter exchange network.
	structure := index.Structures[0]
	filePath, err := download(structure.Files[0].URL)
	if err != nil {
		return nil, err
	}
	m, err := parse(filePath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filePath, err)
	}

	s := build(m)
	s.Source = Source{
		Index:   indexURL,
		File:    filepath.Base(filePath),
		Updated: m.Updated,
		Plans:   len(structure.Plans),
	}
	return s, nil
}

// download fetches url into data/ once and returns the local path.
func download(url string) (string, error) {
	p := filepath.Join("data", path.Base(url))
	if _, err := os.Stat(p); err == nil {
		return p, nil
	}
	if err := os.MkdirAll("data", 0o755); err != nil {
		return "", err
	}

	res, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GET %s: %s", url, res.Status)
	}

	f, err := os.Create(p + ".part")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := io.Copy(f, res.Body); err != nil {
		return "", err
	}
	return p, os.Rename(p+".part", p)
}

// parse streams an in-network file one array element at a time, so an element
// with the wrong shape is skipped instead of failing the whole file.
func parse(p string) (*mrfFile, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(bufio.NewReader(f))

	m := &mrfFile{}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	for dec.More() {
		key, err := dec.Token()
		if err != nil {
			return nil, err
		}
		switch key {
		case "last_updated_on":
			err = dec.Decode(&m.Updated)
		case "provider_references":
			_, err = dec.Token()
			for err == nil && dec.More() {
				var ref mrfRef
				if err = dec.Decode(&ref); skippable(err) {
					m.Skipped++
					err = nil
				} else if err == nil {
					m.Refs = append(m.Refs, ref)
				}
			}
			if err == nil {
				_, err = dec.Token()
			}
		case "in_network":
			_, err = dec.Token()
			for err == nil && dec.More() {
				var item mrfItem
				if err = dec.Decode(&item); skippable(err) {
					m.Skipped++
					err = nil
				} else if err == nil {
					m.Items = append(m.Items, item)
				}
			}
			if err == nil {
				_, err = dec.Token()
			}
		default:
			var skip json.RawMessage
			err = dec.Decode(&skip)
		}
		if err != nil {
			return nil, err
		}
	}
	return m, nil
}

func skippable(err error) bool {
	var typeErr *json.UnmarshalTypeError
	return errors.As(err, &typeErr)
}

func build(m *mrfFile) *Snapshot {
	s := &Snapshot{
		Procedures: map[string]*Procedure{},
		ByEIN:      map[string][]*Provider{},
		ByNPI:      map[int][]*Provider{},
	}
	s.Counts.Items = len(m.Items)
	s.Counts.SkippedItems = m.Skipped

	providers := map[int]*Provider{}
	for _, ref := range m.Refs {
		p := &Provider{Ref: ref.ID}
		for _, g := range ref.Groups {
			e := EIN{EIN: g.TIN.Value, Name: g.TIN.Name, NPIs: []int{}}
			for _, npi := range g.NPIs {
				// NPIs are 10 digits; 0 shows up as a placeholder.
				if npi < 1_000_000_000 || npi > 9_999_999_999 {
					s.Counts.BadNPIs++
					continue
				}
				e.NPIs = append(e.NPIs, npi)
				if list := s.ByNPI[npi]; len(list) == 0 || list[len(list)-1] != p {
					s.ByNPI[npi] = append(list, p)
				}
			}
			p.EINs = append(p.EINs, e)
			s.ByEIN[e.EIN] = append(s.ByEIN[e.EIN], p)
		}
		if len(p.EINs) > 0 {
			providers[ref.ID] = p
		}
	}

	// Revenue code items repeat one payload under many unrelated names, so
	// identical rows are dropped and a revenue code only keeps its name if
	// every item agrees on it.
	seen := map[string]bool{}
	rcNames := map[string]map[string]bool{}
	for _, item := range m.Items {
		if item.Code == "" {
			s.Counts.SkippedItems++
			continue
		}
		for _, rate := range item.Rates {
			for _, price := range rate.Prices {
				// On revenue code rows the procedure code is in additional_information.
				code, typ := item.Code, item.Type
				if item.Type == "RC" && price.Info != "" {
					code, typ = price.Info, "CPT"
					if code[0] < '0' || code[0] > '9' {
						typ = "HCPCS"
					}
				}
				proc := s.Procedures[code]
				if proc == nil {
					proc = &Procedure{Code: code, Type: typ}
					s.Procedures[code] = proc
				}
				if item.Type != "RC" {
					proc.Name, proc.Description = item.Name, item.Description
				} else if typ == "RC" {
					if rcNames[code] == nil {
						rcNames[code] = map[string]bool{}
					}
					rcNames[code][item.Name] = true
				}

				positions := price.POS
				if len(positions) == 0 {
					positions = []string{""}
				}
				for _, ref := range rate.Refs {
					provider := providers[ref]
					if provider == nil || price.Rate <= 0 {
						s.Counts.SkippedRows++
						continue
					}
					for _, pos := range positions {
						key := fmt.Sprint(code, price.Class, pos, ref, price.Rate, price.Modifiers, item.Type, item.Code)
						if seen[key] {
							s.Counts.Duplicates++
							continue
						}
						seen[key] = true
						proc.Rows = append(proc.Rows, Row{
							Provider:  provider,
							Group:     price.Class + ":" + pos,
							Class:     price.Class,
							POS:       pos,
							Rate:      price.Rate,
							Modifiers: append([]string{}, price.Modifiers...),
							Source:    RowSource{Type: item.Type, Code: item.Code, Info: price.Info, POS: price.POS},
						})
						s.Counts.Rows++
					}
				}
			}
		}
	}
	for code, names := range rcNames {
		if len(names) == 1 {
			for name := range names {
				s.Procedures[code].Name = name
			}
		}
	}

	for code, proc := range s.Procedures {
		if len(proc.Rows) == 0 {
			delete(s.Procedures, code)
			continue
		}
		aggregate(proc)
		s.byProviders = append(s.byProviders, proc)
	}
	sort.Slice(s.byProviders, func(i, j int) bool {
		a, b := s.byProviders[i], s.byProviders[j]
		return a.Providers > b.Providers || a.Providers == b.Providers && a.Code < b.Code
	})
	return s
}

// aggregate picks each provider's base rate per group and precomputes the spread.
// The base rate is the row with the fewest modifiers, and the highest of those,
// since lower rates for the same code are usually licensure tiers.
func aggregate(p *Procedure) {
	sort.Slice(p.Rows, func(i, j int) bool {
		a, b := p.Rows[i], p.Rows[j]
		if a.Rate != b.Rate {
			return a.Rate < b.Rate
		}
		if a.Provider.EINs[0].EIN != b.Provider.EINs[0].EIN {
			return a.Provider.EINs[0].EIN < b.Provider.EINs[0].EIN
		}
		if a.Provider.Ref != b.Provider.Ref {
			return a.Provider.Ref < b.Provider.Ref
		}
		return a.Group < b.Group
	})

	groups := map[string][]int{}
	providers := map[*Provider]bool{}
	for i, r := range p.Rows {
		groups[r.Group] = append(groups[r.Group], i)
		providers[r.Provider] = true
	}
	p.Providers = len(providers)

	for key, rows := range groups {
		base := map[*Provider]int{}
		for _, i := range rows {
			r := p.Rows[i]
			j, ok := base[r.Provider]
			if !ok || len(r.Modifiers) < len(p.Rows[j].Modifiers) ||
				len(r.Modifiers) == len(p.Rows[j].Modifiers) && r.Rate > p.Rows[j].Rate {
				base[r.Provider] = i
			}
		}
		var values []float64
		for _, i := range base {
			p.Rows[i].Base = true
			values = append(values, p.Rows[i].Rate)
		}
		sort.Float64s(values)
		for _, i := range rows {
			p.Rows[i].Percentile = percentile(values, p.Rows[i].Rate)
		}

		first := p.Rows[rows[0]]
		label := posLabels[first.POS]
		if label == "" {
			label = "Place of service " + first.POS
		}
		p.Groups = append(p.Groups, Group{
			Key:    key,
			Class:  first.Class,
			POS:    first.POS,
			Label:  label,
			Values: values,
			Min:    values[0],
			P25:    quantile(values, 0.25),
			Median: quantile(values, 0.5),
			P75:    quantile(values, 0.75),
			P90:    quantile(values, 0.9),
			Max:    values[len(values)-1],
		})
	}
	sort.Slice(p.Groups, func(i, j int) bool {
		a, b := p.Groups[i], p.Groups[j]
		return len(a.Values) > len(b.Values) || len(a.Values) == len(b.Values) && a.Key < b.Key
	})

	p.Totals = []Total{}
	for _, g := range p.Groups {
		if g.Class != "institutional" {
			continue
		}
		i := slices.IndexFunc(p.Groups, func(o Group) bool { return o.Class == "professional" && o.POS == g.POS })
		if i < 0 {
			continue
		}
		pro := p.Groups[i]
		p.Totals = append(p.Totals, Total{
			POS:           g.POS,
			Label:         g.Label,
			Professional:  pro.Median,
			Institutional: g.Median,
			Estimate:      pro.Median + g.Median,
		})
	}
	sort.Slice(p.Totals, func(i, j int) bool { return p.Totals[i].Estimate < p.Totals[j].Estimate })
}

// quantile interpolates linearly between the closest ranks of sorted values.
func quantile(values []float64, q float64) float64 {
	pos := q * float64(len(values)-1)
	i := int(pos)
	if i+1 >= len(values) {
		return values[i]
	}
	return values[i] + (values[i+1]-values[i])*(pos-float64(i))
}

// percentile is the share of base rates below rate.
func percentile(values []float64, rate float64) int {
	return sort.SearchFloat64s(values, rate) * 100 / len(values)
}
