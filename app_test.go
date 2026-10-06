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
