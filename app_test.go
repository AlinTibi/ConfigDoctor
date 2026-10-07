package main

import (
	"github.com/AlinTibi/ConfigDoctor/internal/config"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAppSaveReviewAndSourceGuard(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.env")
	os.WriteFile(source, []byte("PASSWORD=fake-value\nPORT=8080\n"), 0600)
	d := config.Parse(source, []byte("PASSWORD=fake-value\nPORT=8080\n"))
	d.ID = "fixture"
	a := NewApp()
	a.docs = []*config.Document{d}
	target := filepath.Join(dir, "example.env")
	if _, e := a.SaveExampleTo(d.ID, false, target, false); e == nil {
		t.Fatal("save without review")
	}
	if _, e := a.SaveExampleTo(d.ID, false, source, true); e == nil {
		t.Fatal("source overwrite")
	}
	if _, e := a.SaveExampleTo(d.ID, false, target, true); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(target)
	if strings.Contains(string(b), "fake-value") {
		t.Fatal("example leaked value")
	}
	if _, e := a.SaveReportTo("json", true, filepath.Join(dir, "unconfirmed.json"), true, false); e == nil {
		t.Fatal("values without second confirmation")
	}
	out := filepath.Join(dir, "report.json")
	if _, e := a.SaveReportTo("json", false, out, true, false); e != nil {
		t.Fatal(e)
	}
	b, _ = os.ReadFile(out)
	if strings.Contains(string(b), "fake-value") {
		t.Fatal("masked report leaked value")
	}
	v, e := a.Inspect(d.ID, config.Query{})
	if e != nil || v.Entries[0].Value != "••••••••" {
		t.Fatal("default inspection not masked")
	}
	b, _ = os.ReadFile(source)
	if string(b) != "PASSWORD=fake-value\nPORT=8080\n" {
		t.Fatal("source changed")
	}
}

func TestAppBaselineReportsAndSelection(t *testing.T) {
	a := NewApp()
	actual := config.Parse(".env", []byte("PASSWORD=fake-private-value\nEXTRA=one\nURL=$HOST"))
	baseline := config.Parse(".env.example", []byte("PASSWORD=\nMISSING="))
	actual.ID = "actual"
	baseline.ID = "baseline"
	a.docs = []*config.Document{actual, baseline}
	c, err := a.CompareBaseline("actual", "baseline", config.Query{})
	if err != nil || c.Baseline.Counts.Missing != 1 || c.Baseline.Counts.Extra != 2 {
		t.Fatal("wrong selection or counts")
	}
	if _, err = a.CompareBaseline("actual", "unknown", config.Query{}); err == nil {
		t.Fatal("unloaded baseline accepted")
	}
	if _, err = a.CompareBaseline("actual", "actual", config.Query{}); err == nil {
		t.Fatal("same file accepted")
	}
	for _, format := range []string{"html", "json", "txt"} {
		preview, err := a.PreviewBaselineReport("actual", "baseline", format)
		if err != nil || strings.Contains(preview, "fake-private-value") || !strings.Contains(preview, "missing baseline key") {
			t.Fatal("unsafe/incomplete preview")
		}
		path := filepath.Join(t.TempDir(), "report."+format)
		if _, err = a.SaveBaselineReportTo("actual", "baseline", format, false, path, false, false); err == nil {
			t.Fatal("unreviewed save allowed")
		}
		if _, err = a.SaveBaselineReportTo("actual", "baseline", format, true, path, true, false); err == nil {
			t.Fatal("unconfirmed secrets allowed")
		}
		if _, err = a.SaveBaselineReportTo("actual", "baseline", format, false, path, true, false); err != nil {
			t.Fatal(err)
		}
		raw, _ := os.ReadFile(path)
		if strings.Contains(string(raw), "fake-private-value") {
			t.Fatal("saved report leak")
		}
	}
}
