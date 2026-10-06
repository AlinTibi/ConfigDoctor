package main

import (
	"context"
	"embed"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if err := checkRuntime(); err != nil {
		showStartupError(err)
		return
	}
	app := NewApp()
	err := wails.Run(&options.App{Title: "Config Doctor", Width: 1400, Height: 900, MinWidth: 1000, MinHeight: 650, AssetServer: &assetserver.Options{Assets: assets}, BackgroundColour: &options.RGBA{R: 14, G: 19, B: 28, A: 255}, OnStartup: app.startup, OnBeforeClose: func(ctx context.Context) bool {
		app.mu.RLock()
		busy := app.busy
		app.mu.RUnlock()
		if busy {
			runtime.MessageDialog(ctx, runtime.MessageDialogOptions{Type: runtime.InfoDialog, Title: "Import in progress", Message: "Wait for the current file import before closing."})
		}
		return busy
	}, Bind: []interface{}{app}, DragAndDrop: &options.DragAndDrop{EnableFileDrop: true, DisableWebViewDrop: true}, Windows: &windows.Options{DisableWindowIcon: false}})
	if err != nil {
		showStartupError(err)
	}
}
