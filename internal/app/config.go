// Package app holds application configuration and the shared lookup
// service used by the user interface.
package app

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
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

// LLMConfig configures translation through a local (or remote)
// OpenAI-compatible chat completions server.
type LLMConfig struct {
	Enabled bool   `json:"enabled"`
	URL     string `json:"url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
	// Prompt template with {text}, {target} and {source} placeholders.
	Prompt string `json:"prompt"`
	// TargetLang is a language name for the prompt; empty means use the
	// global target language.
	TargetLang string `json:"target_lang"`
	InPopup    bool   `json:"in_popup"`
	TimeoutSec int    `json:"timeout_sec"`
}

// Config is the persisted application configuration.
type Config struct {
	Dictionaries []DictConfig `json:"dictionaries"`

	// Translation
	TranslateEnabled bool      `json:"translate_enabled"`
	TargetLang       string    `json:"target_lang"`
	SourceLang       string    `json:"source_lang"` // "auto" or a code
	TranslateInPopup bool      `json:"translate_in_popup"`
	LLM              LLMConfig `json:"llm"`

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
	// SelectionPopup opens the popup as soon as text is selected with the
	// mouse: "off", "always", "ctrl", "shift" or "alt" (modifier that must
	// be held while selecting).
	SelectionPopup string `json:"selection_popup"`
	// CloseOnMouseLeave closes the popup when the mouse moves farther than
	// MouseLeaveDistance pixels away from it.
	CloseOnMouseLeave  bool `json:"close_on_mouse_leave"`
	MouseLeaveDistance int  `json:"mouse_leave_distance"`

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
		TranslateEnabled: true,
		TargetLang:       "en",
		SourceLang:       "auto",
		TranslateInPopup: true,
		LLM: LLMConfig{
			URL:        "http://localhost:11434",
			Model:      "llama3.1",
			InPopup:    true,
			TimeoutSec: 90,
		},
		Hotkey:             Hotkey{Ctrl: true, Alt: true, Key: "D"},
		HotkeyEnabled:      true,
		CtrlRightClick:     true,
		ClipboardWatch:     false,
		RestoreClipboard:   true,
		PopupWidth:         420,
		PopupHeight:        320,
		PopupAutoClose:     true,
		PopupCloseSeconds:  12,
		SelectionPopup:     "always",
		CloseOnMouseLeave:  true,
		MouseLeaveDistance: 60,
		WindowWidth:        760,
		WindowHeight:       560,
		MinimizeToTray:     true,
		MaxSuggestions:     30,
	}
}

var (
	dirOnce sync.Once
	dirVal  string
)

// Dir returns the directory holding the configuration file.
//
// Portable mode: if a config.json exists next to the executable, that
// directory is used. Otherwise %APPDATA%\Linglike (Windows) or the user
// configuration directory is used, falling back to the executable's
// directory when neither is available.
func Dir() string {
	dirOnce.Do(func() {
		exeDir := ""
		if exe, err := os.Executable(); err == nil {
			exeDir = filepath.Dir(exe)
			if _, err := os.Stat(filepath.Join(exeDir, "config.json")); err == nil {
				dirVal = exeDir
				return
			}
		}
		if runtime.GOOS == "windows" {
			if d := os.Getenv("APPDATA"); d != "" {
				dirVal = filepath.Join(d, "Linglike")
				return
			}
			if d := os.Getenv("USERPROFILE"); d != "" {
				dirVal = filepath.Join(d, "AppData", "Roaming", "Linglike")
				return
			}
		}
		if d, err := os.UserConfigDir(); err == nil && d != "" {
			dirVal = filepath.Join(d, "linglike")
			return
		}
		if exeDir != "" {
			dirVal = exeDir
			return
		}
		dirVal = "."
	})
	return dirVal
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
		log.Printf("config: cannot parse %s: %v", Path(), err)
		return Default(), err
	}
	log.Printf("config: loaded %s (target %s, hotkey %s, %d dictionaries)", Path(), cfg.TargetLang, cfg.Hotkey, len(cfg.Dictionaries))
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
	if cfg.Hotkey.Key == "" {
		cfg.Hotkey = Default().Hotkey
	}
	switch cfg.SelectionPopup {
	case "off", "always", "ctrl", "shift", "alt":
	default:
		cfg.SelectionPopup = "off"
	}
	if cfg.MouseLeaveDistance <= 0 {
		cfg.MouseLeaveDistance = 60
	}
	if cfg.LLM.TimeoutSec <= 0 {
		cfg.LLM.TimeoutSec = 90
	}
	if strings.TrimSpace(cfg.LLM.URL) == "" {
		cfg.LLM.URL = Default().LLM.URL
	}
	return cfg, nil
}

// Save writes the configuration. Errors are also logged.
func (c *Config) Save() error {
	err := c.save()
	if err != nil {
		log.Printf("config: save to %s failed: %v", Path(), err)
	} else {
		log.Printf("config: saved to %s (target %s, hotkey %s)", Path(), c.TargetLang, c.Hotkey)
	}
	return err
}

func (c *Config) save() error {
	if err := os.MkdirAll(Dir(), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err == nil {
		if err := os.Rename(tmp, Path()); err == nil {
			return nil
		}
		os.Remove(tmp)
	}
	// Fall back to writing the file directly.
	return os.WriteFile(Path(), b, 0o644)
}

// LLMTarget returns the language name the LLM should translate into.
func (c *Config) LLMTarget() string {
	if t := strings.TrimSpace(c.LLM.TargetLang); t != "" {
		return t
	}
	return c.TargetLang
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
