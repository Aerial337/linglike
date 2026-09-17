//go:build windows

package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/aerial337/linglike/internal/app"
	"github.com/aerial337/linglike/internal/dict"
	"github.com/aerial337/linglike/internal/translate"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

// ---- Dictionaries dialog --------------------------------------------------

type dictRow struct {
	cfg     app.DictConfig
	entries string
	name    string
}

type dictTable struct {
	walk.TableModelBase
	rows []*dictRow
}

func (m *dictTable) RowCount() int { return len(m.rows) }
func (m *dictTable) Value(row, col int) interface{} {
	r := m.rows[row]
	switch col {
	case 0:
		return r.name
	case 1:
		return r.entries
	case 2:
		return r.cfg.Path
	}
	return ""
}
func (m *dictTable) Checked(row int) bool { return m.rows[row].cfg.Enabled }
func (m *dictTable) SetChecked(row int, checked bool) error {
	m.rows[row].cfg.Enabled = checked
	return nil
}

func (w *mainWindow) showDictionariesDialog() {
	a := w.app
	model := &dictTable{}
	loaded := map[string]dict.Dictionary{}
	for _, d := range a.svc.Dictionaries() {
		loaded[strings.ToLower(d.Path())] = d
	}
	for _, dc := range a.cfg.Dictionaries {
		r := &dictRow{cfg: dc}
		r.name = dc.Name
		if d, ok := loaded[strings.ToLower(dc.Path)]; ok {
			r.entries = fmt.Sprintf("%d", d.Count())
			if r.name == "" {
				r.name = d.Name()
			}
		} else {
			r.entries = "–"
		}
		if r.name == "" {
			r.name = strings.TrimSuffix(filepath.Base(dc.Path), filepath.Ext(dc.Path))
		}
		model.rows = append(model.rows, r)
	}

	var dlg *walk.Dialog
	var tv *walk.TableView
	var okBtn, cancelBtn *walk.PushButton

	move := func(delta int) {
		i := tv.CurrentIndex()
		j := i + delta
		if i < 0 || j < 0 || j >= len(model.rows) {
			return
		}
		model.rows[i], model.rows[j] = model.rows[j], model.rows[i]
		model.PublishRowsReset()
		tv.SetCurrentIndex(j)
	}
	addFiles := func(paths []string) {
		known := map[string]bool{}
		for _, r := range model.rows {
			known[strings.ToLower(r.cfg.Path)] = true
		}
		for _, p := range paths {
			if known[strings.ToLower(p)] {
				continue
			}
			known[strings.ToLower(p)] = true
			model.rows = append(model.rows, &dictRow{
				cfg:     app.DictConfig{Path: p, Enabled: true},
				entries: "(load on OK)",
				name:    strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)),
			})
		}
		model.PublishRowsReset()
	}

	err := Dialog{
		AssignTo:      &dlg,
		Title:         "Dictionaries",
		MinSize:       Size{Width: 640, Height: 400},
		Layout:        VBox{},
		DefaultButton: &okBtn,
		CancelButton:  &cancelBtn,
		Children: []Widget{
			Label{Text: "Dictionaries are searched in the order listed. Untick a dictionary to disable it without removing it."},
			TableView{
				AssignTo:   &tv,
				CheckBoxes: true,
				Columns: []TableViewColumn{
					{Title: "Dictionary", Width: 260},
					{Title: "Entries", Width: 80},
					{Title: "File", Width: 400},
				},
				Model: model,
			},
			Composite{
				Layout: HBox{MarginsZero: true},
				Children: []Widget{
					PushButton{Text: "Add files...", OnClicked: func() {
						fd := &walk.FileDialog{Title: "Add dictionaries", Filter: dictFilter()}
						if ok, _ := fd.ShowOpenMultiple(dlg); ok {
							addFiles(fd.FilePaths)
						}
					}},
					PushButton{Text: "Add folder...", OnClicked: func() {
						fd := &walk.FileDialog{Title: "Choose a folder containing dictionaries"}
						if ok, _ := fd.ShowBrowseFolder(dlg); ok && fd.FilePath != "" {
							tmp := &app.Config{}
							AddDictionariesFromDirs(tmp, []string{fd.FilePath})
							var paths []string
							for _, d := range tmp.Dictionaries {
								paths = append(paths, d.Path)
							}
							addFiles(paths)
						}
					}},
					PushButton{Text: "Remove", OnClicked: func() {
						i := tv.CurrentIndex()
						if i < 0 || i >= len(model.rows) {
							return
						}
						model.rows = append(model.rows[:i], model.rows[i+1:]...)
						model.PublishRowsReset()
					}},
					PushButton{Text: "Rename...", OnClicked: func() {
						i := tv.CurrentIndex()
						if i < 0 || i >= len(model.rows) {
							return
						}
						if name, ok := promptText(dlg, "Rename dictionary", "Display name:", model.rows[i].name); ok {
							model.rows[i].name = name
							model.rows[i].cfg.Name = name
							model.PublishRowsReset()
						}
					}},
					PushButton{Text: "Move up", OnClicked: func() { move(-1) }},
					PushButton{Text: "Move down", OnClicked: func() { move(1) }},
					HSpacer{},
					PushButton{AssignTo: &okBtn, Text: "OK", OnClicked: func() { dlg.Accept() }},
					PushButton{AssignTo: &cancelBtn, Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
				},
			},
		},
	}.Create(w.mw)
	if err != nil {
		walk.MsgBox(w.mw, "Linglike", err.Error(), walk.MsgBoxIconError)
		return
	}
	if dlg.Run() != walk.DlgCmdOK {
		return
	}
	a.cfg.Dictionaries = nil
	for _, r := range model.rows {
		a.cfg.Dictionaries = append(a.cfg.Dictionaries, r.cfg)
	}
	if err := a.cfg.Save(); err != nil {
		walk.MsgBox(w.mw, "Linglike", "Cannot save settings to "+app.Path()+":\n"+err.Error(), walk.MsgBoxIconError)
	}
	w.setStatus("Loading dictionaries…")
	a.svc.Reload()
	w.showLoadErrors()
	w.showWelcome()
	if w.current != "" {
		w.doLookup(w.current)
	}
}

// promptText shows a small dialog asking for one line of text.
func promptText(owner walk.Form, title, label, initial string) (string, bool) {
	var dlg *walk.Dialog
	var le *walk.LineEdit
	var ok, cancel *walk.PushButton
	var result string
	err := Dialog{
		AssignTo:      &dlg,
		Title:         title,
		MinSize:       Size{Width: 360, Height: 120},
		Layout:        VBox{},
		DefaultButton: &ok,
		CancelButton:  &cancel,
		Children: []Widget{
			Label{Text: label},
			LineEdit{AssignTo: &le, Text: initial},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				HSpacer{},
				PushButton{AssignTo: &ok, Text: "OK", OnClicked: func() {
					result = strings.TrimSpace(le.Text()) // read before the dialog is destroyed
					dlg.Accept()
				}},
				PushButton{AssignTo: &cancel, Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
			}},
		},
	}.Create(owner)
	if err != nil {
		return "", false
	}
	if dlg.Run() != walk.DlgCmdOK {
		return "", false
	}
	return result, true
}

// ---- Settings dialog ------------------------------------------------------

func langModel(withAuto bool) ([]string, []string) {
	var names, codes []string
	if withAuto {
		names = append(names, "Detect automatically")
		codes = append(codes, "auto")
	}
	for _, l := range translate.Languages() {
		names = append(names, l.Name)
		codes = append(codes, l.Code)
	}
	return names, codes
}

func indexOf(list []string, v string) int {
	for i, s := range list {
		if strings.EqualFold(s, v) {
			return i
		}
	}
	return 0
}

func (w *mainWindow) showSettingsDialog() {
	a := w.app
	cfg := a.cfg
	var dlg *walk.Dialog
	var okBtn, cancelBtn *walk.PushButton
	var hotkeyOn, ctrl, alt, shift, winKey, ctrlRight, clipWatch, restoreClip *walk.CheckBox
	var keyBox, targetBox, sourceBox *walk.ComboBox
	var trOn, trPopup, autoClose, toTray, startHidden *walk.CheckBox
	var popW, popH, closeSecs, maxSugg, leaveDist *walk.NumberEdit
	var selBox *walk.ComboBox
	var leaveOn *walk.CheckBox
	var llmOn, llmPopup *walk.CheckBox
	var llmURL, llmKey, llmModel, llmTarget *walk.LineEdit
	var llmPrompt *walk.TextEdit
	var llmTimeout *walk.NumberEdit
	var llmStatus *walk.Label
	selNames := []string{"Off", "Always (any mouse selection or double-click)", "Only while holding Ctrl", "Only while holding Shift", "Only while holding Alt"}
	selCodes := []string{"off", "always", "ctrl", "shift", "alt"}

	keys := KeyNames()
	targetNames, targetCodes := langModel(false)
	sourceNames, sourceCodes := langModel(true)

	llmPromptText := cfg.LLM.Prompt
	if strings.TrimSpace(llmPromptText) == "" {
		llmPromptText = translate.DefaultLLMPrompt
	}

	// readLLM collects the LLM settings from the controls.
	readLLM := func() app.LLMConfig {
		return app.LLMConfig{
			Enabled:    llmOn.Checked(),
			URL:        strings.TrimSpace(llmURL.Text()),
			APIKey:     strings.TrimSpace(llmKey.Text()),
			Model:      strings.TrimSpace(llmModel.Text()),
			Prompt:     strings.ReplaceAll(llmPrompt.Text(), "\r\n", "\n"),
			TargetLang: strings.TrimSpace(llmTarget.Text()),
			InPopup:    llmPopup.Checked(),
			TimeoutSec: int(llmTimeout.Value()),
		}
	}

	// The controls are destroyed when the dialog closes, so their values
	// must be read while it is still open: in the OK handler.
	var pending app.Config
	accept := func() {
		pending = *cfg
		pending.HotkeyEnabled = hotkeyOn.Checked()
		pending.Hotkey = app.Hotkey{Ctrl: ctrl.Checked(), Alt: alt.Checked(), Shift: shift.Checked(), Win: winKey.Checked(), Key: keys[max(0, keyBox.CurrentIndex())]}
		pending.CtrlRightClick = ctrlRight.Checked()
		pending.ClipboardWatch = clipWatch.Checked()
		pending.RestoreClipboard = restoreClip.Checked()
		pending.PopupWidth = int(popW.Value())
		pending.PopupHeight = int(popH.Value())
		pending.PopupAutoClose = autoClose.Checked()
		pending.PopupCloseSeconds = int(closeSecs.Value())
		pending.TranslateEnabled = trOn.Checked()
		pending.TranslateInPopup = trPopup.Checked()
		pending.SourceLang = sourceCodes[max(0, sourceBox.CurrentIndex())]
		pending.TargetLang = targetCodes[max(0, targetBox.CurrentIndex())]
		pending.MinimizeToTray = toTray.Checked()
		pending.StartHidden = startHidden.Checked()
		pending.MaxSuggestions = int(maxSugg.Value())
		pending.SelectionPopup = selCodes[max(0, selBox.CurrentIndex())]
		pending.CloseOnMouseLeave = leaveOn.Checked()
		pending.MouseLeaveDistance = int(leaveDist.Value())
		pending.LLM = readLLM()
		if strings.TrimSpace(pending.LLM.Prompt) == translate.DefaultLLMPrompt {
			pending.LLM.Prompt = "" // keep following the built-in default
		}
		dlg.Accept()
	}

	testSeq := 0
	testLLM := func() {
		c := readLLM()
		l := &translate.LLM{URL: c.URL, APIKey: c.APIKey, Model: c.Model, Prompt: c.Prompt, Timeout: time.Duration(c.TimeoutSec) * time.Second}
		target := c.TargetLang
		if target == "" {
			target = targetCodes[max(0, targetBox.CurrentIndex())]
		}
		testSeq++
		mine := testSeq
		llmStatus.SetText("Testing " + l.Endpoint() + " …")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(c.TimeoutSec)*time.Second)
			defer cancel()
			res, err := l.Translate(ctx, "Good morning, how are you?", "auto", target)
			dlg.Synchronize(func() {
				if mine != testSeq {
					return
				}
				if err != nil {
					llmStatus.SetText("Error: " + err.Error())
					return
				}
				out := res.Text
				if len(out) > 120 {
					out = out[:120] + "…"
				}
				llmStatus.SetText("OK: " + strings.ReplaceAll(out, "\n", " "))
			})
		}()
	}

	err := Dialog{
		AssignTo:      &dlg,
		Title:         "Configuration",
		MinSize:       Size{Width: 600, Height: 520},
		Layout:        VBox{},
		DefaultButton: &okBtn,
		CancelButton:  &cancelBtn,
		Children: []Widget{
			TabWidget{
				Pages: []TabPage{
					{
						Title:  "Capture",
						Layout: VBox{},
						Children: []Widget{
							GroupBox{
								Title:  "Capture selected text",
								Layout: Grid{Columns: 6},
								Children: []Widget{
									Label{Text: "Selecting text with the mouse opens the popup:", ColumnSpan: 3},
									ComboBox{AssignTo: &selBox, Model: selNames, CurrentIndex: indexOf(selCodes, cfg.SelectionPopup), ColumnSpan: 3},
									CheckBox{AssignTo: &hotkeyOn, Text: "Hotkey:", Checked: cfg.HotkeyEnabled, ColumnSpan: 1},
									CheckBox{AssignTo: &ctrl, Text: "Ctrl", Checked: cfg.Hotkey.Ctrl},
									CheckBox{AssignTo: &alt, Text: "Alt", Checked: cfg.Hotkey.Alt},
									CheckBox{AssignTo: &shift, Text: "Shift", Checked: cfg.Hotkey.Shift},
									CheckBox{AssignTo: &winKey, Text: "Win", Checked: cfg.Hotkey.Win},
									ComboBox{AssignTo: &keyBox, Model: keys, CurrentIndex: indexOf(keys, cfg.Hotkey.Key)},
									CheckBox{AssignTo: &ctrlRight, Text: "Ctrl + right mouse click looks up the selection", Checked: cfg.CtrlRightClick, ColumnSpan: 6},
									CheckBox{AssignTo: &clipWatch, Text: "Watch the clipboard: show a popup whenever text is copied", Checked: cfg.ClipboardWatch, ColumnSpan: 6},
									CheckBox{AssignTo: &restoreClip, Text: "Restore the previous clipboard contents after capturing", Checked: cfg.RestoreClipboard, ColumnSpan: 6},
								},
							},
							GroupBox{
								Title:  "Popup window",
								Layout: Grid{Columns: 4},
								Children: []Widget{
									Label{Text: "Width:"},
									NumberEdit{AssignTo: &popW, Value: float64(cfg.PopupWidth), MinValue: 200, MaxValue: 1600, Decimals: 0},
									Label{Text: "Height:"},
									NumberEdit{AssignTo: &popH, Value: float64(cfg.PopupHeight), MinValue: 120, MaxValue: 1200, Decimals: 0},
									CheckBox{AssignTo: &leaveOn, Text: "Close when the mouse moves away more than", Checked: cfg.CloseOnMouseLeave, ColumnSpan: 1},
									NumberEdit{AssignTo: &leaveDist, Value: float64(cfg.MouseLeaveDistance), MinValue: 10, MaxValue: 1000, Decimals: 0},
									Label{Text: "pixels from the popup", ColumnSpan: 2},
									CheckBox{AssignTo: &autoClose, Text: "Close automatically after", Checked: cfg.PopupAutoClose, ColumnSpan: 1},
									NumberEdit{AssignTo: &closeSecs, Value: float64(cfg.PopupCloseSeconds), MinValue: 2, MaxValue: 120, Decimals: 0},
									Label{Text: "seconds (when the mouse is not over it)", ColumnSpan: 2},
								},
							},
							VSpacer{},
						},
					},
					{
						Title:  "Translation",
						Layout: VBox{},
						Children: []Widget{
							GroupBox{
								Title:  "Google Translate",
								Layout: Grid{Columns: 4},
								Children: []Widget{
									CheckBox{AssignTo: &trOn, Text: "Enable Google Translate", Checked: cfg.TranslateEnabled, ColumnSpan: 2},
									CheckBox{AssignTo: &trPopup, Text: "Also translate in the popup", Checked: cfg.TranslateInPopup, ColumnSpan: 2},
									Label{Text: "Translate from:"},
									ComboBox{AssignTo: &sourceBox, Model: sourceNames, CurrentIndex: indexOf(sourceCodes, cfg.SourceLang)},
									Label{Text: "to:"},
									ComboBox{AssignTo: &targetBox, Model: targetNames, CurrentIndex: indexOf(targetCodes, cfg.TargetLang)},
								},
							},
							GroupBox{
								Title:  "Local LLM (OpenAI-compatible API: Ollama, LM Studio, llama.cpp, vLLM, …)",
								Layout: Grid{Columns: 4},
								Children: []Widget{
									CheckBox{AssignTo: &llmOn, Text: "Enable LLM translation", Checked: cfg.LLM.Enabled, ColumnSpan: 2},
									CheckBox{AssignTo: &llmPopup, Text: "Also translate in the popup", Checked: cfg.LLM.InPopup, ColumnSpan: 2},
									Label{Text: "Server URL:"},
									LineEdit{AssignTo: &llmURL, Text: cfg.LLM.URL, ColumnSpan: 3, ToolTipText: "e.g. http://localhost:11434 (Ollama), http://localhost:1234/v1 (LM Studio) or a full .../v1/chat/completions URL"},
									Label{Text: "API key:"},
									LineEdit{AssignTo: &llmKey, Text: cfg.LLM.APIKey, PasswordMode: true, ColumnSpan: 3, ToolTipText: "Optional. Sent as Authorization: Bearer <key>"},
									Label{Text: "Model:"},
									LineEdit{AssignTo: &llmModel, Text: cfg.LLM.Model, ToolTipText: "Model name as known by the server, e.g. llama3.1, qwen2.5:7b, gemma2"},
									Label{Text: "Timeout (s):"},
									NumberEdit{AssignTo: &llmTimeout, Value: float64(cfg.LLM.TimeoutSec), MinValue: 5, MaxValue: 600, Decimals: 0},
									Label{Text: "Target language:"},
									LineEdit{AssignTo: &llmTarget, Text: cfg.LLM.TargetLang, ColumnSpan: 3, ToolTipText: "Language name used in the prompt, e.g. Persian. Leave empty to use the Google target language above."},
									Label{Text: "Prompt ({text}, {target}, {source} are replaced):", ColumnSpan: 4},
									TextEdit{AssignTo: &llmPrompt, Text: strings.ReplaceAll(llmPromptText, "\n", "\r\n"), VScroll: true, MinSize: Size{Height: 70}, ColumnSpan: 4},
									PushButton{Text: "Reset prompt", OnClicked: func() {
										llmPrompt.SetText(strings.ReplaceAll(translate.DefaultLLMPrompt, "\n", "\r\n"))
									}},
									PushButton{Text: "Test connection", OnClicked: testLLM},
									Label{AssignTo: &llmStatus, Text: "", ColumnSpan: 2},
								},
							},
						},
					},
					{
						Title:  "General",
						Layout: VBox{},
						Children: []Widget{
							GroupBox{
								Title:  "General",
								Layout: Grid{Columns: 4},
								Children: []Widget{
									CheckBox{AssignTo: &toTray, Text: "Closing the window hides Linglike to the tray", Checked: cfg.MinimizeToTray, ColumnSpan: 4},
									CheckBox{AssignTo: &startHidden, Text: "Start hidden in the tray", Checked: cfg.StartHidden, ColumnSpan: 4},
									Label{Text: "Index suggestions:"},
									NumberEdit{AssignTo: &maxSugg, Value: float64(cfg.MaxSuggestions), MinValue: 5, MaxValue: 500, Decimals: 0},
									HSpacer{ColumnSpan: 2},
									Label{Text: "Settings file: " + app.Path(), ColumnSpan: 4, ToolTipText: app.Path()},
								},
							},
							VSpacer{},
						},
					},
				},
			},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				HSpacer{},
				PushButton{AssignTo: &okBtn, Text: "OK", OnClicked: accept},
				PushButton{AssignTo: &cancelBtn, Text: "Cancel", OnClicked: func() { dlg.Cancel() }},
			}},
		},
	}.Create(w.mw)
	if err != nil {
		walk.MsgBox(w.mw, "Linglike", err.Error(), walk.MsgBoxIconError)
		return
	}
	// Set the initial state explicitly as well, in case the declarative
	// property initialisation did not apply.
	hotkeyOn.SetChecked(cfg.HotkeyEnabled)
	ctrl.SetChecked(cfg.Hotkey.Ctrl)
	alt.SetChecked(cfg.Hotkey.Alt)
	shift.SetChecked(cfg.Hotkey.Shift)
	winKey.SetChecked(cfg.Hotkey.Win)
	keyBox.SetCurrentIndex(indexOf(keys, cfg.Hotkey.Key))
	ctrlRight.SetChecked(cfg.CtrlRightClick)
	clipWatch.SetChecked(cfg.ClipboardWatch)
	restoreClip.SetChecked(cfg.RestoreClipboard)
	popW.SetValue(float64(cfg.PopupWidth))
	popH.SetValue(float64(cfg.PopupHeight))
	autoClose.SetChecked(cfg.PopupAutoClose)
	closeSecs.SetValue(float64(cfg.PopupCloseSeconds))
	trOn.SetChecked(cfg.TranslateEnabled)
	trPopup.SetChecked(cfg.TranslateInPopup)
	sourceBox.SetCurrentIndex(indexOf(sourceCodes, cfg.SourceLang))
	targetBox.SetCurrentIndex(indexOf(targetCodes, cfg.TargetLang))
	toTray.SetChecked(cfg.MinimizeToTray)
	startHidden.SetChecked(cfg.StartHidden)
	maxSugg.SetValue(float64(cfg.MaxSuggestions))
	selBox.SetCurrentIndex(indexOf(selCodes, cfg.SelectionPopup))
	leaveOn.SetChecked(cfg.CloseOnMouseLeave)
	leaveDist.SetValue(float64(cfg.MouseLeaveDistance))
	llmOn.SetChecked(cfg.LLM.Enabled)
	llmPopup.SetChecked(cfg.LLM.InPopup)
	llmURL.SetText(cfg.LLM.URL)
	llmKey.SetText(cfg.LLM.APIKey)
	llmModel.SetText(cfg.LLM.Model)
	llmTarget.SetText(cfg.LLM.TargetLang)
	llmTimeout.SetValue(float64(cfg.LLM.TimeoutSec))
	llmPrompt.SetText(strings.ReplaceAll(llmPromptText, "\n", "\r\n"))

	if dlg.Run() != walk.DlgCmdOK {
		return
	}
	*cfg = pending
	if err := cfg.Save(); err != nil {
		walk.MsgBox(w.mw, "Linglike", "Cannot save settings to "+app.Path()+":\n"+err.Error(), walk.MsgBoxIconError)
		return
	}
	a.applyCaptureSettings()
	a.popup.mw.SetSize(walk.Size{Width: cfg.PopupWidth, Height: cfg.PopupHeight})
	w.syncLang()
	a.popup.syncLang()
	if w.current == "" {
		w.showWelcome() // refresh the hotkey shown on the welcome page
	} else {
		w.doLookup(w.current)
	}
	w.setStatus("Settings saved to " + app.Path() + "  ·  hotkey " + cfg.Hotkey.String() + "  ·  translate to " + translate.LanguageName(cfg.TargetLang))
}

// ---- Text translation dialog ---------------------------------------------

func (w *mainWindow) showTranslateDialog(initial string) {
	a := w.app
	var dlg *walk.Dialog
	var src, dst *walk.TextEdit
	var sourceBox, targetBox, engineBox *walk.ComboBox
	var closeBtn, goBtn *walk.PushButton
	var status *walk.Label
	targetNames, targetCodes := langModel(false)
	sourceNames, sourceCodes := langModel(true)
	engines := []string{"Google Translate", "Local LLM"}
	engineIdx := 0
	if a.cfg.LLM.Enabled && !a.cfg.TranslateEnabled {
		engineIdx = 1
	}
	seq := 0

	doTranslate := func() {
		text := strings.TrimSpace(src.Text())
		if text == "" {
			return
		}
		seq++
		mine := seq
		from := sourceCodes[max(0, sourceBox.CurrentIndex())]
		to := targetCodes[max(0, targetBox.CurrentIndex())]
		useLLM := engineBox.CurrentIndex() == 1
		status.SetText("Translating…")
		go func() {
			timeout := 30 * time.Second
			if useLLM {
				timeout = time.Duration(a.cfg.LLM.TimeoutSec) * time.Second
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			var res *translate.Result
			var err error
			if useLLM {
				llmTo := to
				if t := strings.TrimSpace(a.cfg.LLM.TargetLang); t != "" && to == a.cfg.TargetLang {
					llmTo = t
				}
				res, err = a.svc.LLMClient().Translate(ctx, text, from, llmTo)
			} else {
				res, err = translate.DefaultClient.Translate(ctx, text, from, to)
			}
			dlg.Synchronize(func() {
				if mine != seq {
					return
				}
				if err != nil {
					status.SetText("Error: " + err.Error())
					return
				}
				dst.SetText(strings.ReplaceAll(res.Text, "\n", "\r\n"))
				st := "Translated"
				if res.SourceLang != "" {
					st += " from " + translate.LanguageName(res.SourceLang)
				}
				if len(res.Alternatives) > 0 {
					var alts []string
					for _, alt := range res.Alternatives {
						alts = append(alts, alt.PartOfSpeech+": "+strings.Join(alt.Terms, ", "))
					}
					dst.AppendText("\r\n\r\n" + strings.Join(alts, "\r\n"))
				}
				status.SetText(st)
			})
		}()
	}

	err := Dialog{
		AssignTo:     &dlg,
		Title:        "Text Translation",
		MinSize:      Size{Width: 560, Height: 440},
		Layout:       VBox{},
		CancelButton: &closeBtn,
		Children: []Widget{
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				Label{Text: "From:"},
				ComboBox{AssignTo: &sourceBox, Model: sourceNames, CurrentIndex: indexOf(sourceCodes, a.cfg.SourceLang)},
				Label{Text: "To:"},
				ComboBox{AssignTo: &targetBox, Model: targetNames, CurrentIndex: indexOf(targetCodes, a.cfg.TargetLang)},
				PushButton{Text: "Swap", OnClicked: func() {
					t := targetCodes[max(0, targetBox.CurrentIndex())]
					s := sourceCodes[max(0, sourceBox.CurrentIndex())]
					if s != "auto" {
						targetBox.SetCurrentIndex(indexOf(targetCodes, s))
					}
					sourceBox.SetCurrentIndex(indexOf(sourceCodes, t))
					a, b := src.Text(), dst.Text()
					src.SetText(b)
					dst.SetText(a)
				}},
				HSpacer{},
				Label{Text: "Engine:"},
				ComboBox{AssignTo: &engineBox, Model: engines, CurrentIndex: engineIdx},
				PushButton{AssignTo: &goBtn, Text: "Translate (Ctrl+Enter)", OnClicked: doTranslate},
			}},
			TextEdit{AssignTo: &src, VScroll: true, Text: initial, OnKeyDown: func(key walk.Key) {
				if key == walk.KeyReturn && walk.ModifiersDown()&walk.ModControl != 0 {
					doTranslate()
				}
			}},
			TextEdit{AssignTo: &dst, VScroll: true, ReadOnly: true},
			Composite{Layout: HBox{MarginsZero: true}, Children: []Widget{
				Label{AssignTo: &status, Text: ""},
				HSpacer{},
				PushButton{Text: "Copy result", OnClicked: func() { walk.Clipboard().SetText(dst.Text()) }},
				PushButton{AssignTo: &closeBtn, Text: "Close", OnClicked: func() { dlg.Cancel() }},
			}},
		},
	}.Create(w.mw)
	if err != nil {
		walk.MsgBox(w.mw, "Linglike", err.Error(), walk.MsgBoxIconError)
		return
	}
	if strings.TrimSpace(initial) != "" {
		doTranslate()
	}
	dlg.Run()
}
