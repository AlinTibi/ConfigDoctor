package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeNewFile(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.env")
	os.WriteFile(source, []byte("A=fake"), 0600)
	if WriteNew(source, []byte("new"), []string{source}) == nil {
		t.Fatal("source overwrite allowed")
	}
	if WriteNew(source, []byte("new"), nil) == nil {
		t.Fatal("existing file overwrite allowed")
	}
	b, _ := os.ReadFile(source)
	if string(b) != "A=fake" {
		t.Fatal("source changed")
	}
	target := filepath.Join(dir, "example.env")
	if e := WriteNew(target, []byte("A="), []string{source}); e != nil {
		t.Fatal(e)
	}
	b, _ = os.ReadFile(target)
	if string(b) != "A=" {
		t.Fatal("incorrect output")
	}
}
func TestSettingsValidation(t *testing.T) {
	s := Default()
	if s.Validate() != nil || !s.MaskSensitive || s.Recursive {
		t.Fatal("unsafe defaults")
	}
	s.Theme = "invalid"
	if s.Validate() == nil {
		t.Fatal("invalid theme")
	}
	s = Default()
	s.ExportFormat = "exe"
	if s.Validate() == nil {
		t.Fatal("invalid export format")
	}
}
func TestSettingsPersistOnlyPathsAndSafeMask(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	s := Default()
	s.RememberRecent = true
	s.MaskSensitive = false
	s.Recent = []string{"C:/fake-project/sample.env"}
	if e := Save(s); e != nil {
		t.Fatal(e)
	}
	loaded, e := Load()
	if e != nil || !loaded.MaskSensitive || len(loaded.Recent) != 1 {
		t.Fatal(loaded, e)
	}
	s.RememberRecent = false
	if e = Save(s); e != nil {
		t.Fatal(e)
	}
	loaded, e = Load()
	if e != nil || len(loaded.Recent) != 0 {
		t.Fatal("recent paths not cleared")
	}
}
