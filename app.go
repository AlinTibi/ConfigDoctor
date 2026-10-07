package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/AlinTibi/ConfigDoctor/internal/config"
	"github.com/AlinTibi/ConfigDoctor/internal/storage"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx   context.Context
	mu    sync.RWMutex
	docs  []*config.Document
	prefs storage.Settings
	busy  bool
}

func NewApp() *App { return &App{docs: []*config.Document{}, prefs: storage.Default()} }
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	s, e := storage.Load()
	a.prefs = s
	if e != nil {
		runtime.EventsEmit(ctx, "notice", "Settings were unreadable. Safe defaults are active.")
	}
}
func (a *App) GetSettings() storage.Settings { a.mu.RLock(); defer a.mu.RUnlock(); return a.prefs }
func (a *App) SaveSettings(s storage.Settings) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	s.Recent = a.prefs.Recent
	s.MaskSensitive = true
	if !s.RememberRecent {
		s.Recent = []string{}
	}
	if e := storage.Save(s); e != nil {
		return e
	}
	a.prefs = s
	return nil
}
func (a *App) ClearRecent() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.prefs.Recent = []string{}
	return storage.Save(a.prefs)
}
func (a *App) Files() []config.Summary {
	a.mu.RLock()
	defer a.mu.RUnlock()
	s := []config.Summary{}
	for _, d := range a.docs {
		s = append(s, d.Summary())
	}
	return s
}
func (a *App) Clear() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy {
		return fmt.Errorf("wait for the current import")
	}
	a.docs = []*config.Document{}
	return nil
}
func (a *App) Remove(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.busy {
		return fmt.Errorf("wait for the current import")
	}
	for i, d := range a.docs {
		if d.ID == id {
			a.docs = append(a.docs[:i], a.docs[i+1:]...)
			return nil
		}
	}
	return nil
}
func (a *App) AddFiles() ([]config.Summary, error) {
	paths, e := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{Title: "Choose configuration files", Filters: []runtime.FileFilter{{DisplayName: "Configuration files", Pattern: "*.env;*.json;*.yaml;*.yml;*.toml;*.ini;.env;.env.*"}, {DisplayName: "All files (for .env variants)", Pattern: "*"}}})
	if e != nil {
		return nil, e
	}
	return a.LoadPaths(paths)
}
func (a *App) AddFolder() ([]config.Summary, error) {
	p, e := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Choose configuration folder"})
	if e != nil {
		return nil, e
	}
	if p == "" {
		return a.Files(), nil
	}
	return a.LoadPaths([]string{p})
}
func (a *App) LoadPaths(paths []string) ([]config.Summary, error) {
	a.mu.Lock()
	if a.busy {
		a.mu.Unlock()
		return nil, fmt.Errorf("an import is already running")
	}
	a.busy = true
	prefs := a.prefs
	a.mu.Unlock()
	defer func() { a.mu.Lock(); a.busy = false; a.mu.Unlock() }()
	resolved := []string{}
	truncated := false
	for _, p := range paths {
		info, e := os.Lstat(p)
		if e != nil {
			resolved = append(resolved, p)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if !info.IsDir() {
			resolved = append(resolved, p)
			continue
		}
		if prefs.Recursive {
			answer, e := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{Type: runtime.QuestionDialog, Title: "Recursive folder scan", Message: "Scan subfolders too? Hidden, dependency, VCS and linked directories are skipped. Import stops at 500 files.", Buttons: []string{"Scan", "Cancel"}, DefaultButton: "Cancel", CancelButton: "Cancel"})
			if e != nil || answer != "Scan" {
				continue
			}
		}
		visits := 0
		filepath.WalkDir(p, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			visits++
			if visits > 20000 {
				truncated = true
				return filepath.SkipAll
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if entry.IsDir() && path != p {
				n := entry.Name()
				if !prefs.Recursive || strings.HasPrefix(n, ".") || n == "node_modules" || n == "vendor" || n == "dist" || n == "build" {
					return filepath.SkipDir
				}
			}
			if !entry.IsDir() && config.Format(path) != "" {
				resolved = append(resolved, path)
			}
			if len(resolved) >= 500 {
				truncated = true
				return filepath.SkipAll
			}
			return nil
		})
	}
	if len(resolved) > 500 {
		resolved = resolved[:500]
		truncated = true
	}
	for i, path := range resolved {
		abs, e := filepath.Abs(path)
		if e != nil {
			continue
		}
		a.mu.RLock()
		count := len(a.docs)
		total := 0
		sourceBytes := 0
		existing := false
		for _, d := range a.docs {
			if strings.EqualFold(d.Path, abs) {
				existing = true
			}
			total += len(d.Entries)
			sourceBytes += d.SourceBytes
		}
		a.mu.RUnlock()
		if existing {
			continue
		}
		if count >= 500 || total >= 250000 || sourceBytes >= 128<<20 {
			truncated = true
			break
		}
		var d *config.Document
		f, e := os.Open(abs)
		if e == nil {
			var b []byte
			b, e = io.ReadAll(io.LimitReader(f, config.MaxBytes+1))
			f.Close()
			if e == nil {
				d = config.Parse(abs, b)
			}
		}
		if e != nil {
			d = &config.Document{Name: filepath.Base(abs), Path: abs, Format: config.Format(abs), Valid: false, Entries: []config.Entry{}, Diagnostics: []config.Diagnostic{{Severity: "error", Code: "read", Message: "File could not be read."}}}
		}
		if total+len(d.Entries) > 250000 || sourceBytes+d.SourceBytes > 128<<20 {
			truncated = true
			break
		}
		h := sha256.Sum256([]byte(strings.ToLower(abs)))
		d.ID = fmt.Sprintf("%x", h[:8])
		a.mu.Lock()
		a.docs = append(a.docs, d)
		if a.prefs.RememberRecent {
			recent := []string{abs}
			for _, p := range a.prefs.Recent {
				if !strings.EqualFold(p, abs) {
					recent = append(recent, p)
				}
			}
			a.prefs.Recent = recent[:min(15, len(recent))]
		}
		a.mu.Unlock()
		runtime.EventsEmit(a.ctx, "progress", fmt.Sprintf("Imported %d / %d files", i+1, len(resolved)))
	}
	a.mu.Lock()
	if a.prefs.RememberRecent {
		storage.Save(a.prefs)
	}
	a.mu.Unlock()
	if truncated {
		runtime.EventsEmit(a.ctx, "notice", "Import limit reached: 500 files, 250,000 entries, 128 MiB of source text or 20,000 directory items. Narrow the selected folder.")
	}
	return a.Files(), nil
}
func (a *App) Inspect(id string, q config.Query) (config.DocumentView, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, d := range a.docs {
		if d.ID == id {
			return config.Inspect(d, q), nil
		}
	}
	return config.DocumentView{}, fmt.Errorf("choose a loaded file")
}
func (a *App) Compare(q config.Query) config.Comparison {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return config.Compare(a.docs, q)
}
func (a *App) baselineFiles(actualID, baselineID string) (*config.Document, *config.Document, error) {
	var actual, baseline *config.Document
	for _, d := range a.docs {
		if d.ID == actualID {
			actual = d
		}
		if d.ID == baselineID {
			baseline = d
		}
	}
	if actual == nil || baseline == nil {
		return nil, nil, fmt.Errorf("choose both a loaded actual file and a baseline")
	}
	return actual, baseline, nil
}
func (a *App) CompareBaseline(actualID, baselineID string, q config.Query) (config.Comparison, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	actual, baseline, err := a.baselineFiles(actualID, baselineID)
	if err != nil {
		return config.Comparison{}, err
	}
	return config.Baseline(actual, baseline, q)
}
func (a *App) PreviewBaselineReport(actualID, baselineID, format string) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	actual, baseline, err := a.baselineFiles(actualID, baselineID)
	if err != nil {
		return "", err
	}
	data, err := config.Export([]*config.Document{actual, baseline}, format, false, actual, baseline)
	if len(data) > 256*1024 {
		return string(data[:256*1024]) + "\n[Preview excerpt. The complete report will be saved.]", err
	}
	return string(data), err
}
func (a *App) SaveBaselineReportTo(actualID, baselineID, format string, include bool, path string, reviewed, confirmedValues bool) (string, error) {
	if !reviewed || include && !confirmedValues {
		return "", fmt.Errorf("review the destination and explicitly confirm any included values")
	}
	a.mu.RLock()
	actual, baseline, err := a.baselineFiles(actualID, baselineID)
	var data []byte
	if err == nil {
		data, err = config.Export([]*config.Document{actual, baseline}, format, include, actual, baseline)
	}
	a.mu.RUnlock()
	if err != nil {
		return "", err
	}
	return a.writeOutput(data, path)
}
func (a *App) PreviewExample(id string, keepSafe bool) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, d := range a.docs {
		if d.ID == id {
			return config.Example(d, keepSafe)
		}
	}
	return "", fmt.Errorf("choose a loaded .env file")
}
func (a *App) SaveExample(id string, keepSafe bool) (string, error) {
	content, e := a.PreviewExample(id, keepSafe)
	if e != nil {
		return "", e
	}
	return a.saveOutput([]byte(content), ".env.example", "Example configuration", "All files", "*")
}

func (a *App) SaveExampleTo(id string, keepSafe bool, path string, reviewed bool) (string, error) {
	if !reviewed {
		return "", fmt.Errorf("review the example and confirm its destination first")
	}
	content, e := a.PreviewExample(id, keepSafe)
	if e != nil {
		return "", e
	}
	return a.writeOutput([]byte(content), path)
}
func (a *App) ChooseDestination(format string) (string, error) {
	name := "ConfigDoctor-report." + format
	pattern := "*." + format
	if format == "env" {
		name = ".env.example"
		pattern = "*"
	} else if format != "html" && format != "json" && format != "txt" {
		return "", fmt.Errorf("invalid output format")
	}
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: "Choose a new output file", DefaultFilename: name, Filters: []runtime.FileFilter{{DisplayName: "Generated file", Pattern: pattern}}})
}
func (a *App) SaveReportTo(format string, include bool, path string, reviewed bool, confirmedValues bool) (string, error) {
	if !reviewed || include && !confirmedValues {
		return "", fmt.Errorf("review the destination and explicitly confirm any included values")
	}
	a.mu.RLock()
	if len(a.docs) == 0 {
		a.mu.RUnlock()
		return "", fmt.Errorf("load at least one file")
	}
	data, e := config.Export(a.docs, format, include)
	a.mu.RUnlock()
	if e != nil {
		return "", e
	}
	return a.writeOutput(data, path)
}
func (a *App) writeOutput(data []byte, path string) (string, error) {
	if len(data) > 64<<20 {
		return "", fmt.Errorf("output exceeds 64 MiB; export fewer configurations")
	}
	if strings.TrimSpace(path) == "" || !filepath.IsAbs(path) {
		return "", fmt.Errorf("choose a full output path for a new file")
	}
	a.mu.RLock()
	sources := []string{}
	for _, d := range a.docs {
		sources = append(sources, d.Path)
	}
	a.mu.RUnlock()
	if e := storage.WriteNew(path, data, sources); e != nil {
		return "", e
	}
	return path, nil
}
func (a *App) PreviewReport(format string, include bool) (string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if len(a.docs) == 0 {
		return "", fmt.Errorf("load at least one configuration file")
	}
	if include {
		return "", fmt.Errorf("value-bearing reports require the explicit export confirmation")
	}
	data, e := config.Export(a.docs, format, false)
	if len(data) > 256*1024 {
		return string(data[:256*1024]) + "\n[Preview excerpt. The complete report will be saved.]", e
	}
	return string(data), e
}
func (a *App) SaveReport(format string, include bool) (string, error) {
	if include {
		r, e := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{Type: runtime.WarningDialog, Title: "Include configuration values?", Message: "This report will contain real configuration values, including possible secrets. Only export to a private destination. Continue?", Buttons: []string{"Include values", "Cancel"}, DefaultButton: "Cancel", CancelButton: "Cancel"})
		if e != nil || r != "Include values" {
			return "", nil
		}
	}
	a.mu.RLock()
	data, e := config.Export(a.docs, format, include)
	a.mu.RUnlock()
	if e != nil {
		return "", e
	}
	return a.saveOutput(data, "ConfigDoctor-report."+format, "Export report", strings.ToUpper(format)+" report", "*."+format)
}
func (a *App) saveOutput(data []byte, name, title, filter, pattern string) (string, error) {
	p, e := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{Title: title, DefaultFilename: name, Filters: []runtime.FileFilter{{DisplayName: filter, Pattern: pattern}}})
	if e != nil || p == "" {
		return "", e
	}
	r, e := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{Type: runtime.QuestionDialog, Title: "Save new file?", Message: "Save the previewed output to the selected destination? Existing files and source configurations cannot be overwritten.", Buttons: []string{"Save", "Cancel"}, DefaultButton: "Cancel", CancelButton: "Cancel"})
	if e != nil || r != "Save" {
		return "", e
	}
	a.mu.RLock()
	sources := []string{}
	for _, d := range a.docs {
		sources = append(sources, d.Path)
	}
	a.mu.RUnlock()
	e = storage.WriteNew(p, data, sources)
	if e != nil {
		return "", e
	}
	return p, nil
}
