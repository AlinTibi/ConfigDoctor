package config

import (
	"fmt"
	"sort"
	"strings"
)

type EnvReference struct {
	Name  string
	Line  int
	Start int
	End   int
}
type envExpression struct {
	value  string
	tokens map[string]string
}

// References are static diagnostics, not shell execution or value expansion.
// Single quotes, escaped dollars and comments are literals. Complex shell
// operators are deliberately left unclassified instead of guessing defaults.
func scanEnvReferences(raw string, line int) ([]EnvReference, bool) {
	refs := []EnvReference{}
	if strings.HasPrefix(raw, "'") {
		return refs, false
	}
	quoted := strings.HasPrefix(raw, `"`)
	if quoted {
		raw = raw[1:]
	}
	offset := 0
	if quoted {
		offset = 1
	}
	unsupported := false
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\n' {
			line++
		}
		if raw[i] == '\\' && i+1 < len(raw) {
			i++
			continue
		}
		if quoted && raw[i] == '"' {
			break
		}
		if !quoted && raw[i] == '#' && i > 0 && (raw[i-1] == ' ' || raw[i-1] == '\t') {
			break
		}
		if raw[i] != '$' {
			continue
		}
		if i+1 < len(raw) && raw[i+1] == '$' {
			i++
			continue
		}
		start := i + 1
		tokenStart := i
		name := ""
		if start < len(raw) && raw[start] == '{' {
			end := strings.IndexByte(raw[start:], '}')
			if end < 0 {
				unsupported = true
				continue
			}
			end += start
			name = raw[start+1 : end]
			i = end
			if !envKey(name) {
				unsupported = true
				continue
			}
		} else {
			end := start
			for end < len(raw) && (raw[end] == '_' || raw[end] >= 'a' && raw[end] <= 'z' || raw[end] >= 'A' && raw[end] <= 'Z' || end > start && raw[end] >= '0' && raw[end] <= '9') {
				end++
			}
			if end == start {
				continue
			}
			name = raw[start:end]
			i = end - 1
		}
		refs = append(refs, EnvReference{Name: name, Line: line, Start: tokenStart + offset, End: i + 1 + offset})
	}
	return refs, unsupported
}

func (d *Document) diagnoseReferences() {
	defined := make(map[string]bool, len(d.Entries))
	indexes := map[string]int{}
	ordered := make([]string, 0, len(d.Entries))
	for i, e := range d.Entries {
		indexes[e.Key] = i
		defined[e.Key] = true
		ordered = append(ordered, e.Key)
	}
	sort.Strings(ordered)
	for _, key := range ordered {
		seen := map[string]bool{}
		for _, ref := range d.EnvReferences[key] {
			if !defined[ref.Name] && !seen[ref.Name] {
				seen[ref.Name] = true
				d.Diagnostics = append(d.Diagnostics, Diagnostic{Severity: "warning", Code: "unresolved reference", Key: key, Line: ref.Line, Reference: ref.Name, Message: fmt.Sprintf("Variable %s is not defined in this file. Process environment and other files are not consulted.", ref.Name)})
			}
		}
	}
	// Iterative DFS bounds stack usage for long chains. Emit a back-edge finding
	// without recursively expanding values or copying credential-bearing text.
	type frame struct {
		key  string
		next int
	}
	state := map[string]byte{}
	resolved := map[string]bool{}
	budget := 0
	for _, root := range ordered {
		if state[root] != 0 {
			continue
		}
		state[root] = 1
		stack := []frame{{key: root}}
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			refs := d.EnvReferences[top.key]
			if top.next >= len(refs) {
				expr := d.envExpressions[top.key]
				ok := true
				for _, ref := range refs {
					if !resolved[ref.Name] {
						ok = false
						break
					}
				}
				if ok {
					value := expr.value
					for token, name := range expr.tokens {
						replacement := d.Entries[indexes[name]].Value
						if len(value)+len(replacement)*strings.Count(value, token) > MaxBytes {
							ok = false
							break
						}
						value = strings.ReplaceAll(value, token, replacement)
					}
					if ok && budget+len(value) <= MaxBytes {
						e := &d.Entries[indexes[top.key]]
						e.Value = value
						e.Empty = strings.TrimSpace(value) == ""
						e.Secret = PossibleSecret(e.Key, value)
						budget += len(value)
						resolved[top.key] = true
					} else {
						d.Issue("warning", "reference limit", top.key, 0, "Reference expansion exceeds the bounded preview limit; the original expression is retained.")
					}
				}
				state[top.key] = 2
				stack = stack[:len(stack)-1]
				continue
			}
			ref := refs[top.next]
			top.next++
			if !defined[ref.Name] {
				continue
			}
			if state[ref.Name] == 1 {
				d.Diagnostics = append(d.Diagnostics, Diagnostic{Severity: "warning", Code: "cyclic reference", Key: top.key, Line: ref.Line, Reference: ref.Name, Message: fmt.Sprintf("Reference to %s closes a variable cycle. This expression is retained without expansion.", ref.Name)})
			} else if state[ref.Name] == 0 {
				state[ref.Name] = 1
				stack = append(stack, frame{key: ref.Name})
			}
		}
	}
	// Propagate secret classification through references, including cycles, without
	// exposing values in diagnostics or resolving from the host environment.
	reverse := map[string][]string{}
	for key, refs := range d.EnvReferences {
		for _, ref := range refs {
			reverse[ref.Name] = append(reverse[ref.Name], key)
		}
	}
	queue := []string{}
	for _, e := range d.Entries {
		if e.Secret {
			queue = append(queue, e.Key)
		}
	}
	for i := 0; i < len(queue); i++ {
		for _, key := range reverse[queue[i]] {
			e := &d.Entries[indexes[key]]
			if !e.Secret {
				e.Secret = true
				queue = append(queue, key)
			}
		}
	}
	d.envExpressions = nil
}
