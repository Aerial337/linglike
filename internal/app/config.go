// Package app holds application configuration and the shared lookup
// service used by the user interface.
package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DictConfig is one configured dictionary file.
type DictConfig struct {
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
	Name    string `json:"name,omitempty"` // optional display name override
}

// Hotkey is a global keyboard shortcut.
type Hotkey struct {
	Ctrl  bool   `json:"ctrl"`
	Alt   bool   `json:"alt"`
	Shift bool   `json:"shift"`
	Win   bool   `json:"win"`
	Key   string `json:"key"` // single character or key name such as "F12"
}

// Config is the persisted application configuration.
type Config struct {
	Dictionaries []DictConfig `json:"dictionaries"`

	// Translation
	TranslateEnabled bool   `json:"translate_enabled"`
	TargetLang       string `json:"target_lang"`
	SourceLang       string `json:"source_lang"` // "auto" or a code
	TranslateInPopup bool   `json:"translate_in_popup"`

	// Capture of selected text
	Hotkey            Hotkey `json:"hotkey"`
	HotkeyEnabled     bool   `json:"hotkey_enabled"`
	CtrlRightClick    bool   `json:"ctrl_right_click"`
	ClipboardWatch    bool   `json:"clipboard_watch"`
	RestoreClipboard  bool   `json:"restore_clipboard"`
	PopupWidth        int    `json:"popup_width"`
	PopupHeight       int    `json:"popup_height"`
	PopupAutoClose    bool   `json:"popup_auto_close"`
	PopupCloseSeconds int    `json:"popup_close_seconds"`

	// Window
	WindowWidth    int  `json:"window_width"`
	WindowHeight   int  `json:"window_height"`
	StartHidden    bool `json:"start_hidden"`
	MinimizeToTray bool `json:"minimize_to_tray"`

	// Search
	MaxSuggestions int      `json:"max_suggestions"`
	History        []string `json:"history,omitempty"`
}

// Default returns the default configuration.
func Default() *Config {
	return &Config{
		TranslateEnabled:  true,
		TargetLang:        "en",
		SourceLang:        "auto",
		TranslateInPopup:  true,
		Hotkey:            Hotkey{Ctrl: true, Alt: true, Key: "D"},
		HotkeyEnabled:     true,
		CtrlRightClick:    true,
		ClipboardWatch:    false,
		RestoreClipboard:  true,
		PopupWidth:        420,
		PopupHeight:       320,
		PopupAutoClose:    true,
		PopupCloseSeconds: 12,
		WindowWidth:       760,
		WindowHeight:      560,
		MinimizeToTray:    true,
		MaxSuggestions:    30,
	}
}

// Dir returns the directory holding the configuration file.
func Dir() string {
	if runtime.GOOS == "windows" {
		if d := os.Getenv("APPDATA"); d != "" {
			return filepath.Join(d, "Linglike")
		}
	}
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "linglike")
}

// Path returns the configuration file path.
func Path() string { return filepath.Join(Dir(), "config.json") }

// Load reads the configuration, returning defaults when the file is missing.
func Load() (*Config, error) {
	cfg := Default()
	b, err := os.ReadFile(Path())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return cfg, nil
		}
		return cfg, err
	}
	if err := json.Unmarshal(b, cfg); err != nil {
		return Default(), err
	}
	if cfg.TargetLang == "" {
		cfg.TargetLang = "en"
	}
	if cfg.SourceLang == "" {
		cfg.SourceLang = "auto"
	}
	if cfg.PopupWidth < 200 {
		cfg.PopupWidth = 420
	}
	if cfg.PopupHeight < 120 {
		cfg.PopupHeight = 320
	}
	if cfg.MaxSuggestions <= 0 {
		cfg.MaxSuggestions = 30
	}
	return cfg, nil
}

// Save writes the configuration.
func (c *Config) Save() error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, Path())
}

// AddHistory records a search term (most recent first, deduplicated).
func (c *Config) AddHistory(term string) {
	if term == "" {
		return
	}
	out := []string{term}
	for _, h := range c.History {
		if h != term {
			out = append(out, h)
		}
		if len(out) >= 50 {
			break
		}
	}
	c.History = out
}

// String renders the hotkey as e.g. "Ctrl+Alt+D".
func (h Hotkey) String() string {
	var parts []string
	if h.Ctrl {
		parts = append(parts, "Ctrl")
	}
	if h.Alt {
		parts = append(parts, "Alt")
	}
	if h.Shift {
		parts = append(parts, "Shift")
	}
	if h.Win {
		parts = append(parts, "Win")
	}
	parts = append(parts, h.Key)
	return strings.Join(parts, "+")
}
