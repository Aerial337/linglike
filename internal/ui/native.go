//go:build windows

package ui

import (
	"errors"
	"strings"
	"syscall"
	"unsafe"

	"github.com/lxn/win"
)

var (
	user32                            = syscall.NewLazyDLL("user32.dll")
	procRegisterHotKey                = user32.NewProc("RegisterHotKey")
	procUnregisterHotKey              = user32.NewProc("UnregisterHotKey")
	procSetWindowsHookExW             = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx           = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx                = user32.NewProc("CallNextHookEx")
	procGetClipboardSequenceNumber    = user32.NewProc("GetClipboardSequenceNumber")
	procGetAsyncKeyState              = user32.NewProc("GetAsyncKeyState")
	procRemoveClipboardFormatListener = user32.NewProc("RemoveClipboardFormatListener")
	procMonitorFromPoint              = user32.NewProc("MonitorFromPoint")
	procWindowFromPoint               = user32.NewProc("WindowFromPoint")
	procGetAncestor                   = user32.NewProc("GetAncestor")
	procGetClassNameW                 = user32.NewProc("GetClassNameW")
	kernel32                          = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW                  = kernel32.NewProc("CreateMutexW")
	procGetDoubleClickTime            = user32.NewProc("GetDoubleClickTime")
)

const (
	modAlt      = 0x0001
	modControl  = 0x0002
	modShift    = 0x0004
	modWin      = 0x0008
	modNoRepeat = 0x4000

	whMouseLL = 14

	wmAppCapture = win.WM_APP + 1 // posted by the mouse hook: Ctrl+right-click
	wmAppOutside = win.WM_APP + 2 // posted by the mouse hook: click outside popup
	wmAppSelect  = win.WM_APP + 3 // posted by the mouse hook: text was selected with the mouse
	wmAppLeave   = win.WM_APP + 4 // posted by the mouse hook: mouse moved away from the popup

	hotkeyID = 1
)

// msllHookStruct mirrors MSLLHOOKSTRUCT.
type msllHookStruct struct {
	Pt          win.POINT
	MouseData   uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

// keyCodes maps key names used in the configuration to virtual key codes.
func keyCode(name string) (uint32, bool) {
	name = strings.ToUpper(strings.TrimSpace(name))
	if len(name) == 1 {
		c := name[0]
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			return uint32(c), true
		}
	}
	if strings.HasPrefix(name, "F") && len(name) <= 3 {
		n := 0
		for _, ch := range name[1:] {
			if ch < '0' || ch > '9' {
				return 0, false
			}
			n = n*10 + int(ch-'0')
		}
		if n >= 1 && n <= 24 {
			return uint32(win.VK_F1 + n - 1), true
		}
	}
	switch name {
	case "SPACE":
		return win.VK_SPACE, true
	case "INSERT", "INS":
		return win.VK_INSERT, true
	case "HOME":
		return win.VK_HOME, true
	case "END":
		return win.VK_END, true
	case "PAUSE":
		return win.VK_PAUSE, true
	case "SCROLL", "SCROLLLOCK":
		return win.VK_SCROLL, true
	case "PAGEUP":
		return win.VK_PRIOR, true
	case "PAGEDOWN":
		return win.VK_NEXT, true
	case "`", "OEM_3":
		return 0xC0, true
	}
	return 0, false
}

// KeyNames lists the key names selectable for the hotkey.
func KeyNames() []string {
	var out []string
	for c := 'A'; c <= 'Z'; c++ {
		out = append(out, string(c))
	}
	for c := '0'; c <= '9'; c++ {
		out = append(out, string(c))
	}
	for i := 1; i <= 12; i++ {
		out = append(out, "F"+itoa(i))
	}
	return append(out, "Space", "Insert", "Home", "End", "PageUp", "PageDown", "Pause", "`")
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func registerHotKey(hwnd win.HWND, id int, mods uint32, vk uint32) error {
	r, _, err := procRegisterHotKey.Call(uintptr(hwnd), uintptr(id), uintptr(mods|modNoRepeat), uintptr(vk))
	if r == 0 {
		// Older Windows versions reject MOD_NOREPEAT; retry without it.
		r, _, err = procRegisterHotKey.Call(uintptr(hwnd), uintptr(id), uintptr(mods), uintptr(vk))
		if r == 0 {
			return errors.New("RegisterHotKey failed: " + err.Error())
		}
	}
	return nil
}

func unregisterHotKey(hwnd win.HWND, id int) {
	procUnregisterHotKey.Call(uintptr(hwnd), uintptr(id))
}

func clipboardSequence() uint32 {
	r, _, _ := procGetClipboardSequenceNumber.Call()
	return uint32(r)
}

func keyDown(vk int) bool {
	r, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return uint16(r)&0x8000 != 0
}

func removeClipboardListener(hwnd win.HWND) {
	procRemoveClipboardFormatListener.Call(uintptr(hwnd))
}

func monitorWorkArea(pt win.POINT) win.RECT {
	// MonitorFromPoint takes a POINT by value (two int32 packed in one
	// argument on amd64).
	packed := uintptr(uint32(pt.X)) | uintptr(uint32(pt.Y))<<32
	h, _, _ := procMonitorFromPoint.Call(packed, win.MONITOR_DEFAULTTONEAREST)
	var mi win.MONITORINFO
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	if h != 0 && win.GetMonitorInfo(win.HMONITOR(h), &mi) {
		return mi.RcWork
	}
	return win.RECT{Left: 0, Top: 0, Right: win.GetSystemMetrics(win.SM_CXSCREEN), Bottom: win.GetSystemMetrics(win.SM_CYSCREEN)}
}

// topLevelWindowAt returns the top-level window under a screen point.
func topLevelWindowAt(pt win.POINT) win.HWND {
	packed := uintptr(uint32(pt.X)) | uintptr(uint32(pt.Y))<<32
	h, _, _ := procWindowFromPoint.Call(packed)
	if h == 0 {
		return 0
	}
	const gaRoot = 2
	r, _, _ := procGetAncestor.Call(h, gaRoot)
	if r == 0 {
		return win.HWND(h)
	}
	return win.HWND(r)
}

// windowClassAt returns the class name of the window under a screen point.
func windowClassAt(pt win.POINT) string {
	packed := uintptr(uint32(pt.X)) | uintptr(uint32(pt.Y))<<32
	h, _, _ := procWindowFromPoint.Call(packed)
	if h == 0 {
		return ""
	}
	var buf [64]uint16
	n, _, _ := procGetClassNameW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf[:n])
}

// sendCopy simulates Ctrl+C in the foreground application. Modifier keys
// the user may still be holding (from the hotkey) are released first so
// the application sees a clean Ctrl+C.
func sendCopy() {
	var inputs []win.KEYBD_INPUT
	key := func(vk uint16, up bool) {
		var ki win.KEYBD_INPUT
		ki.Type = win.INPUT_KEYBOARD
		ki.Ki.WVk = vk
		if up {
			ki.Ki.DwFlags = win.KEYEVENTF_KEYUP
		}
		inputs = append(inputs, ki)
	}
	for _, vk := range []int{win.VK_MENU, win.VK_LMENU, win.VK_RMENU, win.VK_SHIFT, win.VK_LSHIFT, win.VK_RSHIFT, win.VK_LWIN, win.VK_RWIN} {
		if keyDown(vk) {
			key(uint16(vk), true)
		}
	}
	// Release the hotkey letter too, so it does not get typed into the
	// application together with our Ctrl.
	for vk := 0x30; vk <= 0x5A; vk++ {
		if keyDown(vk) && vk != 'C' {
			key(uint16(vk), true)
		}
	}
	key(win.VK_CONTROL, false)
	key('C', false)
	key('C', true)
	key(win.VK_CONTROL, true)
	win.SendInput(uint32(len(inputs)), unsafe.Pointer(&inputs[0]), int32(unsafe.Sizeof(inputs[0])))
}

// singleInstance creates a named mutex; it reports false if another
// instance already holds it.
func singleInstance(name string) bool {
	n, _ := syscall.UTF16PtrFromString(name)
	procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(n)))
	return syscall.GetLastError() != syscall.ERROR_ALREADY_EXISTS
}

// mouseHook installs a low level mouse hook used for Ctrl+right-click
// capture and for closing the popup when clicking elsewhere.
type mouseHook struct {
	handle    uintptr
	cb        uintptr
	target    win.HWND // window that receives the posted notifications
	enabled   func() bool
	popupWnd  func() win.HWND
	ownWnds   func() []win.HWND // windows of this application (ignored for selection)
	swallowUp bool

	// selection detection: "off", "always", "ctrl", "shift", "alt"
	selectMode func() string
	downPt     win.POINT
	downTime   uint32
	prevDown   win.POINT
	prevTime   uint32
	dblClick   bool

	// mouse-leave detection for the popup; maintained by the popup window
	leaveEnabled bool
	leaveDist    int32
	popupRect    win.RECT
	popupShown   bool
	showPt       win.POINT
	wasNear      bool
}

// setPopupState is called by the popup when it is shown, moved or hidden.
func (h *mouseHook) setPopupState(shown bool, rc win.RECT, cursor win.POINT) {
	h.popupShown = shown
	h.popupRect = rc
	h.showPt = cursor
	h.wasNear = false
}

func doubleClickTime() uint32 {
	r, _, _ := procGetDoubleClickTime.Call()
	if r == 0 {
		return 500
	}
	return uint32(r)
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// distanceToRect returns the Chebyshev distance from a point to a rectangle
// (0 when inside).
func distanceToRect(pt win.POINT, rc win.RECT) int32 {
	var dx, dy int32
	if pt.X < rc.Left {
		dx = rc.Left - pt.X
	} else if pt.X > rc.Right {
		dx = pt.X - rc.Right
	}
	if pt.Y < rc.Top {
		dy = rc.Top - pt.Y
	} else if pt.Y > rc.Bottom {
		dy = pt.Y - rc.Bottom
	}
	if dx > dy {
		return dx
	}
	return dy
}

func (h *mouseHook) isOwnWindowAt(pt win.POINT) bool {
	top := topLevelWindowAt(pt)
	if top == 0 {
		return false
	}
	if h.ownWnds != nil {
		for _, w := range h.ownWnds() {
			if w == top {
				return true
			}
		}
	}
	return windowClassAt(pt) == "ComboLBox"
}

// selectionModifierOK checks the modifier required by the selection mode.
func (h *mouseHook) selectionModifierOK() bool {
	mode := "off"
	if h.selectMode != nil {
		mode = h.selectMode()
	}
	switch mode {
	case "always":
		return true
	case "ctrl":
		return keyDown(win.VK_CONTROL)
	case "shift":
		return keyDown(win.VK_SHIFT)
	case "alt":
		return keyDown(win.VK_MENU)
	}
	return false
}

func (h *mouseHook) install() error {
	h.cb = syscall.NewCallback(h.proc)
	r, _, err := procSetWindowsHookExW.Call(whMouseLL, h.cb, uintptr(win.GetModuleHandle(nil)), 0)
	if r == 0 {
		return errors.New("SetWindowsHookEx failed: " + err.Error())
	}
	h.handle = r
	return nil
}

func (h *mouseHook) uninstall() {
	if h.handle != 0 {
		procUnhookWindowsHookEx.Call(h.handle)
		h.handle = 0
	}
}

func (h *mouseHook) proc(nCode uintptr, wParam uintptr, info *msllHookStruct) uintptr {
	lParam := uintptr(unsafe.Pointer(info))
	if int32(nCode) >= 0 && info != nil {
		switch wParam {
		case win.WM_RBUTTONDOWN:
			if h.enabled != nil && h.enabled() && keyDown(win.VK_CONTROL) {
				h.swallowUp = true
				win.PostMessage(h.target, wmAppCapture, uintptr(uint32(info.Pt.X)), uintptr(uint32(info.Pt.Y)))
				return 1
			}
			h.notifyOutside(info.Pt)
		case win.WM_RBUTTONUP:
			if h.swallowUp {
				h.swallowUp = false
				return 1
			}
		case win.WM_LBUTTONDOWN:
			h.notifyOutside(info.Pt)
			// double-click detection (the low level hook never sees WM_LBUTTONDBLCLK)
			h.dblClick = info.Time-h.prevTime <= doubleClickTime() &&
				abs32(info.Pt.X-h.prevDown.X) <= 4 && abs32(info.Pt.Y-h.prevDown.Y) <= 4
			h.prevDown, h.prevTime = info.Pt, info.Time
			h.downPt, h.downTime = info.Pt, info.Time
		case win.WM_LBUTTONUP:
			dragged := abs32(info.Pt.X-h.downPt.X) >= 6 || abs32(info.Pt.Y-h.downPt.Y) >= 6
			if (dragged || h.dblClick) && h.selectionModifierOK() &&
				!h.isOwnWindowAt(info.Pt) && !h.isOwnWindowAt(h.downPt) {
				win.PostMessage(h.target, wmAppSelect, uintptr(uint32(info.Pt.X)), uintptr(uint32(info.Pt.Y)))
			}
			h.dblClick = false
		case win.WM_MBUTTONDOWN, win.WM_NCLBUTTONDOWN:
			h.notifyOutside(info.Pt)
		case win.WM_MOUSEMOVE:
			h.mouseMoved(info.Pt)
		}
	}
	r, _, _ := procCallNextHookEx.Call(h.handle, nCode, wParam, lParam)
	return r
}

// mouseMoved closes the popup once the mouse has moved away from it
// (Lingoes behaviour): the popup must first have been near the cursor and
// the cursor must then be farther than leaveDist from the popup.
func (h *mouseHook) mouseMoved(pt win.POINT) {
	if !h.leaveEnabled || !h.popupShown {
		return
	}
	d := distanceToRect(pt, h.popupRect)
	if d <= h.leaveDist {
		h.wasNear = true
		return
	}
	moved := abs32(pt.X-h.showPt.X) >= 40 || abs32(pt.Y-h.showPt.Y) >= 40
	if h.wasNear || moved {
		h.popupShown = false // report once
		win.PostMessage(h.target, wmAppLeave, 0, 0)
	}
}

func (h *mouseHook) notifyOutside(pt win.POINT) {
	if h.popupWnd == nil {
		return
	}
	pw := h.popupWnd()
	if pw == 0 || !win.IsWindowVisible(pw) {
		return
	}
	if topLevelWindowAt(pt) == pw {
		return
	}
	// The drop-down list of a combo box is a separate top-level window;
	// choosing a language in the popup must not close it.
	if windowClassAt(pt) == "ComboLBox" {
		return
	}
	win.PostMessage(h.target, wmAppOutside, 0, 0)
}

// msgWindow is a hidden message-only window receiving hotkey, clipboard
// and hook notifications on the UI thread.
type msgWindow struct {
	hwnd        win.HWND
	onHotkey    func()
	onClipboard func()
	onCapture   func(x, y int32)
	onOutside   func()
	onSelect    func(x, y int32)
	onLeave     func()
}

var msgWindowClass = syscall.StringToUTF16Ptr("LinglikeMessageWindow")

func newMsgWindow() (*msgWindow, error) {
	m := &msgWindow{}
	var wc win.WNDCLASSEX
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	wc.LpfnWndProc = syscall.NewCallback(m.wndProc)
	wc.HInstance = win.GetModuleHandle(nil)
	wc.LpszClassName = msgWindowClass
	if win.RegisterClassEx(&wc) == 0 {
		return nil, errors.New("RegisterClassEx failed")
	}
	m.hwnd = win.CreateWindowEx(0, msgWindowClass, nil, 0, 0, 0, 0, 0, win.HWND_MESSAGE, 0, wc.HInstance, nil)
	if m.hwnd == 0 {
		return nil, errors.New("CreateWindowEx failed")
	}
	return m, nil
}

func (m *msgWindow) wndProc(hwnd win.HWND, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case win.WM_HOTKEY:
		if m.onHotkey != nil {
			m.onHotkey()
		}
		return 0
	case win.WM_CLIPBOARDUPDATE:
		if m.onClipboard != nil {
			m.onClipboard()
		}
		return 0
	case wmAppCapture:
		if m.onCapture != nil {
			m.onCapture(int32(uint32(wParam)), int32(uint32(lParam)))
		}
		return 0
	case wmAppOutside:
		if m.onOutside != nil {
			m.onOutside()
		}
		return 0
	case wmAppSelect:
		if m.onSelect != nil {
			m.onSelect(int32(uint32(wParam)), int32(uint32(lParam)))
		}
		return 0
	case wmAppLeave:
		if m.onLeave != nil {
			m.onLeave()
		}
		return 0
	}
	return win.DefWindowProc(hwnd, msg, wParam, lParam)
}
