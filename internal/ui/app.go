//go:build windows

package ui

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aerial337/linglike/internal/app"
	"github.com/aerial337/linglike/internal/dict"
	"github.com/lxn/walk"
	"github.com/lxn/win"
)

// App wires the windows, tray icon and text capture together.
type App struct {
	cfg  *app.Config
	svc  *app.Service
	host *htmlHost
	icon *walk.Icon

	main  *mainWindow
	popup *popupWindow
	tray  *walk.NotifyIcon
	msg   *msgWindow
	hook  *mouseHook

	hotkeyRegistered bool
	clipboardHooked  bool
	ignoreClipUntil  time.Time
	lastClipText     string
	capturing        bool
	querySeq         int
}

// Run starts the application and blocks until it exits.
func Run(cfg *app.Config, svc *app.Service) int {
	if !singleInstance("Local\\LinglikeSingleInstance") {
		walk.MsgBox(nil, "Linglike", "Linglike is already running. Look for its icon in the notification area.", walk.MsgBoxIconInformation)
		return 0
	}
	a := &App{cfg: cfg, svc: svc}
	var err error
	if a.host, err = newHTMLHost(); err != nil {
		walk.MsgBox(nil, "Linglike", "Cannot create temporary files: "+err.Error(), walk.MsgBoxIconError)
		return 1
	}
	defer a.host.cleanup()
	a.icon, _ = walk.NewIconFromResource("APP")

	if a.main, err = newMainWindow(a); err != nil {
		walk.MsgBox(nil, "Linglike", "Cannot create main window: "+err.Error(), walk.MsgBoxIconError)
		return 1
	}
	if a.popup, err = newPopupWindow(a); err != nil {
		walk.MsgBox(nil, "Linglike", "Cannot create popup window: "+err.Error(), walk.MsgBoxIconError)
		return 1
	}
	a.setupTray()
	a.setupCapture()
	defer a.teardownCapture()

	a.main.showLoadErrors()
	if !cfg.StartHidden {
		a.main.mw.Show()
	}
	return a.main.mw.Run()
}

// ---- tray ---------------------------------------------------------------

func (a *App) setupTray() {
	ni, err := walk.NewNotifyIcon(a.main.mw)
	if err != nil {
		log.Println("tray:", err)
		return
	}
	a.tray = ni
	if a.icon != nil {
		ni.SetIcon(a.icon)
	}
	ni.SetToolTip("Linglike – dictionary & translation")
	add := func(text string, f func()) *walk.Action {
		act := walk.NewAction()
		act.SetText(text)
		act.Triggered().Attach(f)
		ni.ContextMenu().Actions().Add(act)
		return act
	}
	add("&Show Linglike", a.showMain)
	add("&Look up clipboard text", func() {
		if t, err := walk.Clipboard().Text(); err == nil && strings.TrimSpace(t) != "" {
			a.showPopupAtCursor(t)
		}
	})
	add("&Text Translation...", func() { a.main.showTranslateDialog("") })
	ni.ContextMenu().Actions().Add(walk.NewSeparatorAction())
	hk := add("Enable &hotkey capture", func() {})
	hk.SetCheckable(true)
	hk.SetChecked(a.cfg.HotkeyEnabled)
	hk.Triggered().Attach(func() {
		a.cfg.HotkeyEnabled = hk.Checked()
		a.cfg.Save()
		a.applyCaptureSettings()
	})
	cw := add("&Watch clipboard", func() {})
	cw.SetCheckable(true)
	cw.SetChecked(a.cfg.ClipboardWatch)
	cw.Triggered().Attach(func() {
		a.cfg.ClipboardWatch = cw.Checked()
		a.cfg.Save()
		a.applyCaptureSettings()
	})
	add("&Dictionaries...", func() { a.main.showDictionariesDialog() })
	add("&Options...", func() { a.main.showSettingsDialog() })
	ni.ContextMenu().Actions().Add(walk.NewSeparatorAction())
	add("E&xit", a.exit)
	ni.MouseDown().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			a.showMain()
		}
	})
	ni.SetVisible(true)
}

func (a *App) showMain() {
	mw := a.main.mw
	mw.Show()
	if win.IsIconic(mw.Handle()) {
		win.ShowWindow(mw.Handle(), win.SW_RESTORE)
	}
	win.SetForegroundWindow(mw.Handle())
	a.main.search.SetFocus()
}

func (a *App) exit() {
	a.cfg.Save()
	a.teardownCapture()
	if a.tray != nil {
		a.tray.Dispose()
	}
	walk.App().Exit(0)
}

// ---- text capture (hotkey, Ctrl+right-click, clipboard) ------------------

func (a *App) setupCapture() {
	m, err := newMsgWindow()
	if err != nil {
		log.Println("message window:", err)
		return
	}
	a.msg = m
	m.onHotkey = func() { a.captureSelection() }
	m.onCapture = func(x, y int32) { a.captureSelection() }
	m.onClipboard = a.clipboardChanged
	m.onOutside = func() { a.popup.clickedOutside() }
	a.hook = &mouseHook{
		target:   m.hwnd,
		enabled:  func() bool { return a.cfg.CtrlRightClick },
		popupWnd: func() win.HWND { return a.popup.mw.Handle() },
	}
	if err := a.hook.install(); err != nil {
		log.Println("mouse hook:", err)
	}
	a.applyCaptureSettings()
}

// applyCaptureSettings (re)registers the hotkey and clipboard listener
// according to the configuration.
func (a *App) applyCaptureSettings() {
	if a.msg == nil {
		return
	}
	if a.hotkeyRegistered {
		unregisterHotKey(a.msg.hwnd, hotkeyID)
		a.hotkeyRegistered = false
	}
	if a.cfg.HotkeyEnabled {
		hk := a.cfg.Hotkey
		var mods uint32
		if hk.Ctrl {
			mods |= modControl
		}
		if hk.Alt {
			mods |= modAlt
		}
		if hk.Shift {
			mods |= modShift
		}
		if hk.Win {
			mods |= modWin
		}
		if vk, ok := keyCode(hk.Key); ok {
			if err := registerHotKey(a.msg.hwnd, hotkeyID, mods, vk); err != nil {
				a.main.setStatus("Hotkey " + hk.String() + " is in use by another program")
			} else {
				a.hotkeyRegistered = true
			}
		}
	}
	if a.cfg.ClipboardWatch && !a.clipboardHooked {
		a.clipboardHooked = win.AddClipboardFormatListener(a.msg.hwnd)
	} else if !a.cfg.ClipboardWatch && a.clipboardHooked {
		removeClipboardListener(a.msg.hwnd)
		a.clipboardHooked = false
	}
}

func (a *App) teardownCapture() {
	if a.hook != nil {
		a.hook.uninstall()
	}
	if a.msg != nil {
		if a.hotkeyRegistered {
			unregisterHotKey(a.msg.hwnd, hotkeyID)
			a.hotkeyRegistered = false
		}
		if a.clipboardHooked {
			removeClipboardListener(a.msg.hwnd)
			a.clipboardHooked = false
		}
	}
}

// captureSelection copies the selection of the foreground application via
// a simulated Ctrl+C and shows the popup with the captured text.
func (a *App) captureSelection() {
	if a.capturing {
		return
	}
	fg := win.GetForegroundWindow()
	if fg == a.popup.mw.Handle() {
		return
	}
	a.capturing = true
	seq0 := clipboardSequence()
	oldText := ""
	hadText := false
	if a.cfg.RestoreClipboard {
		if t, err := walk.Clipboard().Text(); err == nil {
			oldText, hadText = t, true
		}
	}
	a.ignoreClipUntil = time.Now().Add(1500 * time.Millisecond)
	sendCopy()

	attempts := 0
	var check func()
	check = func() {
		attempts++
		if clipboardSequence() != seq0 {
			text, err := walk.Clipboard().Text()
			a.capturing = false
			if a.cfg.RestoreClipboard && hadText && text != oldText {
				a.ignoreClipUntil = time.Now().Add(1500 * time.Millisecond)
				walk.Clipboard().SetText(oldText)
			}
			if err == nil && strings.TrimSpace(text) != "" {
				a.showPopupAtCursor(text)
			}
			return
		}
		if attempts >= 8 { // ~1s: nothing was copied (no selection)
			a.capturing = false
			return
		}
		time.AfterFunc(120*time.Millisecond, func() { a.main.mw.Synchronize(check) })
	}
	time.AfterFunc(120*time.Millisecond, func() { a.main.mw.Synchronize(check) })
}

func (a *App) clipboardChanged() {
	if !a.cfg.ClipboardWatch || a.capturing || time.Now().Before(a.ignoreClipUntil) {
		return
	}
	// The clipboard may still be locked by the copying application; retry
	// shortly.
	time.AfterFunc(60*time.Millisecond, func() {
		a.main.mw.Synchronize(func() {
			ok, err := walk.Clipboard().ContainsText()
			if err != nil || !ok {
				return
			}
			text, err := walk.Clipboard().Text()
			if err != nil {
				return
			}
			text = strings.TrimSpace(text)
			if text == "" || text == a.lastClipText || len(text) > 5000 {
				return
			}
			if win.GetForegroundWindow() == a.main.mw.Handle() {
				return // copying inside Linglike itself
			}
			a.lastClipText = text
			a.showPopupAtCursor(text)
		})
	})
}

func (a *App) showPopupAtCursor(text string) {
	text = cleanCapturedText(text)
	if text == "" {
		return
	}
	var pt win.POINT
	win.GetCursorPos(&pt)
	a.popup.showAt(pt, text)
}

// cleanCapturedText normalises captured text: trims, collapses whitespace
// for short selections and strips a trailing punctuation mark from single
// words.
func cleanCapturedText(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 2000 {
		s = s[:2000]
	}
	if !strings.ContainsAny(s, "\n\r") || len(s) < 200 {
		s = strings.Join(strings.Fields(s), " ")
	}
	if len(strings.Fields(s)) == 1 {
		s = strings.TrimRight(s, ".,;:!?\"')]}»")
		s = strings.TrimLeft(s, "\"'([{«")
	}
	return s
}

// ---- queries --------------------------------------------------------------

// runQuery looks text up in the dictionaries and (asynchronously) via
// machine translation. The callback is invoked on the UI thread with the
// initial result and again when the translation arrives. It returns false
// from the second call if a newer query has superseded this one.
func (a *App) runQuery(text string, compact bool, cb func(q *app.Query, done bool)) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	a.querySeq++
	seq := a.querySeq
	q := &app.Query{Text: text}
	q.Results = a.svc.Lookup(text)
	if len(q.Results) == 0 && !app.IsSentence(text) {
		q.Suggestions = a.svc.Suggest(text, 12)
	}
	wantTr := a.cfg.TranslateEnabled && (!compact || a.cfg.TranslateInPopup)
	q.TrPending = wantTr
	cb(q, !wantTr)
	if !wantTr {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		res, err := a.svc.Translate(ctx, text)
		a.main.mw.Synchronize(func() {
			if seq != a.querySeq {
				return
			}
			q.Translation, q.TrErr, q.TrPending = res, err, false
			cb(q, true)
		})
	}()
}

// ---- dictionary discovery -------------------------------------------------

// discoverDictionaries adds every supported dictionary file found in the
// "dictionaries" folder next to the executable and in the configuration
// directory. It returns the number of files added.
func DiscoverDictionaries(cfg *app.Config) int {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "dictionaries"), filepath.Join(filepath.Dir(exe), "dict"))
	}
	dirs = append(dirs, filepath.Join(app.Dir(), "dictionaries"))
	return AddDictionariesFromDirs(cfg, dirs)
}

// AddDictionariesFromDirs scans directories (recursively) for dictionary
// files and appends the new ones to the configuration.
func AddDictionariesFromDirs(cfg *app.Config, dirs []string) int {
	known := map[string]bool{}
	for _, d := range cfg.Dictionaries {
		known[strings.ToLower(d.Path)] = true
	}
	exts := map[string]bool{}
	for _, e := range dict.SupportedExtensions() {
		exts[e] = true
	}
	added := 0
	for _, dir := range dirs {
		filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if !exts[ext] || known[strings.ToLower(p)] {
				return nil
			}
			if ext == ".txt" || ext == ".tsv" {
				// only accept text files that look like dictionaries
				if !looksLikeTextDict(p) {
					return nil
				}
			}
			known[strings.ToLower(p)] = true
			cfg.Dictionaries = append(cfg.Dictionaries, app.DictConfig{Path: p, Enabled: true})
			added++
			return nil
		})
	}
	return added
}

func looksLikeTextDict(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	lines := strings.Split(string(buf[:n]), "\n")
	tabs := 0
	for _, l := range lines {
		if strings.Contains(l, "\t") {
			tabs++
		}
	}
	return tabs >= 1 && tabs >= len(lines)/2
}

// dictFilter is the file dialog filter for dictionary files.
func dictFilter() string {
	exts := dict.SupportedExtensions()
	var pats []string
	for _, e := range exts {
		pats = append(pats, "*"+e)
	}
	all := strings.Join(pats, ";")
	return fmt.Sprintf("Dictionary files (%s)|%s|Lingoes (*.ld2;*.ldf)|*.ld2;*.ldf|MDict (*.mdx)|*.mdx|StarDict (*.ifo)|*.ifo|Text (*.txt;*.tsv)|*.txt;*.tsv|All files (*.*)|*.*", all, all)
}
