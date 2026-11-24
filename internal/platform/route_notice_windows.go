package platform

import (
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

type noticeRect struct{ left, top, right, bottom int32 }

type noticeMessage struct {
	window  uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	x, y    int32
	private uint32
}

// ShowRouteNotice displays a short, non-activating popup above the taskbar.
// The calling goroutine owns the window and its messages until it closes.
func ShowRouteNotice(message string) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	user := windows.NewLazySystemDLL("user32.dll")
	create := user.NewProc("CreateWindowExW")
	var area noticeRect
	const spiGetWorkArea = 0x0030
	ok, _, _ := user.NewProc("SystemParametersInfoW").Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&area)), 0)
	if ok == 0 || area.right <= area.left || area.bottom <= area.top {
		return
	}
	width := min(int32(420), area.right-area.left)
	const height = 115
	class, _ := windows.UTF16PtrFromString("STATIC")
	title, _ := windows.UTF16PtrFromString("NetMerge")
	body, err := windows.UTF16PtrFromString(message)
	if err != nil {
		return
	}
	const (
		noActivateToolWindow = 0x08000088 // WS_EX_NOACTIVATE | WS_EX_TOPMOST | WS_EX_TOOLWINDOW
		borderedPopup        = 0x80800000 // WS_POPUP | WS_BORDER
		visibleCenteredText  = 0x50000081 // WS_CHILD | WS_VISIBLE | SS_CENTER | SS_NOPREFIX
	)
	window, _, _ := create.Call(noActivateToolWindow, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)),
		borderedPopup, uintptr(max(area.left, area.right-width-12)), uintptr(max(area.top, area.bottom-height-12)), uintptr(width), height,
		0, 0, 0, 0)
	if window == 0 {
		return
	}
	defer user.NewProc("DestroyWindow").Call(window)
	control, _, _ := create.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(body)),
		visibleCenteredText, 12, 12, uintptr(width-26), height-26, window, 0, 0, 0)
	if control == 0 {
		return
	}
	const defaultGUIFont = 17
	font, _, _ := windows.NewLazySystemDLL("gdi32.dll").NewProc("GetStockObject").Call(defaultGUIFont)
	if font != 0 {
		user.NewProc("SendMessageW").Call(control, 0x0030, font, 0) // WM_SETFONT
	}
	user.NewProc("ShowWindow").Call(window, 4) // SW_SHOWNOACTIVATE
	user.NewProc("UpdateWindow").Call(window)
	peek := user.NewProc("PeekMessageW")
	dispatch := user.NewProc("DispatchMessageW")
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		for {
			var msg noticeMessage
			if ok, _, _ := peek.Call(uintptr(unsafe.Pointer(&msg)), window, 0, 0, 1); ok == 0 { // PM_REMOVE
				break
			}
			dispatch.Call(uintptr(unsafe.Pointer(&msg)))
		}
		time.Sleep(25 * time.Millisecond)
	}
}
