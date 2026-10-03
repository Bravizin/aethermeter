//go:build windows

package overlay

import (
	"encoding/binary"
	"syscall"
	"unsafe"
)

var (
	pDialogBoxIndirectParamW = user32.NewProc("DialogBoxIndirectParamW")
	pEndDialog               = user32.NewProc("EndDialog")
	pSetDlgItemTextW         = user32.NewProc("SetDlgItemTextW")
	pGetDlgItemTextW         = user32.NewProc("GetDlgItemTextW")
)

const (
	wmInitDialog = 0x0110
	wmCommand    = 0x0111
	idOK         = 1
	idCancel     = 2
	idEdit       = 101
	idLabel      = 102
)

// dlgBuilder writes an in-memory DLGTEMPLATE (no .rc file needed).
type dlgBuilder struct{ b []byte }

func (d *dlgBuilder) u16(v uint16) { d.b = binary.LittleEndian.AppendUint16(d.b, v) }
func (d *dlgBuilder) u32(v uint32) { d.b = binary.LittleEndian.AppendUint32(d.b, v) }
func (d *dlgBuilder) str(s string) {
	for _, c := range syscall.StringToUTF16(s) { // includes the terminating 0
		d.u16(c)
	}
}
func (d *dlgBuilder) align4() {
	for len(d.b)%4 != 0 {
		d.b = append(d.b, 0)
	}
}

func (d *dlgBuilder) item(style uint32, x, y, cx, cy int16, id uint16, class uint16, text string) {
	d.align4()
	d.u32(style)
	d.u32(0)
	d.u16(uint16(x))
	d.u16(uint16(y))
	d.u16(uint16(cx))
	d.u16(uint16(cy))
	d.u16(id)
	d.u16(0xFFFF)
	d.u16(class)
	d.str(text)
	d.u16(0) // no creation data
}

func buildInputTemplate(title, prompt string) []byte {
	const (
		wsPopupS     = 0x80000000
		wsCaption    = 0x00C00000
		wsSysMenu    = 0x00080000
		wsChildVis   = 0x50000000
		wsTabStop    = 0x00010000
		wsBorder     = 0x00800000
		dsModalFrame = 0x80
		dsSetFont    = 0x40
		dsCenter     = 0x800
		esAutoHScrl  = 0x80
		bsDefPush    = 0x1
		clsButton    = 0x0080
		clsEdit      = 0x0081
		clsStatic    = 0x0082
	)
	d := &dlgBuilder{}
	d.u32(wsPopupS | wsCaption | wsSysMenu | dsModalFrame | dsSetFont | dsCenter)
	d.u32(0x8) // WS_EX_TOPMOST
	d.u16(4)   // items
	d.u16(0)
	d.u16(0)
	d.u16(220)
	d.u16(78)
	d.u16(0) // no menu
	d.u16(0) // default class
	d.str(title)
	d.u16(9)
	d.str("Segoe UI")
	d.item(wsChildVis, 8, 8, 204, 20, idLabel, clsStatic, prompt)
	d.item(wsChildVis|wsTabStop|wsBorder|esAutoHScrl, 8, 32, 204, 14, idEdit, clsEdit, "")
	d.item(wsChildVis|wsTabStop|bsDefPush, 106, 56, 50, 14, idOK, clsButton, "OK")
	d.item(wsChildVis|wsTabStop, 162, 56, 50, 14, idCancel, clsButton, "Cancelar")
	return d.b
}

var inputState struct {
	initial string
	result  string
}

func inputDlgProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	switch msg {
	case wmInitDialog:
		pSetDlgItemTextW.Call(hwnd, idEdit, ptr(unsafe.Pointer(u16p(inputState.initial))))
		pSetForegroundWindow.Call(hwnd)
		return 1
	case wmCommand:
		switch loword(wparam) {
		case idOK:
			buf := make([]uint16, 128)
			pGetDlgItemTextW.Call(hwnd, idEdit, ptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
			inputState.result = syscall.UTF16ToString(buf)
			pEndDialog.Call(hwnd, 1)
			return 1
		case idCancel:
			pEndDialog.Call(hwnd, 0)
			return 1
		}
	}
	return 0
}

var inputDlgCallback = syscall.NewCallback(inputDlgProc)

// askText shows a small modal text box; ok=false if cancelled.
func askText(owner uintptr, title, prompt, initial string) (string, bool) {
	tmpl := buildInputTemplate(title, prompt)
	inputState.initial, inputState.result = initial, ""
	hinst, _, _ := pGetModuleHandleW.Call(0)
	r, _, _ := pDialogBoxIndirectParamW.Call(hinst, ptr(unsafe.Pointer(&tmpl[0])), owner, inputDlgCallback, 0)
	if int32(r) != 1 {
		return "", false
	}
	return inputState.result, true
}
