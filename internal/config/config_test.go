package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func entry(t *testing.T, d *Document, key string) Entry {
	t.Helper()
	for _, e := range d.Entries {
		if e.Key == key {
			return e
		}
	}
	t.Fatalf("missing %s in %+v", key, d.Entries)
	return Entry{}
}
func hasIssue(d *Document, code string) bool {
	for _, i := range d.Diagnostics {
		if i.Code == code {
			return true
		}
	}
	return false
}
func TestSupportedFormats(t *testing.T) {
	cases := []struct{ name, source, key, value, kind string }{
		{".env", "# safe fixture\nexport PORT=8080\nNAME=\"two words\"\nEMPTY=\n", "NAME", "two words", "string"},
		{".env.production", "A='line one\nline two'\nB=ok\n", "A", "line one\nline two", "string"},
		{"config.json", `{"database":{"port":8080},"enabled":true}`, "database.port", "8080", "number"},
		{"config.yml", "database:\n  port: 8080\nenabled: true\n", "database.port", "8080", "number"},
		{"config.toml", "[database]\nport=8080\n", "database.port", "8080", "number"},
		{"config.ini", "[database]\nport=8080\n", "database.port", "8080", "string"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := Parse(c.name, []byte(c.source))
			if !d.Valid {
				t.Fatalf("%+v", d.Diagnostics)
			}
			e := entry(t, d, c.key)
			if e.Value != c.value || e.Type != c.kind {
				t.Fatalf("%+v", e)
			}
		})
	}
}
func TestMalformedInputs(t *testing.T) {
	for name, source := range map[string]string{"x.env": "PASSWORD=\"private-fixture-value", "x.json": `{"password":"private-fixture-value",}`, "x.yaml": "password: [private-fixture-value", "x.toml": "password = [private-fixture-value", "x.ini": "[broken\nprivate-fixture-value"} {
		t.Run(name, func(t *testing.T) {
			d := Parse(name, []byte(source))
			if d.Valid {
				t.Fatal("malformed file accepted")
			}
			b, _ := json.Marshal(d.Diagnostics)
			if strings.Contains(string(b), "private-fixture-value") {
				t.Fatal("parser diagnostic leaked an input value")
			}
		})
	}
}
func TestDuplicates(t *testing.T) {
	for name, source := range map[string]string{"x.env": "A=first\nA=last\n", "x.json": `{"A":"first","A":"last"}`, "x.yaml": "A: first\nA: last\n", "x.ini": "A=first\nA=last\n"} {
		t.Run(name, func(t *testing.T) {
			d := Parse(name, []byte(source))
			if !d.Valid || !hasIssue(d, "duplicate") {
				t.Fatalf("%+v", d.Diagnostics)
			}
			if entry(t, d, "A").Value != "last" {
				t.Fatal("final duplicate value not used")
			}
		})
	}
	d := Parse("x.toml", []byte("A=1\nA=2\n"))
	if d.Valid {
		t.Fatal("TOML duplicate accepted")
	}
}
func TestINIDuplicateEmpty(t *testing.T) {
	for _, source := range []string{"A=\nA=\n", "A=first\nA=\n", "[database]\nA=first\n[database]\nA=last\n"} {
		d := Parse("x.ini", []byte(source))
		if !hasIssue(d, "duplicate") {
			t.Fatalf("duplicate not reported: %q", source)
		}
		if strings.Contains(source, "A=\n") && !entry(t, d, "A").Empty {
			t.Fatal("last empty value lost")
		}
	}
}
func TestINICommentsAndQuotes(t *testing.T) {
	d := Parse("x.ini", []byte("A=\"a; b\"\nURL=https://example.invalid/path#anchor\n"))
	if !d.Valid || entry(t, d, "URL").Value != "https://example.invalid/path#anchor" {
		t.Fatal(d)
	}
}
func TestEnvCommentsOrderingWhitespaceAndInterpolation(t *testing.T) {
	d := Parse(".env", []byte("# hello\n HOST =localhost\nPORT=8080\nURL=\"http://${HOST}:${PORT}\"\nEMPTY=\n"))
	if !d.Valid || !hasIssue(d, "whitespace") {
		t.Fatal(d.Diagnostics)
	}
	if entry(t, d, "URL").Value != "http://localhost:8080" {
		t.Fatal(entry(t, d, "URL"))
	}
	if d.Entries[0].Key != "HOST" || d.Entries[1].Key != "PORT" {
		t.Fatal("order lost")
	}
}
func TestEnvApostropheAndMultiline(t *testing.T) {
	d := Parse("x.env", []byte("LABEL=can't\nSECRET='one\ntwo'\nPORT=8080\n"))
	if !d.Valid || entry(t, d, "PORT").Value != "8080" {
		t.Fatal(d.Diagnostics)
	}
}
func TestEnvInvalidKey(t *testing.T) {
	d := Parse("x.env", []byte("1BAD=value\n"))
	if d.Valid {
		t.Fatal("invalid shell-compatible key accepted")
	}
}
func TestNormalizationCollisionAndArrays(t *testing.T) {
	d := Parse("x.json", []byte(`{"a.b":1,"a":{"b":2},"list":[{"name":"one"},null],"empty":{},"none":[]}`))
	for _, key := range []string{`["a.b"]`, "a.b", "list", "list[0]", "list[0].name", "list[1]", "empty", "none"} {
		entry(t, d, key)
	}
	if !entry(t, d, "empty").Empty || !entry(t, d, "none").Empty {
		t.Fatal("empty containers lost")
	}
}
func TestRootValues(t *testing.T) {
	for _, s := range []string{`null`, `42`, `[]`, `{}`} {
		d := Parse("x.json", []byte(s))
		if !d.Valid || len(d.Entries) != 1 || d.Entries[0].Key != "$" {
			t.Fatal(d)
		}
	}
}
func TestYAMLUnsupportedStructures(t *testing.T) {
	for _, s := range []string{"a: 1\n---\nb: 2", "? [a, b]\n: value", "a: &a\n  b: *a", "a: &a {x: 1}\nb:\n  <<: *a", "a: !custom value"} {
		if d := Parse("x.yaml", []byte(s)); d.Valid {
			t.Fatalf("unsupported structure accepted: %s", s)
		}
	}
}
func TestYAMLAlias(t *testing.T) {
	d := Parse("x.yaml", []byte("a: &a {x: 1}\nb: *a\n"))
	if !d.Valid || entry(t, d, "b.x").Value != "1" {
		t.Fatal(d)
	}
}
func TestJSONTrailingValueAndDeepStructure(t *testing.T) {
	for _, s := range []string{`{} {}`, strings.Repeat("[", MaxDepth+2) + "0" + strings.Repeat("]", MaxDepth+2)} {
		if Parse("x.json", []byte(s)).Valid {
			t.Fatal("invalid structure accepted")
		}
	}
}
func TestEncodingAndLimits(t *testing.T) {
	if !Parse("x.json", append([]byte{0xef, 0xbb, 0xbf}, []byte(`{}`)...)).Valid {
		t.Fatal("UTF8 BOM rejected")
	}
	for _, b := range [][]byte{{0xff, 0xfe, 0, 0}, []byte("a\x00b"), make([]byte, MaxBytes+1)} {
		if Parse("x.json", b).Valid {
			t.Fatal("invalid input accepted")
		}
	}
}
func TestSecretDetectionAndMasking(t *testing.T) {
	for _, key := range []string{"PASSWORD", "PASS", "SECRET", "TOKEN", "API_KEY", "APIKEY", "PRIVATE_KEY", "ACCESS_KEY", "AUTH", "BEARER", "CONNECTION_STRING", "DATABASE_URL"} {
		if !PossibleSecret(key, "fake-value") {
			t.Fatal(key)
		}
	}
	if PossibleSecret("PORT", "8080") || PossibleSecret("PASSWORD", "") {
		t.Fatal("incorrect safe/empty detection")
	}
	if !PossibleSecret("URL", "postgres://user:fakepass@example.invalid/db") {
		t.Fatal("connection URL not detected")
	}
	e := Entry{Key: "SECRET", Value: "fake-only", Type: "string", Secret: true}
	if Visible(e, false).Value == e.Value || Visible(e, true).Value != e.Value {
		t.Fatal("mask or reveal failed")
	}
	e.Secret = false
	if Visible(e, false).Value == e.Value {
		t.Fatal("ordinary values revealed by default")
	}
}
func TestCrossFormatComparison(t *testing.T) {
	a := Parse("a.json", []byte(`{"port":8080,"optional":"","arr":[1,2]}`))
	b := Parse("b.yaml", []byte("port: 8080\narr: [1, 3]\nextra: true\n"))
	a.ID = "a"
	b.ID = "b"
	c := Compare([]*Document{a, b}, Query{Filter: "all"})
	for _, r := range c.Rows {
		if r.Key == "port" && r.Status[0] != "same" {
			t.Fatal(r)
		}
		if r.Key == "arr[1]" && !strings.Contains(strings.Join(r.Status, ","), "different") {
			t.Fatal(r)
		}
		for _, cell := range r.Cells {
			if cell.Present && !cell.Entry.Empty && cell.Entry.Type != "array" && cell.Entry.Value != "••••••••" {
				t.Fatal("comparison leaked value")
			}
		}
	}
	m := Compare([]*Document{a, b}, Query{Filter: "missing"})
	if m.Total != 2 {
		t.Fatal(m)
	}
	bad := Parse("bad.json", []byte("{"))
	errRows := Compare([]*Document{a, bad}, Query{Filter: "errors"})
	for _, r := range errRows.Rows {
		if strings.Contains(strings.Join(r.Status, ","), "missing") {
			t.Fatal("invalid file treated as missing keys")
		}
	}
}
func TestTypeMismatchAndPaging(t *testing.T) {
	a := Parse("a.json", []byte(`{"a":1,"b":2}`))
	b := Parse("b.yaml", []byte("a: '1'\nb: 2\n"))
	c := Compare([]*Document{a, b}, Query{Size: 1, Filter: "all"})
	if c.Total != 2 || c.Pages != 2 || len(c.Rows) != 1 {
		t.Fatal(c)
	}
	if !strings.Contains(strings.Join(c.Rows[0].Status, ","), "type mismatch") {
		t.Fatal(c)
	}
	c = Compare([]*Document{a, b}, Query{Search: "b", Reveal: true})
	if c.Total != 1 || c.Rows[0].Cells[0].Entry.Value != "2" {
		t.Fatal(c)
	}
}
func TestExampleGeneration(t *testing.T) {
	d := Parse(".env", []byte("# secret in comment: fake-password\nPORT=8080\nAPI_KEY=fake-private-token\nURL=https://example.invalid/private\nDEBUG=false\nPORT=9000\n"))
	for _, safe := range []bool{false, true} {
		s, e := Example(d, safe)
		if e != nil {
			t.Fatal(e)
		}
		for _, n := range []string{"fake-password", "fake-private-token", "https://example.invalid/private"} {
			if strings.Contains(s, n) {
				t.Fatal("example leaked input")
			}
		}
		if strings.Count(s, "PORT=") != 1 || !strings.Contains(s, "API_KEY=\n") {
			t.Fatal(s)
		}
		if safe && !strings.Contains(s, "PORT=9000") {
			t.Fatal(s)
		}
		if !safe && strings.Contains(s, "9000") {
			t.Fatal("nonzero defaults not requested")
		}
		if !Parse(".env.example", []byte(s)).Valid {
			t.Fatal("generated example invalid")
		}
	}
	if _, e := Example(Parse("x.json", []byte(`{}`)), false); e == nil {
		t.Fatal("non-env example allowed")
	}
}
func TestMaskedReportsAndHTMLSafety(t *testing.T) {
	d := Parse(".env", []byte("PASSWORD=fake-sensitive-value\nSAFE=fake-public-value\n"))
	d.Name = "<script>alert('x')</script>.env"
	d.Path = "C:/private/path.env"
	for _, f := range []string{"html", "json", "txt"} {
		b, e := Export([]*Document{d}, f, false)
		if e != nil {
			t.Fatal(e)
		}
		for _, n := range []string{"fake-sensitive-value", "fake-public-value", "C:/private/path"} {
			if strings.Contains(string(b), n) {
				t.Fatal("masked report leaked values/paths")
			}
		}
		if f == "html" && strings.Contains(string(b), "<script>") {
			t.Fatal("HTML injection")
		}
		b, e = Export([]*Document{d}, f, true)
		if e != nil || !strings.Contains(string(b), "fake-sensitive-value") {
			t.Fatal("explicit values missing")
		}
	}
}
func TestHundredFilesThousandsOfKeys(t *testing.T) {
	var b strings.Builder
	b.WriteByte('{')
	for i := 0; i < 2000; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "\"KEY_%d\":%d", i, i)
	}
	b.WriteByte('}')
	docs := make([]*Document, 110)
	for i := range docs {
		docs[i] = Parse(fmt.Sprintf("config%d.json", i), []byte(b.String()))
		if !docs[i].Valid {
			t.Fatal(docs[i].Diagnostics)
		}
	}
	c := Compare(docs, Query{Size: 40, Filter: "all"})
	if c.Total != 2000 || len(c.Rows) != 40 || len(c.Rows[0].Cells) != 110 {
		t.Fatal("large comparison failed")
	}
}
func BenchmarkCompare(b *testing.B) {
	docs := []*Document{}
	for i := 0; i < 110; i++ {
		d := &Document{Valid: true}
		for j := 0; j < 2000; j++ {
			d.Add(fmt.Sprintf("key_%d", j), "string", "fake", 0)
		}
		docs = append(docs, d)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Compare(docs, Query{Size: 40})
	}
}
func TestNumericNormalization(t *testing.T) {
	a := Parse("a.json", []byte(`{"n":1e2,"large":9007199254740993}`))
	b := Parse("b.yaml", []byte("n: 100.0\nlarge: 9007199254740993\n"))
	c := Compare([]*Document{a, b}, Query{})
	for _, r := range c.Rows {
		if r.Status[0] != "same" {
			t.Fatal(r)
		}
	}
}
func TestINIMultilineAndQuotedKey(t *testing.T) {
	d := Parse("x.ini", []byte("[database]\n`key=name`=first\n`key=name`=last\nMESSAGE=\"\"\"line one\nline two\"\"\"\n"))
	if !d.Valid {
		t.Fatal(d.Diagnostics)
	}
	if entry(t, d, `database["key=name"]`).Value != "last" || !hasIssue(d, "duplicate") {
		t.Fatal(d)
	}
}
func TestYAMLExpansionLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("a: &a [1, 2, 3, 4, 5, 6, 7, 8, 9, 10]\n")
	previous := "a"
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("level%d", i)
		fmt.Fprintf(&b, "%s: &%s [*%s, *%s, *%s, *%s, *%s, *%s, *%s, *%s, *%s, *%s]\n", name, name, previous, previous, previous, previous, previous, previous, previous, previous, previous, previous)
		previous = name
	}
	if Parse("x.yaml", []byte(b.String())).Valid {
		t.Fatal("alias expansion limit bypassed")
	}
}
func TestReportComparison(t *testing.T) {
	a := Parse("dev.env", []byte("PORT=8080\nPASSWORD=fake-one\n"))
	b := Parse("production.env", []byte("PASSWORD=fake-two\n"))
	raw, e := Export([]*Document{a, b}, "json", false)
	if e != nil {
		t.Fatal(e)
	}
	var report Report
	if e = json.Unmarshal(raw, &report); e != nil {
		t.Fatal(e)
	}
	found := false
	for _, f := range report.Differences {
		if f.Key == "PORT" && len(f.MissingIn) == 1 {
			found = true
		}
	}
	if !found || strings.Contains(string(raw), "fake-one") || strings.Contains(string(raw), "fake-two") {
		t.Fatal("missing comparison or value leak")
	}
}
func FuzzParse(f *testing.F) {
	for _, s := range []string{"{}", "[1,2]", "A=one\nA=two", "a: [1,2]", "[database]\nport=8080", "# comment"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if len(s) > 4096 {
			t.Skip()
		}
		for _, name := range []string{"x.env", "x.json", "x.yaml", "x.toml", "x.ini"} {
			d := Parse(name, []byte(s))
			if d == nil {
				t.Fatal("nil document")
			}
		}
	})
}
func TestINIExplicitDefaultSection(t *testing.T) {
	d := Parse("x.ini", []byte("[DEFAULT]\nA=one\nA=\n"))
	if !d.Valid || !entry(t, d, "A").Empty || !hasIssue(d, "duplicate") {
		t.Fatal(d)
	}
}
func TestTypedTemporalValues(t *testing.T) {
	d := Parse("x.toml", []byte("date=2026-10-06\ntime=12:30:00\nstamp=2026-10-06T12:30:00Z\n"))
	if !d.Valid || entry(t, d, "date").Type != "date" || entry(t, d, "stamp").Type != "datetime" {
		t.Fatal(d)
	}
}
