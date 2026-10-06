package config

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
)

type Cell struct {
	File    string `json:"file"`
	Present bool   `json:"present"`
	Invalid bool   `json:"invalid"`
	Entry   Entry  `json:"entry"`
}
type Row struct {
	Key    string   `json:"key"`
	Status []string `json:"status"`
	Cells  []Cell   `json:"cells"`
}
type Query struct {
	Page   int    `json:"page"`
	Size   int    `json:"size"`
	Search string `json:"search"`
	Filter string `json:"filter"`
	Reveal bool   `json:"reveal"`
}
type Comparison struct {
	Files []Summary `json:"files"`
	Rows  []Row     `json:"rows"`
	Total int       `json:"total"`
	Page  int       `json:"page"`
	Pages int       `json:"pages"`
}

func Compare(docs []*Document, q Query) Comparison {
	result := Comparison{Files: []Summary{}, Rows: []Row{}}
	indexes := make([]map[string]Entry, len(docs))
	keys := map[string]bool{}
	for i, d := range docs {
		result.Files = append(result.Files, d.Summary())
		indexes[i] = make(map[string]Entry, len(d.Entries))
		if d.Valid {
			for _, e := range d.Entries {
				keys[e.Key] = true
				indexes[i][e.Key] = e
			}
		}
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	size := q.Size
	if size < 1 || size > 200 {
		size = 60
	}
	page := max(0, q.Page)
	start := page * size
	for _, k := range ordered {
		if !strings.Contains(strings.ToLower(k), strings.ToLower(q.Search)) {
			continue
		}
		r := Row{Key: k, Status: []string{}, Cells: []Cell{}}
		missing, empty, secret, different, mismatch, invalid := false, false, false, false, false, false
		var first *Entry
		for i, d := range docs {
			e, present := indexes[i][k]

			if !d.Valid {
				invalid = true
			} else if !present {
				missing = true
			} else {
				empty = empty || e.Empty
				secret = secret || e.Secret
				if first == nil {
					x := e
					first = &x
				} else {
					different = different || differentValues(*first, e)
					mismatch = mismatch || first.Type != e.Type
				}

			}

		}
		if invalid {
			r.Status = append(r.Status, "error")
		}
		if missing {
			r.Status = append(r.Status, "missing")
		}
		if mismatch {
			r.Status = append(r.Status, "type mismatch")
		}
		if different {
			r.Status = append(r.Status, "different")
		}
		if empty {
			r.Status = append(r.Status, "empty")
		}
		if secret {
			r.Status = append(r.Status, "possible secrets")
		}
		if len(r.Status) == 0 {
			r.Status = append(r.Status, "same")
		}
		matches := q.Filter == "" || q.Filter == "all"
		for _, s := range r.Status {
			if s == q.Filter || q.Filter == "warnings" && s != "same" || q.Filter == "errors" && s == "error" {
				matches = true
			}
		}
		if !matches {
			continue
		}
		if result.Total >= start && len(result.Rows) < size {
			r.Cells = make([]Cell, 0, len(docs))
			for i, d := range docs {
				e, present := indexes[i][k]
				r.Cells = append(r.Cells, Cell{File: d.ID, Present: present, Invalid: !d.Valid, Entry: Visible(e, q.Reveal)})
			}
			result.Rows = append(result.Rows, r)
		}
		result.Total++
	}
	result.Page = page
	result.Pages = max(1, (result.Total+size-1)/size)
	return result
}
func differentValues(a, b Entry) bool {
	if a.Value == b.Value {
		return false
	}
	if a.Type != "number" || b.Type != "number" {
		return true
	}
	safe := func(s string) bool {
		if len(s) > 256 {
			return false
		}
		if i := strings.IndexAny(s, "eE"); i >= 0 {
			n, e := strconv.Atoi(s[i+1:])
			if e != nil || n > 1000 || n < -1000 {
				return false
			}
		}
		return true
	}
	if !safe(a.Value) || !safe(b.Value) {
		return true
	}
	x, ok := new(big.Rat).SetString(a.Value)
	if !ok {
		return true
	}
	y, ok := new(big.Rat).SetString(b.Value)
	return !ok || x.Cmp(y) != 0
}

type DocumentView struct {
	Summary     Summary      `json:"summary"`
	Entries     []Entry      `json:"entries"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Total       int          `json:"total"`
	Page        int          `json:"page"`
	Pages       int          `json:"pages"`
}

func Inspect(d *Document, q Query) DocumentView {
	relevant := map[string]bool{}
	for _, i := range d.Diagnostics {
		if i.Key != "" && ((q.Filter == "errors" && i.Severity == "error") || (q.Filter == "warnings" && i.Severity == "warning")) {
			relevant[i.Key] = true
		}
	}
	size := q.Size
	if size < 1 || size > 200 {
		size = 100
	}
	v := DocumentView{Summary: d.Summary(), Entries: []Entry{}, Diagnostics: d.Diagnostics, Page: max(0, q.Page)}
	for _, e := range d.Entries {
		if (q.Filter == "errors" || q.Filter == "warnings") && !relevant[e.Key] {
			continue
		}
		if !strings.Contains(strings.ToLower(e.Key), strings.ToLower(q.Search)) {
			continue
		}
		if q.Filter == "empty" && !e.Empty || q.Filter == "possible secrets" && !e.Secret {
			continue
		}
		if v.Total >= v.Page*size && len(v.Entries) < size {
			v.Entries = append(v.Entries, Visible(e, q.Reveal))
		}
		v.Total++
	}
	v.Pages = max(1, (v.Total+size-1)/size)
	return v
}
func Example(d *Document, keepSafe bool) (string, error) {
	if d.Format != "env" || !d.Valid {
		return "", fmt.Errorf("example generation requires a valid .env file")
	}
	values := map[string]Entry{}
	for _, e := range d.Entries {
		values[e.Key] = e
	}
	var b strings.Builder
	b.WriteString("# Generated by Config Doctor. Review before sharing.\n# Values and original comment text are removed by default.\n")
	emitted := map[string]bool{}
	for _, line := range d.EnvLines {
		if line.Key == "" {
			if strings.TrimSpace(line.Comment) == "" {
				b.WriteByte('\n')
			} else {
				b.WriteString("# Comment omitted for privacy.\n")
			}
			continue
		}
		if emitted[line.Key] {
			continue
		}
		emitted[line.Key] = true
		e := values[line.Key]
		v := ""
		if keepSafe && !e.Secret && safeExample(line.Key, e.Value) {
			v = e.Value
		}
		b.WriteString(line.Key + "=" + v + "\n")
	}
	return b.String(), nil
}
func safeExample(key, value string) bool {
	switch strings.ToUpper(key) {
	case "PORT", "HTTP_PORT", "HTTPS_PORT":
		for _, c := range value {
			if c < '0' || c > '9' {
				return false
			}
		}
		return len(value) > 0 && len(value) <= 5
	case "DEBUG", "ENABLED":
		return value == "true" || value == "false"
	case "NODE_ENV", "ENVIRONMENT":
		return value == "development" || value == "production" || value == "test"
	}
	return false
}
