//go:build windows

package ui

import (
	"strings"
	"time"

	"github.com/aerial337/linglike/internal/app"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
)

// popupWindow is the small always-on-top window shown near the mouse
// cursor with the definition of captured text.
type popupWindow struct {
	app    *App
	mw     *walk.MainWindow
	title  *walk.Label
	pinBtn *walk.PushButton
	web    *walk.WebView

	text    string
	pinned  bool
	showSeq int
}

func newPopupWindow(a *App) (*popupWindow, error) {
	p := &popupWindow{app: a}
	err := MainWindow{
		AssignTo: &p.mw,
		Title:    "Linglike",
		Layout:   VBox{MarginsZero: true, SpacingZero: true},
		Size:     Size{Width: a.cfg.PopupWidth, Height: a.cfg.PopupHeight},
		Children: []Widget{
			Composite{
				Layout: HBox{Margins: Margins{Left: 6, Top: 2, Right: 2, Bottom: 2}, Spacing: 2},
				Children: []Widget{
					Label{AssignTo: &p.title, Text: "", Font: Font{Family: "Segoe UI", PointSize: 9, Bold: true}},
					HSpacer{},
					PushButton{AssignTo: &p.pinBtn, Text: "Pin", MaxSize: Size{Width: 40}, OnClicked: p.togglePin},
					PushButton{Text: "Open", MaxSize: Size{Width: 46}, OnClicked: p.openInMain},
					PushButton{Text: "X", MaxSize: Size{Width: 26}, OnClicked: p.hide},
				},
			},
			WebView{AssignTo: &p.web, OnNavigating: p.navigating},
		},
	}.Create()
	if err != nil {
		return nil, err
	}
	p.mw.StatusBar().SetVisible(false)
	// Turn the frame into a borderless tool window that stays on top and
	// does not appear in the taskbar.
	hwnd := p.mw.Handle()
	style := uint32(win.GetWindowLong(hwnd, win.GWL_STYLE))
	style &^= uint32(win.WS_CAPTION | win.WS_THICKFRAME | win.WS_MINIMIZEBOX | win.WS_MAXIMIZEBOX | win.WS_SYSMENU)
	style |= uint32(win.WS_POPUP) | uint32(win.WS_BORDER)
	win.SetWindowLong(hwnd, win.GWL_STYLE, int32(style))
	ex := uint32(win.GetWindowLong(hwnd, win.GWL_EXSTYLE))
	ex |= uint32(win.WS_EX_TOOLWINDOW | win.WS_EX_TOPMOST)
	ex &^= uint32(win.WS_EX_APPWINDOW)
	win.SetWindowLong(hwnd, win.GWL_EXSTYLE, int32(ex))
	win.SetWindowPos(hwnd, win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE|win.SWP_FRAMECHANGED)
	p.mw.KeyDown().Attach(func(key walk.Key) {
		if key == walk.KeyEscape {
			p.hide()
		}
	})
	p.mw.Closing().Attach(func(canceled *bool, reason walk.CloseReason) {
		*canceled = true
		p.hide()
	})
	return p, nil
}

func (p *popupWindow) togglePin() {
	p.pinned = !p.pinned
	if p.pinned {
		p.pinBtn.SetText("Unpin")
	} else {
		p.pinBtn.SetText("Pin")
		p.scheduleAutoClose()
	}
}

func (p *popupWindow) openInMain() {
	text := p.text
	p.hide()
	p.app.showMain()
	p.app.main.lookup(text)
}

func (p *popupWindow) hide() {
	p.showSeq++
	p.pinned = false
	p.pinBtn.SetText("Pin")
	p.mw.Hide()
}

func (p *popupWindow) clickedOutside() {
	if !p.pinned && p.mw.Visible() {
		p.hide()
	}
}

func (p *popupWindow) navigating(e *walk.WebViewNavigatingEventData) {
	if word, ok := entryFromURL(e.Url()); ok {
		e.SetCanceled(true)
		p.show(word)
	}
}

// showAt positions the popup near a screen point and shows the lookup.
func (p *popupWindow) showAt(pt win.POINT, text string) {
	dpi := p.mw.DPI()
	scale := func(v int) int32 { return int32(v * dpi / 96) }
	w, h := scale(p.app.cfg.PopupWidth), scale(p.app.cfg.PopupHeight)
	work := monitorWorkArea(pt)
	x, y := pt.X+scale(12), pt.Y+scale(18)
	if x+w > work.Right {
		x = work.Right - w
	}
	if y+h > work.Bottom {
		y = pt.Y - h - scale(10)
		if y < work.Top {
			y = work.Bottom - h
		}
	}
	if x < work.Left {
		x = work.Left
	}
	if y < work.Top {
		y = work.Top
	}
	p.mw.SetBoundsPixels(walk.Rectangle{X: int(x), Y: int(y), Width: int(w), Height: int(h)})
	p.show(text)
}

// show displays the popup (at its current position) with the lookup of text.
func (p *popupWindow) show(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	p.text = text
	p.showSeq++
	seq := p.showSeq
	title := text
	if len(title) > 60 {
		title = title[:57] + "…"
	}
	p.title.SetText(title)
	hwnd := p.mw.Handle()
	if !p.mw.Visible() {
		win.ShowWindow(hwnd, win.SW_SHOWNOACTIVATE)
	}
	win.SetWindowPos(hwnd, win.HWND_TOPMOST, 0, 0, 0, 0, win.SWP_NOMOVE|win.SWP_NOSIZE|win.SWP_NOACTIVATE)
	p.app.runQuery(text, true, func(q *app.Query, done bool) {
		if seq != p.showSeq {
			return
		}
		page := p.app.svc.Page(q, true)
		if u, err := p.app.host.write("popup", page); err == nil {
			p.web.SetURL(u)
		}
	})
	p.scheduleAutoClose()
}

// scheduleAutoClose hides the popup after the configured delay unless it
// is pinned or the mouse is over it.
func (p *popupWindow) scheduleAutoClose() {
	if !p.app.cfg.PopupAutoClose {
		return
	}
	secs := p.app.cfg.PopupCloseSeconds
	if secs < 2 {
		secs = 2
	}
	seq := p.showSeq
	var tick func()
	tick = func() {
		if seq != p.showSeq || !p.mw.Visible() || p.pinned {
			return
		}
		var pt win.POINT
		win.GetCursorPos(&pt)
		var rc win.RECT
		win.GetWindowRect(p.mw.Handle(), &rc)
		over := pt.X >= rc.Left && pt.X <= rc.Right && pt.Y >= rc.Top && pt.Y <= rc.Bottom
		fg := win.GetForegroundWindow() == p.mw.Handle()
		if over || fg {
			time.AfterFunc(2*time.Second, func() { p.mw.Synchronize(tick) })
			return
		}
		p.hide()
	}
	time.AfterFunc(time.Duration(secs)*time.Second, func() { p.mw.Synchronize(tick) })
}
