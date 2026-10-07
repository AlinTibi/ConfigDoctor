package config

import (
	"fmt"
	"sort"
	"strings"
)

type BaselineCounts struct {
	Missing    int `json:"missing"`
	Present    int `json:"present"`
	Extra      int `json:"extra"`
	Empty      int `json:"empty"`
	Unresolved int `json:"unresolved"`
	Cycles     int `json:"cycles"`
}

type BaselineInfo struct {
	Actual   string         `json:"actual"`
	Template string         `json:"template"`
	Counts   BaselineCounts `json:"counts"`
}

func baselineRows(actual, template *Document) ([]Row, BaselineCounts) {
	indexes := []map[string]Entry{{}, {}}
	keys := map[string]bool{}
	for i, d := range []*Document{actual, template} {
		for _, e := range d.Entries {
			indexes[i][e.Key] = e
			keys[e.Key] = true
		}
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	counts := BaselineCounts{}
	issues := map[string][]string{}
	for _, diagnostic := range actual.Diagnostics {
		if diagnostic.Code == "unresolved reference" {
			counts.Unresolved++
			issues[diagnostic.Key] = append(issues[diagnostic.Key], diagnostic.Code)
		}
		if diagnostic.Code == "cyclic reference" {
			counts.Cycles++
			issues[diagnostic.Key] = append(issues[diagnostic.Key], diagnostic.Code)
		}
	}
	rows := make([]Row, 0, len(ordered))
	for _, key := range ordered {
		a, hasActual := indexes[0][key]
		b, hasBaseline := indexes[1][key]
		r := Row{Key: key, Status: []string{}, Cells: []Cell{{File: actual.ID, Present: hasActual, Entry: a}, {File: template.ID, Present: hasBaseline, Entry: b}}}
		switch {
		case !hasActual:
			r.Status = append(r.Status, "missing baseline key")
			counts.Missing++
		case !hasBaseline:
			r.Status = append(r.Status, "extra")
			counts.Extra++
		default:
			r.Status = append(r.Status, "present")
			counts.Present++
		}
		if hasActual && a.Empty {
			r.Status = append(r.Status, "empty")
			counts.Empty++
		}
		if hasActual && a.Secret {
			r.Status = append(r.Status, "possible secrets")
		}
		if hasActual && hasBaseline && a.Type != b.Type {
			r.Status = append(r.Status, "type mismatch")
		}
		seen := map[string]bool{}
		for _, issue := range issues[key] {
			if !seen[issue] {
				r.Status = append(r.Status, issue)
				seen[issue] = true
			}
		}
		rows = append(rows, r)
	}
	return rows, counts
}

func Baseline(actual, template *Document, q Query) (Comparison, error) {
	if err := validateBaseline(actual, template); err != nil {
		return Comparison{}, err
	}
	rows, counts := baselineRows(actual, template)
	return pageBaseline(actual, template, rows, counts, q), nil
}
func validateBaseline(actual, template *Document) error {
	if actual == nil || template == nil {
		return fmt.Errorf("choose both an actual file and a baseline")
	}
	if actual == template || actual.ID != "" && actual.ID == template.ID {
		return fmt.Errorf("actual and baseline must be different files")
	}
	if !actual.Valid || !template.Valid {
		return fmt.Errorf("baseline comparison requires two syntax-valid files; inspect validation errors first")
	}
	return nil
}
func pageBaseline(actual, template *Document, rows []Row, counts BaselineCounts, q Query) Comparison {
	result := Comparison{Files: []Summary{actual.Summary(), template.Summary()}, Rows: []Row{}, Baseline: &BaselineInfo{Actual: actual.ID, Template: template.ID, Counts: counts}, Page: max(0, q.Page)}
	size := q.Size
	if size < 1 || size > 200 {
		size = 40
	}
	for _, row := range rows {
		if !strings.Contains(strings.ToLower(row.Key), strings.ToLower(q.Search)) {
			continue
		}
		match := q.Filter == "" || q.Filter == "all"
		for _, status := range row.Status {
			if status == q.Filter || q.Filter == "missing" && status == "missing baseline key" || q.Filter == "references" && (status == "unresolved reference" || status == "cyclic reference") || q.Filter == "warnings" && status != "present" && status != "extra" {
				match = true
			}
		}
		if !match {
			continue
		}
		if result.Total >= result.Page*size && len(result.Rows) < size {
			for i := range row.Cells {
				row.Cells[i].Entry = Visible(row.Cells[i].Entry, q.Reveal)
			}
			result.Rows = append(result.Rows, row)
		}
		result.Total++
	}
	result.Pages = max(1, (result.Total+size-1)/size)
	return result
}
