package config

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestBaselineClassification(t *testing.T) {
	for _, tc := range []struct {
		name, actual, baseline         string
		missing, present, extra, empty int
	}{
		{"matches", "A=value\nB=two", "A=\nB=default", 0, 2, 0, 0},
		{"missing", "A=value", "A=\nB=", 1, 1, 0, 0},
		{"multiple missing", "A=value", "A=\nB=\nC=", 2, 1, 0, 0},
		{"extra", "A=value\nEXTRA=value", "A=", 0, 1, 1, 0},
		{"empty actual", "A=", "A=default", 0, 1, 0, 1},
		{"comments", "A=value", "# B=comment only\nA= # no requiredness convention", 0, 1, 0, 0},
		{"quoted", "A=\"two words\"\nB='Bună ziua 🙂'", "A=default\nB=", 0, 2, 0, 0},
		{"CRLF", "A=value\r\nB=\r\n", "A=\r\nB=\r\nC=\r\n", 1, 2, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := Parse(".env", []byte(tc.actual))
			b := Parse(".env.example", []byte(tc.baseline))
			a.ID = "actual"
			b.ID = "baseline"
			c, err := Baseline(a, b, Query{})
			if err != nil {
				t.Fatal(err)
			}
			n := c.Baseline.Counts
			if n.Missing != tc.missing || n.Present != tc.present || n.Extra != tc.extra || n.Empty != tc.empty {
				t.Fatalf("wrong counts: %+v", n)
			}
			for _, row := range c.Rows {
				for _, cell := range row.Cells {
					if cell.Present && !cell.Entry.Empty && cell.Entry.Value != "••••••••" {
						t.Fatal("value exposed")
					}
				}
			}
			missing, _ := Baseline(a, b, Query{Filter: "missing"})
			if missing.Total != tc.missing {
				t.Fatal("missing filter")
			}
			extra, _ := Baseline(a, b, Query{Filter: "extra"})
			if extra.Total != tc.extra {
				t.Fatal("extra filter")
			}
			warnings, _ := Baseline(a, b, Query{Filter: "warnings"})
			for _, row := range warnings.Rows {
				if len(row.Status) == 1 && row.Status[0] == "extra" {
					t.Fatal("extra marked warning")
				}
			}
		})
	}
}

func TestBaselineInvalidAndSelections(t *testing.T) {
	a := Parse(".env", []byte("A=value"))
	b := Parse(".env.example", []byte("A=value"))
	bad := Parse(".env", []byte("A=ok\nmalformed line"))
	for _, pair := range [][2]*Document{{nil, b}, {a, nil}, {a, a}, {a, bad}, {bad, b}} {
		if _, err := Baseline(pair[0], pair[1], Query{}); err == nil {
			t.Fatal("unsafe baseline accepted")
		}
	}
}

func TestBaselineOtherFormats(t *testing.T) {
	for _, tc := range []struct{ name, actual, baseline string }{
		{"json", `{"service":{"port":8080,"token":"fake-secret"},"extra":true}`, `{"service":{"port":9000,"token":"","host":"localhost"}}`},
		{"yaml", "service:\n  port: 8080\n  token: fake-secret\nextra: true", "service:\n  port: 9000\n  token: ''\n  host: localhost"},
		{"toml", "extra=true\n[service]\nport=8080\ntoken='fake-secret'", "[service]\nport=9000\ntoken=''\nhost='localhost'"},
		{"ini", "extra=true\n[service]\nport=8080\ntoken=fake-secret", "[service]\nport=9000\ntoken=\nhost=localhost"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := Parse("actual."+tc.name, []byte(tc.actual))
			b := Parse("baseline."+tc.name, []byte(tc.baseline))
			c, err := Baseline(a, b, Query{})
			if err != nil || c.Baseline.Counts.Missing != 1 || c.Baseline.Counts.Extra != 1 {
				t.Fatalf("%v %+v", err, c.Baseline)
			}
			if generic := Compare([]*Document{a, b}, Query{}); generic.Total != c.Total {
				t.Fatal("generic comparison regressed")
			}
		})
	}
}

func TestBaselineReportsAndPaging(t *testing.T) {
	a := Parse(".env", []byte("PASSWORD=fake-private-value\nURL=https://${HOST}\nEMPTY=\nEXTRA=fake-extra-value"))
	b := Parse(".env.example", []byte("PASSWORD=\nURL=\nEMPTY=\nMISSING="))
	a.ID = "a"
	b.ID = "b"
	a.Path = "C:/private/path/.env"
	for _, format := range []string{"html", "json", "txt"} {
		t.Run(format, func(t *testing.T) {
			raw, err := Export([]*Document{a, b}, format, false, a, b)
			if err != nil {
				t.Fatal(err)
			}
			for _, private := range []string{"fake-private-value", "fake-extra-value", "https://${HOST}", "C:/private/path"} {
				if strings.Contains(string(raw), private) {
					t.Fatal("masked report leak")
				}
			}
			for _, status := range []string{"missing baseline key", "extra", "unresolved reference", "HOST"} {
				if !strings.Contains(string(raw), status) {
					t.Fatal("report missing classification")
				}
			}
			if format == "json" {
				var r Report
				if err = json.Unmarshal(raw, &r); err != nil || r.Baseline == nil || r.Baseline.Counts.Missing != 1 {
					t.Fatal("baseline report metadata")
				}
			}
		})
	}
	c, err := Baseline(a, b, Query{Size: 1, Page: 1})
	if err != nil || c.Total != 5 || c.Pages != 5 || len(c.Rows) != 1 || c.Baseline.Counts.Empty != 1 {
		t.Fatal("paging or unfiltered counts")
	}
	c, err = Baseline(a, b, Query{Filter: "references"})
	if err != nil || c.Total != 1 || c.Rows[0].Key != "URL" {
		t.Fatal("reference filter")
	}
}

func TestEnvReferenceDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, source       string
		unresolved, cycles int
	}{
		{"braced resolved", "HOST=localhost\nURL=https://${HOST}", 0, 0},
		{"braced unresolved", "URL=https://${HOST}", 1, 0},
		{"bare resolved", "HOST=localhost\nURL=https://$HOST", 0, 0},
		{"bare unresolved", "URL=https://$HOST", 1, 0},
		{"forward chain", "A=${B}\nB=${C}\nC=value", 0, 0},
		{"cycle", "A=${B}\nB=${A}", 0, 1},
		{"self cycle", "A=$A", 0, 1},
		{"empty defined", "HOST=\nURL=${HOST}", 0, 0},
		{"escaped", `A=\${MISSING}` + "\n" + `B="\$OTHER"`, 0, 0},
		{"single quoted", `A='${MISSING} $OTHER'`, 0, 0},
		{"comment", "A=value # $MISSING\n# B=${OTHER}", 0, 0},
		{"Unicode", "MESSAGE=Bună ziua 🙂\nURL=$HOST", 1, 0},
		{"LF CRLF", "A=${B}\r\nB=$C\r\nC=ok\r\n", 0, 0},
		{"double quotes", `A="prefix-${MISSING}"`, 1, 0},
		{"lowercase", "host=localhost\nURL=${host}", 0, 0},
		{"final duplicate", "A=$MISSING\nA=value", 0, 0},
		{"complex default", `A=${MISSING:-localhost}`, 0, 0},
		{"literal double dollar", `A=$$MISSING`, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := Parse(".env", []byte(tc.source))
			if !d.Valid {
				t.Fatal(d.Diagnostics)
			}
			u, c := 0, 0
			for _, issue := range d.Diagnostics {
				if issue.Code == "unresolved reference" {
					u++
				}
				if issue.Code == "cyclic reference" {
					c++
				}
				if issue.Code == "unresolved reference" || issue.Code == "cyclic reference" {
					if issue.Reference == "" || issue.Key == "" || issue.Line < 1 {
						t.Fatal("reference context missing")
					}
				}
			}
			if u != tc.unresolved || c != tc.cycles {
				t.Fatalf("counts %d,%d expected %d,%d", u, c, tc.unresolved, tc.cycles)
			}
			view := Inspect(d, Query{Filter: "references"})
			if u+c > 0 && view.Total == 0 {
				t.Fatal("inspection filter")
			}
		})
	}
}

func TestEnvNoProcessEnvironmentAndSecretLeak(t *testing.T) {
	t.Setenv("HOST", "fake-host-process-value")
	d := Parse(".env", []byte("PASSWORD=fake-private-fixture\nURL=https://${HOST}\nDATABASE_URL=postgres://${USER}:${PASSWORD}@${HOST}/app"))
	if entry(t, d, "URL").Value != "https://${HOST}" {
		t.Fatal("reference was erased or process environment used")
	}
	raw, _ := json.Marshal(d.Diagnostics)
	if strings.Contains(string(raw), "fake-private-fixture") || strings.Contains(string(raw), "fake-host-process-value") {
		t.Fatal("diagnostic leaked value")
	}
	for _, key := range []string{"PASSWORD", "SECRET", "TOKEN", "API_KEY", "PRIVATE_KEY", "AUTH", "DATABASE_URL"} {
		if !PossibleSecret(key, "fake-only") {
			t.Fatal(key)
		}
	}
}

func TestEnvLongChainAndLineNumbers(t *testing.T) {
	var source strings.Builder
	for i := 0; i < 10000; i++ {
		fmt.Fprintf(&source, "A%d=${A%d}\n", i, i+1)
	}
	source.WriteString("A10000=final\n")
	d := Parse(".env", []byte(source.String()))
	if !d.Valid || hasIssue(d, "unresolved reference") || hasIssue(d, "cyclic reference") {
		t.Fatal("long chain failed")
	}
	if entry(t, d, "A0").Value != "final" {
		t.Fatal("forward chain did not resolve")
	}
	d = Parse(".env", []byte("A=\"first\n${MISSING}\"\n"))
	for _, issue := range d.Diagnostics {
		if issue.Code == "unresolved reference" && issue.Line != 2 {
			t.Fatal("wrong multiline location")
		}
	}
}

func TestEnvExpressionValuesAndSecretPropagation(t *testing.T) {
	d := Parse(".env", []byte("URL=pre-${PASSWORD}-$PASSWORD\nPASSWORD=fake-secret-fixture\nLITERAL='${PASSWORD}'\nESCAPED=\"\\$PASSWORD\"\n"))
	if !d.Valid || entry(t, d, "URL").Value != "pre-fake-secret-fixture-fake-secret-fixture" || !entry(t, d, "URL").Secret {
		t.Fatal("resolved values or inherited secret classification")
	}
	if entry(t, d, "LITERAL").Value != "${PASSWORD}" || entry(t, d, "ESCAPED").Value != "$PASSWORD" {
		t.Fatal("literal value expanded")
	}
	for _, format := range []string{"json", "txt", "html"} {
		raw, err := Export([]*Document{d}, format, false)
		if err != nil || strings.Contains(string(raw), "fake-secret-fixture") {
			t.Fatal("resolved secret report leak")
		}
	}
	d = Parse(".env", []byte("A=${B}\nB=${A}\n"))
	if entry(t, d, "A").Value != "${B}" || entry(t, d, "B").Value != "${A}" {
		t.Fatal("cycle erased")
	}
	d = Parse(".env", []byte("A=value\nA=$MISSING"))
	if !hasIssue(d, "unresolved reference") || entry(t, d, "A").Value != "$MISSING" {
		t.Fatal("last duplicate expression not retained")
	}
}

func TestEnvExpansionBudget(t *testing.T) {
	var source strings.Builder
	source.WriteString("A0=abcdefgh\n")
	for i := 1; i < 24; i++ {
		fmt.Fprintf(&source, "A%d=${A%d}${A%d}\n", i, i-1, i-1)
	}
	d := Parse(".env", []byte(source.String()))
	if !d.Valid || !hasIssue(d, "reference limit") {
		t.Fatal("expansion not bounded")
	}
	if len(entry(t, d, "A23").Value) > MaxBytes {
		t.Fatal("expanded value over limit")
	}
}
