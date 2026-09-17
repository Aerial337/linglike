//go:build windows

// linglike is a Lingoes-style dictionary and translation tool for Windows.
package main

import (
	"log"
	"os"
	"path/filepath"

	"github.com/aerial337/linglike/internal/app"
	"github.com/aerial337/linglike/internal/ui"
)

func main() {
	// Log to a file next to the settings; the process has no console.
	if f, err := os.OpenFile(filepath.Join(app.Dir(), "linglike.log"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
		log.SetOutput(f)
		defer f.Close()
	} else if err := os.MkdirAll(app.Dir(), 0o755); err == nil {
		if f, err := os.Create(filepath.Join(app.Dir(), "linglike.log")); err == nil {
			log.SetOutput(f)
			defer f.Close()
		}
	}
	cfg, err := app.Load()
	if err != nil {
		log.Println("config:", err)
	}
	if len(os.Args) > 1 {
		// Files passed on the command line (e.g. drag & drop onto the exe)
		// are added as dictionaries.
		for _, p := range os.Args[1:] {
			if abs, err := filepath.Abs(p); err == nil {
				cfg.Dictionaries = append(cfg.Dictionaries, app.DictConfig{Path: abs, Enabled: true})
			}
		}
		cfg.Save()
	}
	if len(cfg.Dictionaries) == 0 {
		if n := ui.DiscoverDictionaries(cfg); n > 0 {
			cfg.Save()
		}
	}
	svc := app.NewService(cfg)
	os.Exit(ui.Run(cfg, svc))
}
