package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/joho/godotenv"
	"github.com/pelletier/go-toml/v2"
	"gopkg.in/ini.v1"
	"gopkg.in/yaml.v3"
)

func Format(name string) string {
	n := strings.ToLower(filepath.Base(name))
	if n == ".env" || strings.HasPrefix(n, ".env.") || strings.HasSuffix(n, ".env") {
		return "env"
	}
	switch strings.ToLower(filepath.Ext(n)) {
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".toml":
		return "toml"
	case ".ini":
		return "ini"
	}
	return ""
}
func Parse(name string, data []byte) *Document {
	d := &Document{Name: filepath.Base(name), Path: name, Format: Format(name), Valid: true, SourceBytes: len(data), Entries: []Entry{}, Diagnostics: []Diagnostic{}}
	if d.Format == "" {
		d.Issue("error", "format", "", 0, "Unsupported configuration format.")
		return d
	}
	if len(data) > MaxBytes {
		d.Issue("error", "limit", "", 0, "File exceeds the 16 MiB limit.")
		return d
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(data) || bytes.Contains(data, []byte{0}) {
		d.Issue("error", "encoding", "", 0, "Use UTF-8 text; binary or other encodings are not supported.")
		return d
	}
	var value any
	var err error
	switch d.Format {
	case "env":
		err = d.parseEnv(string(data))
	case "json":
		dec := json.NewDecoder(bytes.NewReader(data))
		dec.UseNumber()
		value, err = d.jsonValue(dec, "", 0)
		if err == nil {
			_, e := dec.Token()
			if e != io.EOF {
				err = fmt.Errorf("expected exactly one JSON value")
			}
		}
	case "yaml":
		dec := yaml.NewDecoder(bytes.NewReader(data))
		var node yaml.Node
		err = dec.Decode(&node)
		if err == io.EOF {
			err = nil
			value = map[string]any{}
		} else if err == nil {
			value, err = d.yamlValue(node.Content[0], "", 0, map[*yaml.Node]bool{})
		}
		if err == nil {
			var extra yaml.Node
			if e := dec.Decode(&extra); e != io.EOF {
				err = fmt.Errorf("multiple YAML documents are not supported; inspect each document separately")
			}
		}
	case "toml":
		var m map[string]any
		err = toml.Unmarshal(data, &m)
		value = m
	case "ini":
		err = d.parseINI(data)
	}
	if err != nil {
		line := 0
		var se *json.SyntaxError
		if e, ok := err.(*json.SyntaxError); ok {
			se = e
		}
		if se != nil {
			line = bytes.Count(data[:min(int(se.Offset), len(data))], []byte{'\n'}) + 1
		}
		if te, ok := err.(*toml.DecodeError); ok {
			line, _ = te.Position()
		}
		if line == 0 {
			if m := regexp.MustCompile(`(?:line|at line) (\d+)`).FindStringSubmatch(err.Error()); len(m) > 1 {
				line, _ = strconv.Atoi(m[1])
			}
		}
		// Parser errors may echo credential-bearing input. Expose only locations and a safe explanation.
		message := "Invalid " + strings.ToUpper(d.Format) + " syntax or unsupported structure. Check quoting, delimiters and duplicate declarations."
		if strings.Contains(err.Error(), "levels") || strings.Contains(err.Error(), "entries") {
			message = "Configuration exceeds structural limits."
		}
		if strings.Contains(err.Error(), "multiple YAML") {
			message = "Multiple YAML documents are unsupported. Inspect each document separately."
		}
		if strings.Contains(err.Error(), "merge") {
			message = "YAML merge keys are unsupported. Expand merged mappings before comparison."
		}
		d.Issue("error", "syntax", "", line, message)
		d.Entries = []Entry{}
		return d
	}
	if d.Format != "env" && d.Format != "ini" {
		if err = d.Normalize(value, "", 0); err != nil {
			d.Issue("error", "limit", "", 0, "Configuration exceeds structural limits.")
			d.Entries = []Entry{}
		}
	}
	for _, e := range d.Entries {
		if e.Empty {
			d.Issue("warning", "empty", e.Key, 0, "Empty value.")
		}
		if e.Secret {
			d.Issue("warning", "secret", e.Key, 0, "Possible secret. Review before sharing.")
		}
	}
	d.Issue("info", "summary", "", 0, fmt.Sprintf("%d normalized entries inspected. Secret detection is heuristic, not a complete security scan.", len(d.Entries)))
	return d
}

func (d *Document) jsonValue(dec *json.Decoder, path string, depth int) (any, error) {
	d.nodes++
	if d.nodes > MaxEntries {
		return nil, fmt.Errorf("too many entries")
	}
	if depth > MaxDepth {
		return nil, fmt.Errorf("too many levels")
	}
	t, e := dec.Token()
	if e != nil {
		return nil, e
	}
	delim, ok := t.(json.Delim)
	if !ok {
		return t, nil
	}
	switch delim {
	case '{':
		m := map[string]any{}
		for dec.More() {
			t, e := dec.Token()
			if e != nil {
				return nil, e
			}
			k, ok := t.(string)
			if !ok {
				return nil, fmt.Errorf("object key must be a string")
			}
			p := Child(path, k)
			if _, ok = m[k]; ok {
				d.Issue("warning", "duplicate", p, 0, "Duplicate key; the final value is used for comparison.")
			}
			v, e := d.jsonValue(dec, p, depth+1)
			if e != nil {
				return nil, e
			}
			m[k] = v
		}
		_, e = dec.Token()
		return m, e
	case '[':
		a := []any{}
		for dec.More() {
			v, e := d.jsonValue(dec, fmt.Sprintf("%s[%d]", path, len(a)), depth+1)
			if e != nil {
				return nil, e
			}
			a = append(a, v)
			if len(a) > MaxEntries {
				return nil, fmt.Errorf("too many entries")
			}
		}
		_, e = dec.Token()
		return a, e
	}
	return nil, fmt.Errorf("unexpected delimiter")
}
func (d *Document) yamlValue(n *yaml.Node, path string, depth int, active map[*yaml.Node]bool) (any, error) {
	d.nodes++
	if d.nodes > MaxEntries {
		return nil, fmt.Errorf("too many entries")
	}
	if depth > MaxDepth {
		return nil, fmt.Errorf("too many levels")
	}
	if active[n] {
		return nil, fmt.Errorf("cyclic YAML alias")
	}
	active[n] = true
	defer delete(active, n)
	switch n.Kind {
	case yaml.AliasNode:
		return d.yamlValue(n.Alias, path, depth+1, active)
	case yaml.MappingNode:
		m := map[string]any{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Tag == "!!merge" {
				return nil, fmt.Errorf("YAML merge unsupported")
			}
			if k.Kind != yaml.ScalarNode || k.Tag != "!!str" {
				return nil, fmt.Errorf("YAML keys must be strings")
			}
			p := Child(path, k.Value)
			if _, ok := m[k.Value]; ok {
				d.Issue("warning", "duplicate", p, k.Line, "Duplicate key; the final value is used for comparison.")
			}
			v, e := d.yamlValue(n.Content[i+1], p, depth+1, active)
			if e != nil {
				return nil, e
			}
			m[k.Value] = v
		}
		return m, nil
	case yaml.SequenceNode:
		a := []any{}
		for i, c := range n.Content {
			v, e := d.yamlValue(c, fmt.Sprintf("%s[%d]", path, i), depth+1, active)
			if e != nil {
				return nil, e
			}
			a = append(a, v)
		}
		return a, nil
	case yaml.ScalarNode:
		if n.Tag != "!!str" && n.Tag != "!!bool" && n.Tag != "!!int" && n.Tag != "!!float" && n.Tag != "!!null" && n.Tag != "!!timestamp" {
			return nil, fmt.Errorf("unsupported YAML tag")
		}
		var v any
		e := n.Decode(&v)
		return v, e
	}
	return nil, fmt.Errorf("unsupported YAML node")
}

func (d *Document) parseEnv(source string) error {
	// godotenv parses syntax and interpolation without touching process environment.
	values, err := godotenv.Unmarshal(source)
	if err != nil {
		return err
	}
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	seen := map[string]bool{}
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		line := i + 1
		t := strings.TrimSpace(raw)
		if t == "" || strings.HasPrefix(t, "#") {
			d.EnvLines = append(d.EnvLines, EnvLine{Comment: raw, Line: line})
			continue
		}
		t = strings.TrimSpace(strings.TrimPrefix(t, "export "))
		left, right, ok := strings.Cut(t, "=")
		if !ok {
			d.Issue("error", "syntax", "", line, "Expected KEY=value assignment.")
			continue
		}
		key := strings.TrimSpace(left)
		if !envKey(key) {
			d.Issue("error", "syntax", "", line, "Invalid environment variable name.")
			continue
		}
		if left != key || raw != strings.TrimLeft(raw, " \t") {
			d.Issue("warning", "whitespace", key, line, "Whitespace around the key or assignment.")
		}
		v := strings.TrimSpace(right)
		if len(v) > 0 && (v[0] == '\'' || v[0] == '"') {
			quote := v[0]
			part := v[1:]
			for !quoteClosed(part, quote) {
				i++
				if i >= len(lines) {
					return fmt.Errorf("unclosed quoted value")
				}
				part += "\n" + lines[i]
			}
		}
		d.EnvLines = append(d.EnvLines, EnvLine{Key: key, Line: line})
		if seen[key] {
			d.Issue("warning", "duplicate", key, line, "Duplicate variable; the final value is used for comparison.")
			continue
		}
		seen[key] = true
		if err := d.Add(key, "string", values[key], 0); err != nil {
			return err
		}
	}
	return nil
}
func quoteClosed(s string, q byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && q == '"' {
			i++
			continue
		}
		if s[i] == q {
			return true
		}
	}
	return false
}
func envKey(k string) bool {
	if k == "" {
		return false
	}
	for i, c := range k {
		if !(c == '_' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || i > 0 && c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
func (d *Document) parseINI(data []byte) error {
	opts := ini.LoadOptions{AllowNonUniqueSections: true, SpaceBeforeInlineComment: true}
	if _, err := ini.LoadSources(opts, data); err != nil {
		return err
	}
	// Parse each complete assignment with the same library. Shadow-value APIs discard
	// empty duplicates, so they cannot reliably represent the last declaration.
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	header := ""
	section := ""
	sections := map[string]bool{}
	index := map[string]int{}
	for i := 0; i < len(lines); i++ {
		raw := lines[i]
		line := i + 1
		trim := strings.TrimSpace(raw)
		if trim == "" || strings.HasPrefix(trim, "#") || strings.HasPrefix(trim, ";") {
			continue
		}
		if strings.HasPrefix(trim, "[") {
			f, err := ini.LoadSources(opts, []byte(raw))
			if err != nil {
				return err
			}
			list := f.Sections()
			if len(list) < 1 {
				return fmt.Errorf("invalid section")
			}
			section = list[len(list)-1].Name()
			header = raw
			if sections[section] {
				d.Issue("warning", "duplicate", Child("", section), line, "Repeated INI section.")
			}
			sections[section] = true
			continue
		}
		block := raw
		for {
			// Continuations must include the following line even if EOF would be accepted.
			if strings.HasSuffix(strings.TrimSpace(lines[i]), "\\") && i+1 < len(lines) {
				i++
				block += "\n" + lines[i]
				continue
			}
			f, err := ini.LoadSources(opts, []byte(header+"\n"+block))
			if err != nil && i+1 < len(lines) {
				i++
				block += "\n" + lines[i]
				continue
			}
			if err != nil {
				return err
			}
			var keys []*ini.Key
			for _, sec := range f.Sections() {
				keys = append(keys, sec.Keys()...)
			}
			if len(keys) != 1 {
				return fmt.Errorf("ambiguous INI assignment")
			}
			k := keys[0]
			prefix := ""
			if section != "" && section != ini.DefaultSection {
				prefix = Child("", section)
			}
			key := Child(prefix, k.Name())
			v := k.Value()
			if idx, ok := index[key]; ok {
				d.Issue("warning", "duplicate", key, line, "Duplicate INI key; the final value is used for comparison.")
				d.Entries[idx].Value = v
				d.Entries[idx].Empty = strings.TrimSpace(v) == ""
				d.Entries[idx].Secret = PossibleSecret(key, v)
			} else {
				index[key] = len(d.Entries)
				if err = d.Add(key, "string", v, strings.Count(key, ".")); err != nil {
					return err
				}
			}
			break
		}
	}
	return nil
}
