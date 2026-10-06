package storage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Settings struct {
	Theme          string   `json:"theme"`
	MaskSensitive  bool     `json:"maskSensitive"`
	RememberRecent bool     `json:"rememberRecent"`
	ExportFormat   string   `json:"exportFormat"`
	Recursive      bool     `json:"recursive"`
	Recent         []string `json:"recent"`
}

func Default() Settings {
	return Settings{Theme: "dark", MaskSensitive: true, ExportFormat: "html", Recent: []string{}}
}
func (s Settings) Validate() error {
	if s.Theme != "dark" && s.Theme != "system" {
		return fmt.Errorf("invalid theme")
	}
	if s.ExportFormat != "html" && s.ExportFormat != "json" && s.ExportFormat != "txt" {
		return fmt.Errorf("invalid export format")
	}
	return nil
}
func Path() (string, error) {
	p, e := os.UserConfigDir()
	return filepath.Join(p, "ALMARFELD", "ConfigDoctor", "settings.json"), e
}
func Load() (Settings, error) {
	s := Default()
	p, e := Path()
	if e != nil {
		return s, e
	}
	b, e := os.ReadFile(p)
	if os.IsNotExist(e) {
		return s, nil
	}
	if e != nil {
		return s, e
	}
	if e = json.Unmarshal(b, &s); e != nil {
		return Default(), fmt.Errorf("settings could not be read")
	}
	if e = s.Validate(); e != nil {
		return Default(), e
	}
	s.MaskSensitive = true
	if !s.RememberRecent {
		s.Recent = []string{}
	}
	return s, nil
}
func Save(s Settings) error {
	if e := s.Validate(); e != nil {
		return e
	}
	s.MaskSensitive = true
	if !s.RememberRecent {
		s.Recent = []string{}
	}
	if len(s.Recent) > 15 {
		s.Recent = s.Recent[:15]
	}
	b, e := json.MarshalIndent(s, "", "  ")
	if e != nil {
		return e
	}
	p, e := Path()
	if e != nil {
		return e
	}
	if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		return e
	}
	return os.WriteFile(p, b, 0600)
}

// WriteNew creates a destination exclusively, without overwriting a source or existing file.
func WriteNew(path string, data []byte, sources []string) error {
	target, e := filepath.Abs(path)
	if e != nil {
		return e
	}
	for _, source := range sources {
		p, _ := filepath.Abs(source)
		if strings.EqualFold(filepath.Clean(p), filepath.Clean(target)) {
			return fmt.Errorf("the destination is a source file; choose a new file")
		}
	}
	f, e := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		if os.IsExist(e) {
			return fmt.Errorf("destination already exists; choose a new file name")
		}
		return fmt.Errorf("cannot create destination")
	}
	n, e := f.Write(data)
	if e == nil && n != len(data) {
		e = fmt.Errorf("incomplete write")
	}
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil {
		os.Remove(target)
		return fmt.Errorf("could not write complete output")
	}
	return closeErr
}
