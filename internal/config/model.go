package config

import (
	"encoding/json"
	"fmt"
	"github.com/pelletier/go-toml/v2"
	"regexp"
	"sort"
	"strings"
	"time"
)

const MaxBytes = 16 << 20
const MaxEntries = 100000
const MaxDepth = 64

type Diagnostic struct {
	Severity string `json:"severity"`
	Code     string `json:"code"`
	Key      string `json:"key"`
	Line     int    `json:"line"`
	Message  string `json:"message"`
}
type Entry struct {
	Key    string `json:"key"`
	Type   string `json:"type"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
	Empty  bool   `json:"empty"`
	Depth  int    `json:"depth"`
}
type EnvLine struct {
	Comment string
	Key     string
	Line    int
}
type Document struct {
	nodes       int
	SourceBytes int          `json:"-"`
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Path        string       `json:"path"`
	Format      string       `json:"format"`
	Valid       bool         `json:"valid"`
	Entries     []Entry      `json:"entries"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	EnvLines    []EnvLine    `json:"-"`
}
type Summary struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path"`
	Format   string `json:"format"`
	Valid    bool   `json:"valid"`
	Keys     int    `json:"keys"`
	Errors   int    `json:"errors"`
	Warnings int    `json:"warnings"`
	Empty    int    `json:"empty"`
	Secrets  int    `json:"secrets"`
}

func (d *Document) Issue(level, code, key string, line int, message string) {
	d.Diagnostics = append(d.Diagnostics, Diagnostic{level, code, key, line, message})
	if level == "error" {
		d.Valid = false
	}
}
func (d Document) Summary() Summary {
	s := Summary{ID: d.ID, Name: d.Name, Path: d.Path, Format: d.Format, Valid: d.Valid, Keys: len(d.Entries)}
	for _, i := range d.Diagnostics {
		if i.Severity == "error" {
			s.Errors++
		}
		if i.Severity == "warning" {
			s.Warnings++
		}
	}
	for _, e := range d.Entries {
		if e.Empty {
			s.Empty++
		}
		if e.Secret {
			s.Secrets++
		}
	}
	return s
}
func Visible(e Entry, reveal bool) Entry {
	if !reveal && e.Type != "object" && e.Type != "array" && !e.Empty {
		e.Value = "••••••••"
	}
	return e
}

var simpleKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`)
var sensitiveKey = regexp.MustCompile(`(?i)(password|passwd|(^|[^a-z])pass([^a-z]|$)|secret|token|api[_-]?key|private[_-]?key|access[_-]?key|auth|bearer|connection[_-]?string|database[_-]?url)`)
var sensitiveValue = regexp.MustCompile(`(?i)(-----BEGIN .*PRIVATE KEY-----|\b(?:gh[pousr]_[A-Za-z0-9_]{20,}|github_pat_[A-Za-z0-9_]{20,}|sk-[A-Za-z0-9_-]{20,}|AKIA[A-Z0-9]{16})\b|^[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}$|[a-z][a-z0-9+.-]*://[^\s/:]+:[^\s@]+@)`)

func PossibleSecret(key, value string) bool {
	return strings.TrimSpace(value) != "" && (sensitiveKey.MatchString(key) || sensitiveValue.MatchString(value))
}
func Child(parent, key string) string {
	if simpleKey.MatchString(key) {
		if parent == "" {
			return key
		}
		return parent + "." + key
	}
	b, _ := json.Marshal(key)
	return parent + "[" + string(b) + "]"
}
func (d *Document) Add(key, kind, value string, depth int) error {
	if len(d.Entries) >= MaxEntries {
		return fmt.Errorf("configuration exceeds %d normalized entries", MaxEntries)
	}
	e := Entry{Key: key, Type: kind, Value: value, Depth: depth, Secret: PossibleSecret(key, value), Empty: kind == "null" || (kind == "string" && strings.TrimSpace(value) == "") || ((kind == "object" || kind == "array") && value == "0 items")}
	if kind == "object" || kind == "array" {
		e.Secret = false
	}
	d.Entries = append(d.Entries, e)
	return nil
}
func (d *Document) Normalize(value any, path string, depth int) error {
	if depth > MaxDepth {
		return fmt.Errorf("structure exceeds %d levels", MaxDepth)
	}
	switch v := value.(type) {
	case map[string]any:
		if path != "" || len(v) == 0 {
			p := path
			if p == "" {
				p = "$"
			}
			if err := d.Add(p, "object", fmt.Sprintf("%d items", len(v)), depth); err != nil {
				return err
			}
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if err := d.Normalize(v[k], Child(path, k), depth+1); err != nil {
				return err
			}
		}
	case []any:
		if path == "" {
			path = "$"
		}
		if err := d.Add(path, "array", fmt.Sprintf("%d items", len(v)), depth); err != nil {
			return err
		}
		for i, x := range v {
			if err := d.Normalize(x, fmt.Sprintf("%s[%d]", path, i), depth+1); err != nil {
				return err
			}
		}
	default:
		if path == "" {
			path = "$"
		}
		kind := "string"
		val := fmt.Sprint(v)
		switch v.(type) {
		case nil:
			kind = "null"
			val = "null"
		case bool:
			kind = "boolean"
		case json.Number, int, int64, uint64, float64:
			kind = "number"
		case time.Time:
			kind = "datetime"
			val = v.(time.Time).UTC().Format(time.RFC3339Nano)
		case toml.LocalDate:
			kind = "date"
		case toml.LocalTime:
			kind = "time"
		case toml.LocalDateTime:
			kind = "local datetime"
		}
		return d.Add(path, kind, val, depth)
	}
	return nil
}
