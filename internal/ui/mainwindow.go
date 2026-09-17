//go:build windows

package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/aerial337/linglike/internal/app"
	"github.com/aerial337/linglike/internal/dict"
	"github.com/aerial337/linglike/internal/render"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
)

// treeItem is a node of the left hand navigation tree.
type treeItem struct {
	text     string
	parent   *treeItem
	children []*treeItem
	action   func()
	section  int // index of the result section to scroll to, -1 if none
}

func (t *treeItem) Text() string { return t.text }
func (t *treeItem) Parent() walk.TreeItem {
	if t.parent == nil {
		return nil
	}
	return t.parent
}
func (t *treeItem) ChildCount() int             { return len(t.children) }
func (t *treeItem) ChildAt(i int) walk.TreeItem { return t.children[i] }

type treeModel struct {
	walk.TreeModelBase
	roots []*treeItem
}

func (m *treeModel) RootCount() int             { return len(m.roots) }
func (m *treeModel) RootAt(i int) walk.TreeItem { return m.roots[i] }

// mainWindow is the Lingoes-style main window: search bar on top, results
// and options tree on the left, result page on the right.
type mainWindow struct {
	app    *App
	mw     *walk.MainWindow
	search *walk.LineEdit
	tree   *walk.TreeView
	index  *walk.ListBox
	web    *walk.WebView
	status *walk.StatusBarItem
	lang   *walk.ComboBox

	model    *treeModel
	results  *treeItem
	options  *treeItem
	pageURL  string
	current  string
	indexSeq int
	suppress bool
}

func newMainWindow(a *App) (*mainWindow, error) {
	w := &mainWindow{app: a}
	w.results = &treeItem{text: "Results", section: -1}
	w.options = &treeItem{text: "Options", section: -1}
	opt := func(text string, f func()) {
		w.options.children = append(w.options.children, &treeItem{text: text, parent: w.options, action: f, section: -1})
	}
	opt("Dictionaries...", w.showDictionariesDialog)
	opt("Configuration...", w.showSettingsDialog)
	opt("Text Translation", func() { w.showTranslateDialog(w.search.Text()) })
	opt("About Linglike", w.showAbout)
	w.model = &treeModel{roots: []*treeItem{w.results, w.options}}

	var icon Property
	if a.icon != nil {
		icon = a.icon
	}
	err := MainWindow{
		AssignTo: &w.mw,
		Title:    "Linglike",
		Icon:     icon,
		MinSize:  Size{Width: 520, Height: 360},
		Size:     Size{Width: a.cfg.WindowWidth, Height: a.cfg.WindowHeight},
		Layout:   VBox{Margins: Margins{Left: 4, Top: 4, Right: 4, Bottom: 2}, Spacing: 4},
		MenuItems: []MenuItem{
			Menu{Text: "&File", Items: []MenuItem{
				Action{Text: "&Dictionaries...", OnTriggered: w.showDictionariesDialog},
				Action{Text: "&Configuration...", OnTriggered: w.showSettingsDialog},
				Separator{},
				Action{Text: "&Hide to tray", OnTriggered: func() { w.mw.Hide() }},
				Action{Text: "E&xit", OnTriggered: a.exit},
			}},
			Menu{Text: "&Tools", Items: []MenuItem{
				Action{Text: "&Text Translation...", OnTriggered: func() { w.showTranslateDialog(w.search.Text()) }},
				Action{Text: "Look up &clipboard text", OnTriggered: func() {
					if t, err := walk.Clipboard().Text(); err == nil && strings.TrimSpace(t) != "" {
						w.lookup(t)
					}
				}},
				Action{Text: "&Reload dictionaries", OnTriggered: func() {
					a.svc.Reload()
					w.showLoadErrors()
					w.setStatus(w.dictSummary())
				}},
			}},
			Menu{Text: "&Help", Items: []MenuItem{
				Action{Text: "&About Linglike", OnTriggered: w.showAbout},
			}},
		},
		Children: []Widget{
			Composite{
				Layout: HBox{MarginsZero: true, Spacing: 4},
				Children: []Widget{
					LineEdit{
						AssignTo:      &w.search,
						CueBanner:     "Type a word or paste text and press Enter",
						OnKeyDown:     w.searchKeyDown,
						OnTextChanged: w.searchTextChanged,
					},
					PushButton{Text: "Search", OnClicked: func() { w.lookup(w.search.Text()) }},
					Label{Text: "Translate to:"},
					ComboBox{
						AssignTo:              &w.lang,
						Model:                 a.langNames,
						CurrentIndex:          a.targetIndex(),
						MaxSize:               Size{Width: 150},
						ToolTipText:           "Target language for Google Translate",
						OnCurrentIndexChanged: w.langChanged,
					},
					PushButton{Text: "Text...", ToolTipText: "Text Translation window", OnClicked: func() { w.showTranslateDialog(w.search.Text()) }},
				},
			},
			HSplitter{
				Children: []Widget{
					Composite{
						Layout:        VBox{MarginsZero: true, Spacing: 2},
						StretchFactor: 1,
						MinSize:       Size{Width: 150},
						Children: []Widget{
							TreeView{
								AssignTo:             &w.tree,
								Model:                w.model,
								OnCurrentItemChanged: w.treeItemChanged,
								OnItemActivated:      w.treeItemChanged,
							},
							Label{Text: "Index", Font: Font{Family: "Segoe UI", PointSize: 9, Bold: true}},
							ListBox{
								AssignTo:        &w.index,
								Model:           []string{},
								OnItemActivated: w.indexActivated,
								OnCurrentIndexChanged: func() {
									if w.suppress {
										return
									}
									w.indexActivated()
								},
							},
						},
					},
					WebView{
						AssignTo:      &w.web,
						StretchFactor: 4,
						OnNavigating:  w.navigating,
					},
				},
			},
		},
		StatusBarItems: []StatusBarItem{{AssignTo: &w.status, Text: "Ready", Width: 600}},
	}.Create()
	if err != nil {
		return nil, err
	}
	w.mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		b := w.mw.Bounds()
		a.cfg.WindowWidth, a.cfg.WindowHeight = b.Width, b.Height
		if a.cfg.MinimizeToTray && reason == walk.CloseReasonUser {
			*canceled = true
			w.mw.Hide()
			return
		}
		a.cfg.Save()
	})
	w.tree.SetExpanded(w.results, true)
	w.tree.SetExpanded(w.options, true)
	w.lang.SetCurrentIndex(a.targetIndex())
	w.showWelcome()
	return w, nil
}

// langChanged handles a new target language picked in the toolbar.
func (w *mainWindow) langChanged() {
	if w.suppress {
		return
	}
	i := w.lang.CurrentIndex()
	if i < 0 || i >= len(w.app.langCodes) {
		return
	}
	if w.app.setTargetLang(w.app.langCodes[i]) && w.current != "" {
		w.doLookup(w.current)
	}
}

// syncLang shows the configured target language in the toolbar.
func (w *mainWindow) syncLang() {
	if w.lang == nil {
		return
	}
	w.suppress = true
	w.lang.SetCurrentIndex(w.app.targetIndex())
	w.suppress = false
}

func (w *mainWindow) dictSummary() string {
	ds := w.app.svc.Dictionaries()
	total := 0
	for _, d := range ds {
		total += d.Count()
	}
	return fmt.Sprintf("%d dictionaries loaded, %d entries", len(ds), total)
}

func (w *mainWindow) setStatus(s string) {
	if w.status != nil {
		w.status.SetText(s)
	}
}

func (w *mainWindow) showWelcome() {
	p := &render.Page{Query: "Linglike"}
	ds := w.app.svc.Dictionaries()
	var sb strings.Builder
	if len(ds) == 0 {
		sb.WriteString(`<p>No dictionaries are installed yet.</p><p>Open <b>Options › Dictionaries...</b> and add Lingoes <b>.ld2</b>, MDict <b>.mdx</b>, StarDict <b>.ifo</b> or tab separated <b>.txt</b> files, or copy them into a <b>dictionaries</b> folder next to <b>linglike.exe</b> and restart.</p>`)
	} else {
		sb.WriteString("<p>Installed dictionaries:</p><ul>")
		for _, d := range ds {
			sb.WriteString("<li>" + escape(d.Name()) + fmt.Sprintf(" <span class=\"info\">(%d entries)</span></li>", d.Count()))
		}
		sb.WriteString("</ul>")
	}
	sb.WriteString(`<p class="info">Select text in any program and press <b>` + escape(w.app.cfg.Hotkey.String()) + `</b>`)
	if w.app.cfg.CtrlRightClick {
		sb.WriteString(` or <b>Ctrl + right-click</b>`)
	}
	sb.WriteString(` to look it up in a popup.</p>`)
	p.Sections = append(p.Sections, render.Section{ID: "welcome", Title: "Welcome to Linglike", Kind: "info", Body: sb.String()})
	w.navigate(p)
	w.setStatus(w.dictSummary())
}

func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

func (w *mainWindow) navigate(p *render.Page) {
	u, err := w.app.host.write("main", p)
	if err != nil {
		w.setStatus("Cannot write page: " + err.Error())
		return
	}
	w.pageURL = u
	w.web.SetURL(u)
}

func (w *mainWindow) navigating(e *walk.WebViewNavigatingEventData) {
	if word, ok := entryFromURL(e.Url()); ok {
		e.SetCanceled(true)
		w.lookup(word)
	}
}

func (w *mainWindow) searchKeyDown(key walk.Key) {
	switch key {
	case walk.KeyReturn:
		w.lookup(w.search.Text())
	case walk.KeyEscape:
		w.search.SetText("")
	case walk.KeyDown:
		if w.index.Model() != nil && w.index.CurrentIndex() < 0 {
			w.suppress = true
			w.index.SetCurrentIndex(0)
			w.suppress = false
		}
		w.index.SetFocus()
	}
}

// searchTextChanged updates the index list with prefix suggestions after a
// short delay.
func (w *mainWindow) searchTextChanged() {
	w.indexSeq++
	seq := w.indexSeq
	text := strings.TrimSpace(w.search.Text())
	time.AfterFunc(120*time.Millisecond, func() {
		w.mw.Synchronize(func() {
			if seq != w.indexSeq {
				return
			}
			w.updateIndex(text)
		})
	})
}

func (w *mainWindow) updateIndex(text string) {
	var items []string
	if text != "" && !app.IsSentence(text) {
		items = w.app.svc.Suggest(text, w.app.cfg.MaxSuggestions)
	}
	w.suppress = true
	w.index.SetModel(items)
	w.suppress = false
}

func (w *mainWindow) indexActivated() {
	i := w.index.CurrentIndex()
	items, _ := w.index.Model().([]string)
	if i < 0 || i >= len(items) {
		return
	}
	w.lookupKeepIndex(items[i])
}

func (w *mainWindow) treeItemChanged() {
	item, ok := w.tree.CurrentItem().(*treeItem)
	if !ok || item == nil {
		return
	}
	if item.action != nil {
		// Reselect the parent so that clicking the same option again
		// triggers a new selection change.
		w.tree.SetCurrentItem(w.options)
		item.action()
		return
	}
	if item.section >= 0 && w.pageURL != "" {
		w.web.SetURL(w.pageURL + "#" + render.SectionID(item.section))
	}
}

// lookup searches for text and shows the results in the main page.
func (w *mainWindow) lookup(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if w.search.Text() != text {
		w.indexSeq++ // do not let the pending suggestion update fire
		w.search.SetText(text)
		w.updateIndex(text)
	}
	w.doLookup(text)
}

func (w *mainWindow) lookupKeepIndex(text string) {
	w.indexSeq++
	w.search.SetText(text)
	w.doLookup(text)
}

func (w *mainWindow) doLookup(text string) {
	w.current = text
	w.app.cfg.AddHistory(text)
	start := time.Now()
	w.app.runQuery(text, false, func(q *app.Query, done bool) {
		if q.Text != w.current {
			return
		}
		page := w.app.svc.Page(q, false)
		w.navigate(page)
		w.updateResultsTree(q.Results, page)
		if done {
			w.setStatus(fmt.Sprintf("%d dictionaries matched \"%s\"  ·  %d ms", len(q.Results), text, time.Since(start).Milliseconds()))
		} else {
			w.setStatus(fmt.Sprintf("%d dictionaries matched \"%s\"  ·  translating…", len(q.Results), text))
		}
	})
}

func (w *mainWindow) updateResultsTree(results []dict.Result, page *render.Page) {
	w.results.children = nil
	for i, s := range page.Sections {
		if s.Kind == "dict" || s.Kind == "translate" {
			w.results.children = append(w.results.children, &treeItem{text: s.Title, parent: w.results, section: i})
		}
	}
	w.model.PublishItemsReset(w.results)
	w.tree.SetExpanded(w.results, true)
}

func (w *mainWindow) showLoadErrors() {
	errs := w.app.svc.Errors()
	if len(errs) == 0 {
		return
	}
	var sb strings.Builder
	sb.WriteString("Some dictionaries could not be loaded:\n\n")
	for _, e := range errs {
		sb.WriteString(fmt.Sprintf("%s\n    %v\n\n", e.Path, e.Err))
	}
	walk.MsgBox(w.mw, "Linglike – dictionaries", sb.String(), walk.MsgBoxIconWarning)
}

func (w *mainWindow) showAbout() {
	walk.MsgBox(w.mw, "About Linglike",
		"Linglike 1.0\n\nA Lingoes-style dictionary and translation tool.\n\n"+
			"Supported dictionaries: Lingoes LD2/LDF, MDict MDX, StarDict, tab separated text.\n"+
			"Machine translation: Google Translate (unofficial web endpoint).\n\n"+
			"Select text in any program and press "+w.app.cfg.Hotkey.String()+" (or Ctrl + right-click) to look it up.",
		walk.MsgBoxIconInformation)
}
