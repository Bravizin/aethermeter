//go:build windows

package overlay

import (
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")

	pRegisterClassExW           = user32.NewProc("RegisterClassExW")
	pCreateWindowExW            = user32.NewProc("CreateWindowExW")
	pDefWindowProcW             = user32.NewProc("DefWindowProcW")
	pGetMessageW                = user32.NewProc("GetMessageW")
	pTranslateMessage           = user32.NewProc("TranslateMessage")
	pDispatchMessageW           = user32.NewProc("DispatchMessageW")
	pPostQuitMessage            = user32.NewProc("PostQuitMessage")
	pShowWindow                 = user32.NewProc("ShowWindow")
	pIsWindowVisible            = user32.NewProc("IsWindowVisible")
	pSetWindowPos               = user32.NewProc("SetWindowPos")
	pGetWindowRect              = user32.NewProc("GetWindowRect")
	pGetClientRect              = user32.NewProc("GetClientRect")
	pSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	pGetWindowLongPtrW          = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtrW          = user32.NewProc("SetWindowLongPtrW")
	pBeginPaint                 = user32.NewProc("BeginPaint")
	pEndPaint                   = user32.NewProc("EndPaint")
	pInvalidateRect             = user32.NewProc("InvalidateRect")
	pFillRect                   = user32.NewProc("FillRect")
	pDrawTextW                  = user32.NewProc("DrawTextW")
	pSetTimer                   = user32.NewProc("SetTimer")
	pRegisterHotKey             = user32.NewProc("RegisterHotKey")
	pUnregisterHotKey           = user32.NewProc("UnregisterHotKey")
	pCreatePopupMenu            = user32.NewProc("CreatePopupMenu")
	pAppendMenuW                = user32.NewProc("AppendMenuW")
	pTrackPopupMenu             = user32.NewProc("TrackPopupMenu")
	pDestroyMenu                = user32.NewProc("DestroyMenu")
	pGetCursorPos               = user32.NewProc("GetCursorPos")
	pScreenToClient             = user32.NewProc("ScreenToClient")
	pSetForegroundWindow        = user32.NewProc("SetForegroundWindow")
	pMessageBoxW                = user32.NewProc("MessageBoxW")
	pLoadCursorW                = user32.NewProc("LoadCursorW")
	pLoadIconW                  = user32.NewProc("LoadIconW")
	pTrackMouseEvent            = user32.NewProc("TrackMouseEvent")
	pSetCursor                  = user32.NewProc("SetCursor")
	pMonitorFromWindow          = user32.NewProc("MonitorFromWindow")
	pUpdateLayeredWindow        = user32.NewProc("UpdateLayeredWindow")
	pGetDC                      = user32.NewProc("GetDC")
	pReleaseDC                  = user32.NewProc("ReleaseDC")
	pGetMonitorInfoW            = user32.NewProc("GetMonitorInfoW")
	pSetWindowRgn               = user32.NewProc("SetWindowRgn")
	pGetDpiForWindow            = user32.NewProc("GetDpiForWindow")
	pSetProcessDpiAwarenessCtx  = user32.NewProc("SetProcessDpiAwarenessContext")
	pSetProcessDPIAware         = user32.NewProc("SetProcessDPIAware")
	pOpenClipboard              = user32.NewProc("OpenClipboard")
	pEmptyClipboard             = user32.NewProc("EmptyClipboard")
	pSetClipboardData           = user32.NewProc("SetClipboardData")
	pCloseClipboard             = user32.NewProc("CloseClipboard")
	pGetSystemMetrics           = user32.NewProc("GetSystemMetrics")
	pDestroyWindow              = user32.NewProc("DestroyWindow")

	pCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject           = gdi32.NewProc("SelectObject")
	pDeleteObject           = gdi32.NewProc("DeleteObject")
	pDeleteDC               = gdi32.NewProc("DeleteDC")
	pBitBlt                 = gdi32.NewProc("BitBlt")
	pCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	pSetBkMode              = gdi32.NewProc("SetBkMode")
	pSetTextColor           = gdi32.NewProc("SetTextColor")
	pCreateFontW            = gdi32.NewProc("CreateFontW")
	pCreateRoundRectRgn     = gdi32.NewProc("CreateRoundRectRgn")
	pCreateDIBSection       = gdi32.NewProc("CreateDIBSection")
	pGdiFlush               = gdi32.NewProc("GdiFlush")

	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	pGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	pGlobalLock       = kernel32.NewProc("GlobalLock")
	pGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	pCreateMutexW     = kernel32.NewProc("CreateMutexW")

	pShellExecuteW = shell32.NewProc("ShellExecuteW")
)

const (
	wsPopup         = 0x80000000
	wsExLayered     = 0x00080000
	wsExTopmost     = 0x00000008
	wsExToolWindow  = 0x00000080
	wsExTransparent = 0x00000020
	wsExNoActivate  = 0x08000000

	wmDestroy          = 0x0002
	wmMove             = 0x0003
	wmSetCursor        = 0x0020
	wmWindowPosChanged = 0x0047
	wmMouseMove        = 0x0200
	wmMouseLeave       = 0x02A3
	tmeLeave           = 0x00000002
	wmPaint            = 0x000F
	wmClose            = 0x0010
	wmEraseBkgnd       = 0x0014
	wmMouseActivate    = 0x0021
	wmNcHitTest        = 0x0084
	wmNcRButtonUp      = 0x00A5
	wmTimer            = 0x0113
	wmLButtonUp        = 0x0202
	wmRButtonUp        = 0x0205
	wmMouseWheel       = 0x020A
	wmExitSizeMove     = 0x0232
	wmDpiChanged       = 0x02E0
	wmHotkey           = 0x0312
	wmApp              = 0x8000

	htClient  = 1
	htCaption = 2

	maNoActivate = 3

	swHide          = 0
	swShowNoActive  = 4
	swpNoSize       = 0x0001
	swpNoMove       = 0x0002
	swpNoZOrder     = 0x0004
	swpNoActivate   = 0x0010
	swpFrameChanged = 0x0020

	lwaAlpha = 0x2

	dtLeft       = 0x0000
	dtCenter     = 0x0001
	dtRight      = 0x0002
	dtVCenter    = 0x0004
	dtSingleLine = 0x0020
	dtNoPrefix   = 0x0800
	dtEndEllip   = 0x8000

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfChecked   = 0x0008
	mfPopup     = 0x0010
	mfSeparator = 0x0800

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	modControl  = 0x0002
	modShift    = 0x0004
	modNoRepeat = 0x4000

	cfUnicodeText = 13
	gmemMoveable  = 0x0002

	mbOK        = 0x0
	mbIconError = 0x10
	mbIconInfo  = 0x40
	mbTopmost   = 0x40000

	errorAlreadyExists = 183
)

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

type trackMouseEvent struct {
	Size      uint32
	Flags     uint32
	Hwnd      uintptr
	HoverTime uint32
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [1]uint32
}

type paintStruct struct {
	Hdc         uintptr
	Erase       int32
	RcPaint     rect
	Restore     int32
	IncUpdate   int32
	RgbReserved [32]byte
}

func u16p(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func ptr(p unsafe.Pointer) uintptr { return uintptr(p) }

// rgb converts 0xRRGGBB into a GDI COLORREF (0x00BBGGRR).
func rgb(c uint32) uintptr {
	r, g, b := (c>>16)&0xFF, (c>>8)&0xFF, c&0xFF
	return uintptr(r | g<<8 | b<<16)
}

// mix blends colour a towards b by t (0..1).
func mix(a, b uint32, t float64) uint32 {
	ch := func(s uint) uint32 {
		x := float64((a>>s)&0xFF)*(1-t) + float64((b>>s)&0xFF)*t
		return uint32(x+0.5) & 0xFF
	}
	return ch(16)<<16 | ch(8)<<8 | ch(0)
}

func loword(v uintptr) int { return int(int16(v & 0xFFFF)) }
func hiword(v uintptr) int { return int(int16((v >> 16) & 0xFFFF)) }

func messageBox(hwnd uintptr, title, text string, flags uintptr) {
	pMessageBoxW.Call(hwnd, ptr(unsafe.Pointer(u16p(text))), ptr(unsafe.Pointer(u16p(title))), flags|mbTopmost)
}

func shellOpen(target string) {
	pShellExecuteW.Call(0, ptr(unsafe.Pointer(u16p("open"))), ptr(unsafe.Pointer(u16p(target))), 0, 0, 1)
}

func setClipboard(hwnd uintptr, text string) bool {
	u, err := syscall.UTF16FromString(text)
	if err != nil {
		return false
	}
	if r, _, _ := pOpenClipboard.Call(hwnd); r == 0 {
		return false
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()
	size := uintptr(len(u) * 2)
	h, _, _ := pGlobalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return false
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		return false
	}
	dst := unsafe.Slice((*uint16)(unsafe.Add(nil, p)), len(u))
	copy(dst, u)
	pGlobalUnlock.Call(h)
	r, _, _ := pSetClipboardData.Call(cfUnicodeText, h)
	return r != 0
}

// SingleInstance returns false if another AetherMeter is already running.
func SingleInstance() bool {
	_, _, e := pCreateMutexW.Call(0, 0, ptr(unsafe.Pointer(u16p("Local\\AetherMeterSingleInstance"))))
	return e != syscall.Errno(errorAlreadyExists)
}

// ShowError shows a blocking error dialog (used before the overlay exists).
func ShowError(title, text string) { messageBox(0, title, text, mbIconError) }
