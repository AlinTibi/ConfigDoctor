package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"sort"
	"strings"
	"time"
)

type Report struct {
	Product        string     `json:"product"`
	Generated      string     `json:"generated"`
	IncludesValues bool       `json:"includesValues"`
	Notice         string     `json:"notice"`
	Files          []Document `json:"files"`
	Differences    []Finding  `json:"differences"`
}
type Finding struct {
	Key       string   `json:"key"`
	Status    []string `json:"status"`
	MissingIn []string `json:"missingIn"`
}

func Export(docs []*Document, format string, include bool) ([]byte, error) {
	r := Report{Product: "Config Doctor", Generated: time.Now().UTC().Format(time.RFC3339), IncludesValues: include, Notice: "Possible-secret detection is heuristic. Review keys, file names and diagnostics before sharing any report. Comparison excludes invalid files.", Files: []Document{}, Differences: []Finding{}}
	valid := []*Document{}
	for i, d := range docs {
		if d.Valid {
			copy := *d
			copy.ID = fmt.Sprintf("File %d: %s", i+1, d.Name)
			valid = append(valid, &copy)
		}
	}
	if len(valid) > 1 {
		r.Differences = reportDifferences(valid)
	}

	for _, d := range docs {
		copy := *d
		copy.Path = ""
		copy.ID = ""
		copy.EnvLines = nil
		copy.Entries = make([]Entry, len(d.Entries))
		for i, e := range d.Entries {
			copy.Entries[i] = Visible(e, include)
		}
		r.Files = append(r.Files, copy)
	}
	switch format {
	case "json":
		return json.MarshalIndent(r, "", "  ")
	case "txt":
		var b strings.Builder
		fmt.Fprintf(&b, "%s report\n%s\n%s\nValues included: %t\n\n", r.Product, r.Generated, r.Notice, include)
		b.WriteString("COMPARISON\n")
		for _, f := range r.Differences {
			fmt.Fprintf(&b, "%s | %s | Missing in: %s\n", f.Key, strings.Join(f.Status, ", "), strings.Join(f.MissingIn, ", "))
		}
		b.WriteString("\n")
		for _, d := range r.Files {
			fmt.Fprintf(&b, "FILE: %s (%s) | Valid: %t\n", d.Name, d.Format, d.Valid)
			for _, i := range d.Diagnostics {
				fmt.Fprintf(&b, "%s | %s | line %d | %s\n", strings.ToUpper(i.Severity), i.Key, i.Line, i.Message)
			}
			for _, e := range d.Entries {
				fmt.Fprintf(&b, "%s [%s] = %s\n", e.Key, e.Type, e.Value)
			}
			b.WriteString("\n")
		}
		return []byte(b.String()), nil
	case "html":
		t := template.Must(template.New("report").Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'"><title>Config Doctor report</title><style>body{font:16px system-ui;max-width:1100px;margin:40px auto;padding:20px;background:#10151e;color:#e5ecf5}table{border-collapse:collapse;width:100%;margin:24px 0}th,td{text-align:left;border:1px solid #364252;padding:10px;overflow-wrap:anywhere}code{white-space:pre-wrap}h1,h2{color:#a7e6d6}</style><h1>{{.Product}} report</h1><p>{{.Generated}}</p><p>{{.Notice}}</p><p>Values included: {{.IncludesValues}}</p><h2>Comparison</h2><table><tr><th>Key</th><th>Status</th><th>Missing in</th></tr>{{range .Differences}}<tr><td><code>{{.Key}}</code></td><td>{{range .Status}}{{.}} · {{end}}</td><td>{{range .MissingIn}}{{.}} · {{end}}</td></tr>{{end}}</table>{{range .Files}}<h2>{{.Name}} ({{.Format}})</h2><p>Syntax valid: {{.Valid}}</p><ul>{{range .Diagnostics}}<li>{{.Severity}} · {{.Key}} · line {{.Line}} · {{.Message}}</li>{{end}}</ul><table><tr><th>Key</th><th>Type</th><th>Value</th></tr>{{range .Entries}}<tr><td><code>{{.Key}}</code></td><td>{{.Type}}</td><td><code>{{.Value}}</code>{{if .Secret}} · Possible secret{{end}}</td></tr>{{end}}</table>{{end}}</html>`))
		var b bytes.Buffer
		err := t.Execute(&b, r)
		return b.Bytes(), err
	}
	return nil, fmt.Errorf("choose HTML, JSON or TXT report format")
}

func reportDifferences(docs []*Document) []Finding {
	indexes := make([]map[string]Entry, len(docs))
	keys := map[string]bool{}
	for i, d := range docs {
		indexes[i] = make(map[string]Entry, len(d.Entries))
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
	findings := []Finding{}
	for _, key := range ordered {
		f := Finding{Key: key, Status: []string{}, MissingIn: []string{}}
		missing, empty, secret, different, mismatch := 0, false, false, false, false
		var first *Entry
		for i, d := range docs {
			e, ok := indexes[i][key]
			if !ok {
				missing++
				if len(f.MissingIn) < 20 {
					f.MissingIn = append(f.MissingIn, d.ID)
				}
				continue
			}
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
		if missing > 0 {
			f.Status = append(f.Status, "missing")
		}
		if missing > 20 {
			f.MissingIn = append(f.MissingIn, fmt.Sprintf("and %d more files; inspect their entries below", missing-20))
		}
		if different {
			f.Status = append(f.Status, "different")
		}
		if mismatch {
			f.Status = append(f.Status, "type mismatch")
		}
		if empty {
			f.Status = append(f.Status, "empty")
		}
		if secret {
			f.Status = append(f.Status, "possible secrets")
		}
		if len(f.Status) > 0 {
			findings = append(findings, f)
		}
	}
	return findings
}
