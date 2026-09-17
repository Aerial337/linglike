package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	os.Setenv("XDG_CONFIG_HOME", dir)
	os.Setenv("APPDATA", dir)
	cfg := Default()
	cfg.TargetLang = "fa"
	cfg.ClipboardWatch = true
	cfg.Hotkey = Hotkey{Ctrl: true, Key: "F9"}
	cfg.Dictionaries = []DictConfig{{Path: "a.ld2", Enabled: false}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil { // overwrite existing
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.TargetLang != "fa" || !got.ClipboardWatch || got.Hotkey.Key != "F9" || len(got.Dictionaries) != 1 || got.Dictionaries[0].Enabled {
		t.Fatalf("round trip mismatch: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(Dir(), "config.json")); err != nil {
		t.Fatal(err)
	}
}
