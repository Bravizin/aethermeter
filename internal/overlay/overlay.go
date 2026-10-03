//go:build windows

// Package overlay shows the meter as two always-on-top layered windows:
//   - header: tabs (DPS / cura / tank), alvo, HP do chefe, info — always clickable
//   - body:   player cards — click-through when the overlay is locked
//
// Drawing is done by package ui into a DIB section; GDI only renders text.
package overlay

import (
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"aethermeter/internal/applog"
	"aethermeter/internal/combat"
	"aethermeter/internal/config"
	"aethermeter/internal/engine"
	"aethermeter/internal/ui"
)

const AppName = "AetherMeter"

// menu command ids
const (
	cmdLock = 100 + iota
	cmdReset
	cmdModeMain
	cmdModeAll
	cmdPartyOnly
	cmdCopy
	cmdPrev
	cmdNext
	cmdLive
	cmdRecord
	cmdDiag
	cmdOpenLogs
	cmdOpenConfig
	cmdNpcap
	cmdHide
	cmdQuit
	cmdTabDps
	cmdTabHeal
	cmdTabTank
	cmdOpacityBase = 200 // +percent
	cmdScaleBase   = 300 // +percent
	cmdRowsBase    = 400 // +rows
)

// hotkey actions, in registration order (id = index+1)
var hotkeyActions = []string{"lock", "reset", "hide", "copy", "mode", "party", "tab"}

// Options configure the overlay at start-up.
type Options struct {
	Engine   *engine.Engine
	Config   config.Config
	StartErr string // capture error to show (e.g. Npcap missing)
	Banner   string // e.g. "DEMO" or "REPLAY"
}

// window is one layered popup with its own DIB back buffer.
type window struct {
	hwnd     uintptr
	w, h     int
	dc, bmp  uintptr
	oldBmp   uintptr
	pix      []uint32
	bufW     int
	bufH     int
	tracking bool
}

type Overlay struct {
	eng *engine.Engine
	cfg config.Config

	header, body, panel window

	view     ui.View
	hits     ui.Hits
	startErr string
	banner   string

	toast      string
	toastUntil time.Time

	scale      float64
	fonts      map[ui.Font]uintptr
	hidden     bool
	panelShown bool

	hotkeyErrs []string
}

var ov *Overlay // single instance; window procedures need a global

// Run creates the windows and runs the message loop until the user quits.
func Run(o Options) error {
	runtime.LockOSThread()
	if pSetProcessDpiAwarenessCtx.Find() == nil {
		pSetProcessDpiAwarenessCtx.Call(uintptr(^uintptr(3))) // PER_MONITOR_AWARE_V2 (-4)
	} else {
		pSetProcessDPIAware.Call()
	}

	ov = &Overlay{eng: o.Engine, cfg: o.Config, startErr: o.StartErr, banner: o.Banner, fonts: map[ui.Font]uintptr{}}
	ov.eng.Tracker.IdleTimeout = time.Duration(ov.cfg.IdleSeconds) * time.Second
	ov.eng.Tracker.Remember(ov.cfg.SelfName, ov.cfg.SelfClass)
	ov.view.HoverTab = -1

	hinst, _, _ := pGetModuleHandleW.Call(0)
	cursor, _, _ := pLoadCursorW.Call(0, 32512) // IDC_ARROW
	icon, _, _ := pLoadIconW.Call(hinst, 2)     // icon group embedded by rsrc (id 2)
	register := func(name string, proc uintptr) error {
		wc := wndClassEx{WndProc: proc, Instance: hinst, Cursor: cursor, Icon: icon, IconSm: icon, ClassName: u16p(name)}
		wc.Size = uint32(unsafe.Sizeof(wc))
		if r, _, err := pRegisterClassExW.Call(ptr(unsafe.Pointer(&wc))); r == 0 {
			return fmt.Errorf("RegisterClassEx %s: %v", name, err)
		}
		return nil
	}
	if err := register("AetherMeterHeader", syscall.NewCallback(headerProc)); err != nil {
		return err
	}
	if err := register("AetherMeterBody", syscall.NewCallback(bodyProc)); err != nil {
		return err
	}
	if err := register("AetherMeterPanel", syscall.NewCallback(panelProc)); err != nil {
		return err
	}

	x, y := ov.cfg.X, ov.cfg.Y
	if !ov.cfg.HasPos {
		sw, _, _ := pGetSystemMetrics.Call(0)
		x, y = int(sw)-ov.cfg.Width-60, 140
	}
	ex := uintptr(wsExLayered | wsExTopmost | wsExToolWindow | wsExNoActivate)
	hh, _, err := pCreateWindowExW.Call(ex, ptr(unsafe.Pointer(u16p("AetherMeterHeader"))), ptr(unsafe.Pointer(u16p(AppName))),
		wsPopup, uintptr(x), uintptr(y), uintptr(ov.cfg.Width), 60, 0, 0, hinst, 0)
	if hh == 0 {
		return fmt.Errorf("CreateWindowEx header: %v", err)
	}
	bex := ex
	if ov.cfg.Locked {
		bex |= wsExTransparent
	}
	// the body is owned by the header so it always stays right above it
	bh, _, err := pCreateWindowExW.Call(bex, ptr(unsafe.Pointer(u16p("AetherMeterBody"))), ptr(unsafe.Pointer(u16p(AppName))),
		wsPopup, uintptr(x), uintptr(y+60), uintptr(ov.cfg.Width), 60, hh, 0, hinst, 0)
	if bh == 0 {
		return fmt.Errorf("CreateWindowEx body: %v", err)
	}
	// skills side panel, opened by clicking a player
	ph, _, err := pCreateWindowExW.Call(ex, ptr(unsafe.Pointer(u16p("AetherMeterPanel"))), ptr(unsafe.Pointer(u16p(AppName))),
		wsPopup, uintptr(x), uintptr(y), uintptr(ov.cfg.Width), 60, hh, 0, hinst, 0)
	if ph == 0 {
		return fmt.Errorf("CreateWindowEx panel: %v", err)
	}
	ov.header.hwnd, ov.body.hwnd, ov.panel.hwnd = hh, bh, ph
	ov.applyOpacity()
	ov.updateScale()
	ov.registerHotkeys()
	ov.refresh()
	pShowWindow.Call(hh, swShowNoActive)
	pShowWindow.Call(bh, swShowNoActive)
	pSetTimer.Call(hh, 1, 250, 0)
	if len(ov.hotkeyErrs) > 0 {
		ov.flash("Atalho ocupado: " + strings.Join(ov.hotkeyErrs, ", ") + " (mude no config.json)")
	}

	var m msg
	for {
		r, _, _ := pGetMessageW.Call(ptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(ptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(ptr(unsafe.Pointer(&m)))
	}
	ov.saveConfig()
	return nil
}

func defProc(hwnd, umsg, wparam, lparam uintptr) uintptr {
	r, _, _ := pDefWindowProcW.Call(hwnd, umsg, wparam, lparam)
	return r
}

// clientPoint converts screen coordinates from lParam into client coordinates.
func clientPoint(hwnd, lparam uintptr) (int, int) {
	pt := point{int32(loword(lparam)), int32(hiword(lparam))}
	pScreenToClient.Call(hwnd, ptr(unsafe.Pointer(&pt)))
	return int(pt.X), int(pt.Y)
}

func headerProc(hwnd, umsg, wparam, lparam uintptr) uintptr {
	o := ov
	if o == nil || o.header.hwnd == 0 {
		return defProc(hwnd, umsg, wparam, lparam)
	}
	switch umsg {
	case wmTimer:
		o.refresh()
		return 0
	case wmPaint:
		o.paint(&o.header, paintHeader)
		return 0
	case wmEraseBkgnd:
		return 1
	case wmMouseActivate:
		return maNoActivate
	case wmNcHitTest:
		x, y := clientPoint(hwnd, lparam)
		for _, t := range o.hits.Tabs {
			if t.Has(x, y) {
				return htClient
			}
		}
		if o.cfg.Locked {
			return htClient
		}
		return htCaption
	case wmMouseMove:
		x, y := loword(lparam), hiword(lparam)
		hover := -1
		for i, t := range o.hits.Tabs {
			if t.Has(x, y) {
				hover = i
			}
		}
		if !o.header.tracking {
			tme := trackMouseEvent{Flags: tmeLeave, Hwnd: hwnd}
			tme.Size = uint32(unsafe.Sizeof(tme))
			pTrackMouseEvent.Call(ptr(unsafe.Pointer(&tme)))
			o.header.tracking = true
		}
		if hover != o.view.HoverTab {
			o.view.HoverTab = hover
			pInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case wmMouseLeave:
		o.header.tracking = false
		if o.view.HoverTab != -1 {
			o.view.HoverTab = -1
			pInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case wmSetCursor:
		if o.view.HoverTab >= 0 {
			c, _, _ := pLoadCursorW.Call(0, 32649) // IDC_HAND
			pSetCursor.Call(c)
			return 1
		}
	case wmLButtonUp:
		x, y := loword(lparam), hiword(lparam)
		for i, t := range o.hits.Tabs {
			if t.Has(x, y) {
				o.setMetric(combat.Metric(i))
				return 0
			}
		}
		return 0
	case wmRButtonUp, wmNcRButtonUp:
		o.showMenu()
		return 0
	case wmMouseWheel:
		o.wheel(wparam)
		return 0
	case wmMove, wmWindowPosChanged:
		o.placeBody()
		if umsg == wmMove {
			return 0
		}
	case wmExitSizeMove:
		o.saveConfig()
		return 0
	case wmDpiChanged:
		o.updateScale()
		o.refresh()
		return 0
	case wmHotkey:
		o.onHotkey(int(wparam))
		return 0
	case wmClose:
		pDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		o.saveConfig()
		pPostQuitMessage.Call(0)
		return 0
	}
	return defProc(hwnd, umsg, wparam, lparam)
}

func bodyProc(hwnd, umsg, wparam, lparam uintptr) uintptr {
	o := ov
	if o == nil || o.body.hwnd == 0 {
		return defProc(hwnd, umsg, wparam, lparam)
	}
	switch umsg {
	case wmPaint:
		o.paint(&o.body, paintBody)
		return 0
	case wmEraseBkgnd:
		return 1
	case wmMouseActivate:
		return maNoActivate
	case wmNcHitTest:
		return htClient
	case wmLButtonUp:
		o.onBodyClick(loword(lparam), hiword(lparam))
		return 0
	case wmRButtonUp:
		o.showMenu()
		return 0
	case wmMouseWheel:
		o.wheel(wparam)
		return 0
	}
	return defProc(hwnd, umsg, wparam, lparam)
}

func panelProc(hwnd, umsg, wparam, lparam uintptr) uintptr {
	o := ov
	if o == nil || o.panel.hwnd == 0 {
		return defProc(hwnd, umsg, wparam, lparam)
	}
	switch umsg {
	case wmPaint:
		o.paint(&o.panel, paintPanel)
		return 0
	case wmEraseBkgnd:
		return 1
	case wmMouseActivate:
		return maNoActivate
	case wmNcHitTest:
		return htClient
	case wmLButtonUp: // click anywhere on the panel closes it
		o.view.Detail = 0
		o.refresh()
		return 0
	case wmRButtonUp:
		o.showMenu()
		return 0
	}
	return defProc(hwnd, umsg, wparam, lparam)
}

// ---- state -----------------------------------------------------------------------

func (o *Overlay) px(v float64) int { return int(v*o.scale + 0.5) }

func (o *Overlay) updateScale() {
	dpi := uintptr(96)
	if pGetDpiForWindow.Find() == nil {
		if d, _, _ := pGetDpiForWindow.Call(o.header.hwnd); d != 0 {
			dpi = d
		}
	}
	s := float64(dpi) / 96 * o.cfg.Scale
	if s == o.scale && len(o.fonts) > 0 {
		return
	}
	o.scale = s
	for _, f := range o.fonts {
		pDeleteObject.Call(f)
	}
	o.fonts = map[ui.Font]uintptr{
		ui.FontTitle:     makeFont(o.px(16), 800),
		ui.FontBold:      makeFont(o.px(14.5), 700),
		ui.FontReg:       makeFont(o.px(14), 600),
		ui.FontSmall:     makeFont(o.px(12), 600),
		ui.FontSmallBold: makeFont(o.px(12), 700),
	}
}

func makeFont(h int, weight int) uintptr {
	f, _, _ := pCreateFontW.Call(uintptr(int32(-h)), 0, 0, 0, uintptr(weight), 0, 0, 0,
		1 /*DEFAULT_CHARSET*/, 0, 0, 5 /*CLEARTYPE_QUALITY*/, 0, ptr(unsafe.Pointer(u16p("Segoe UI"))))
	return f
}

func (o *Overlay) applyOpacity() {
	for _, h := range []uintptr{o.header.hwnd, o.body.hwnd, o.panel.hwnd} {
		pSetLayeredWindowAttributes.Call(h, 0, uintptr(o.cfg.Opacity), lwaAlpha)
	}
}

func (o *Overlay) placeBody() {
	if o.body.hwnd == 0 {
		return
	}
	var wr rect
	pGetWindowRect.Call(o.header.hwnd, ptr(unsafe.Pointer(&wr)))
	pSetWindowPos.Call(o.body.hwnd, 0, uintptr(wr.Left), uintptr(wr.Bottom), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
	o.placePanel()
}

// placePanel puts the skills panel beside the overlay: to the right, or to the
// left when there's no room on that monitor.
func (o *Overlay) placePanel() {
	if o.panel.hwnd == 0 || o.panel.w == 0 {
		return
	}
	var wr rect
	pGetWindowRect.Call(o.header.hwnd, ptr(unsafe.Pointer(&wr)))
	gap := int32(o.px(6))
	x := wr.Right + gap
	mon, _, _ := pMonitorFromWindow.Call(o.header.hwnd, 2 /*MONITOR_DEFAULTTONEAREST*/)
	if mon != 0 {
		mi := monitorInfo{}
		mi.Size = uint32(unsafe.Sizeof(mi))
		if r, _, _ := pGetMonitorInfoW.Call(mon, ptr(unsafe.Pointer(&mi))); r != 0 {
			if x+int32(o.panel.w) > mi.Work.Right {
				x = wr.Left - gap - int32(o.panel.w)
			}
		}
	}
	pSetWindowPos.Call(o.panel.hwnd, 0, uintptr(x), uintptr(wr.Top), 0, 0, swpNoSize|swpNoZOrder|swpNoActivate)
}

func (o *Overlay) setLocked(v bool) {
	o.cfg.Locked = v
	idx := int32(-20) // GWL_EXSTYLE
	ex, _, _ := pGetWindowLongPtrW.Call(o.body.hwnd, uintptr(idx))
	if v {
		ex |= wsExTransparent
	} else {
		ex &^= wsExTransparent
	}
	pSetWindowLongPtrW.Call(o.body.hwnd, uintptr(idx), ex)
	pSetWindowPos.Call(o.body.hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpNoActivate|swpFrameChanged)
	if v {
		o.flash("Travado: o clique passa pro jogo (abas continuam clicáveis)")
	} else {
		o.flash("Destravado: arraste pelo topo")
	}
	o.saveConfig()
}

func (o *Overlay) setMetric(m combat.Metric) {
	o.cfg.Metric = int(m)
	o.view.Detail = 0
	o.saveConfig()
	o.refresh()
}

func (o *Overlay) flash(s string) {
	o.toast = s
	o.toastUntil = time.Now().Add(2500 * time.Millisecond)
	o.refresh()
}

func (o *Overlay) saveConfig() {
	if o.header.hwnd != 0 {
		var wr rect
		pGetWindowRect.Call(o.header.hwnd, ptr(unsafe.Pointer(&wr)))
		o.cfg.X, o.cfg.Y, o.cfg.HasPos = int(wr.Left), int(wr.Top), true
	}
	if err := o.cfg.Save(); err != nil {
		applog.Printf("[config] erro salvando: %v", err)
	}
}

func (o *Overlay) wheel(wparam uintptr) {
	if hiword(wparam) > 0 {
		o.navigate(+1)
	} else {
		o.navigate(-1)
	}
}

func (o *Overlay) navigate(delta int) {
	o.view.Back += delta
	if o.view.Back < 0 {
		o.view.Back = 0
	}
	if h := o.view.Snap.History; h > 0 && o.view.Back > h-1 {
		o.view.Back = h - 1
	}
	o.refresh()
}

func (o *Overlay) registerHotkeys() {
	for i, action := range hotkeyActions {
		combo := o.cfg.Hotkeys[action]
		if combo == "" || strings.EqualFold(combo, "none") {
			continue
		}
		mods, vk, err := config.ParseHotkey(combo)
		if err != nil {
			applog.Printf("[atalho] %s = %q inválido: %v", action, combo, err)
			o.hotkeyErrs = append(o.hotkeyErrs, combo)
			continue
		}
		if r, _, _ := pRegisterHotKey.Call(o.header.hwnd, uintptr(i+1), uintptr(mods)|modNoRepeat, uintptr(vk)); r == 0 {
			applog.Printf("[atalho] %s já está em uso por outro programa", combo)
			o.hotkeyErrs = append(o.hotkeyErrs, combo)
		}
	}
}

func (o *Overlay) hk(action string) string {
	if c := o.cfg.Hotkeys[action]; c != "" {
		return "\t" + c
	}
	return ""
}

func (o *Overlay) onHotkey(id int) {
	if id < 1 || id > len(hotkeyActions) {
		return
	}
	switch hotkeyActions[id-1] {
	case "lock":
		o.setLocked(!o.cfg.Locked)
	case "reset":
		o.command(cmdReset)
	case "hide":
		o.command(cmdHide)
	case "copy":
		o.command(cmdCopy)
	case "mode":
		if o.cfg.Mode == int(combat.ViewAll) {
			o.command(cmdModeMain)
		} else {
			o.command(cmdModeAll)
		}
	case "party":
		o.command(cmdPartyOnly)
	case "tab":
		o.setMetric(combat.Metric((o.cfg.Metric + 1) % 3))
	}
}

// onBodyClick opens the skills panel for a player, or closes it when the
// same player is clicked again.
func (o *Overlay) onBodyClick(x, y int) {
	for _, r := range o.hits.Rows {
		if r.Has(x, y) {
			if o.view.Detail == r.ID {
				o.view.Detail = 0
			} else {
				o.view.Detail = r.ID
			}
			o.refresh()
			return
		}
	}
}

func (o *Overlay) command(id int) {
	switch {
	case id == cmdLock:
		o.setLocked(!o.cfg.Locked)
	case id == cmdReset:
		o.eng.Tracker.Reset()
		o.view.Back, o.view.Detail = 0, 0
		o.flash("Medidor zerado")
	case id == cmdTabDps:
		o.setMetric(combat.MetricDamage)
	case id == cmdTabHeal:
		o.setMetric(combat.MetricHeal)
	case id == cmdTabTank:
		o.setMetric(combat.MetricTaken)
	case id == cmdModeMain:
		o.cfg.Mode = int(combat.ViewMainTarget)
		o.flash("Dano: só no alvo principal / chefe")
		o.saveConfig()
	case id == cmdModeAll:
		o.cfg.Mode = int(combat.ViewAll)
		o.flash("Dano: tudo o que apanhou na luta")
		o.saveConfig()
	case id == cmdPartyOnly:
		o.cfg.PartyOnly = !o.cfg.PartyOnly
		if o.cfg.PartyOnly {
			o.flash("Mostrando só a PT")
		} else {
			o.flash("Mostrando todos os jogadores")
		}
		o.saveConfig()
	case id == cmdCopy:
		if len(o.view.Snap.Rows) == 0 {
			o.flash("Nada pra copiar ainda")
		} else if setClipboard(o.header.hwnd, combat.Summary(o.view.Snap)) {
			o.flash("Resumo copiado — cola no Discord")
		}
	case id == cmdPrev:
		o.navigate(+1)
	case id == cmdNext:
		o.navigate(-1)
	case id == cmdLive:
		o.view.Back = 0
		o.refresh()
	case id == cmdRecord:
		if o.eng.Recording() != "" {
			o.eng.StopRecording()
			o.flash("Gravação salva na pasta de logs")
		} else {
			name := fmt.Sprintf("sessao-%s.pmrec", time.Now().Format("20060102-150405"))
			if err := o.eng.StartRecording(filepath.Join(applog.Dir(), name)); err != nil {
				o.flash("Erro ao gravar: " + err.Error())
			} else {
				o.flash("Gravando a sessão pra diagnóstico…")
			}
		}
	case id == cmdDiag:
		messageBox(o.header.hwnd, AppName+" — diagnóstico", o.diagnostics(), mbIconInfo)
	case id == cmdOpenLogs:
		shellOpen(applog.Dir())
	case id == cmdOpenConfig:
		o.saveConfig()
		shellOpen(filepath.Join(applog.Dir(), "config.json"))
	case id == cmdNpcap:
		shellOpen("https://npcap.com/#download")
	case id == cmdHide:
		o.hidden = !o.hidden
		show := uintptr(swShowNoActive)
		if o.hidden {
			show = swHide
		}
		pShowWindow.Call(o.header.hwnd, show)
		pShowWindow.Call(o.body.hwnd, show)
		if o.hidden || !o.view.PanelOpen() {
			pShowWindow.Call(o.panel.hwnd, swHide)
		} else {
			pShowWindow.Call(o.panel.hwnd, swShowNoActive)
		}
	case id == cmdQuit:
		pDestroyWindow.Call(o.header.hwnd)
	case id >= cmdOpacityBase && id < cmdOpacityBase+101:
		o.cfg.Opacity = (id - cmdOpacityBase) * 255 / 100
		o.applyOpacity()
		o.saveConfig()
	case id >= cmdScaleBase && id < cmdScaleBase+201:
		o.cfg.Scale = float64(id-cmdScaleBase) / 100
		o.updateScale()
		o.refresh()
		o.saveConfig()
	case id >= cmdRowsBase && id < cmdRowsBase+30:
		o.cfg.MaxRows = id - cmdRowsBase
		o.refresh()
		o.saveConfig()
	}
}

func (o *Overlay) diagnostics() string {
	st := o.eng.CaptureStatus()
	var b strings.Builder
	fmt.Fprintf(&b, "Captura: %s — %s\n", st.State, st.Detail)
	if st.Adapter != "" {
		fmt.Fprintf(&b, "Adaptador: %s\nPorta do servidor: %d\n", st.Adapter, st.Port)
	}
	fmt.Fprintf(&b, "\nPacotes do jogo: %d\nEventos decodificados: %d\nEventos de dano: %d\n",
		o.eng.Packets.Load(), o.eng.Events.Load(), o.eng.Damage.Load())
	counts := o.eng.OpcodeCounts()
	type kv struct {
		op uint16
		n  int64
	}
	var list []kv
	for k, v := range counts {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].n > list[j].n })
	if len(list) > 10 {
		list = list[:10]
	}
	if len(list) > 0 {
		b.WriteString("\nOpcodes mais vistos:\n")
		for _, e := range list {
			fmt.Fprintf(&b, "  %02X %02X  ×%d\n", byte(e.op), byte(e.op>>8), e.n)
		}
	}
	if o.startErr != "" {
		fmt.Fprintf(&b, "\nErro: %s\n", o.startErr)
	}
	if len(o.hotkeyErrs) > 0 {
		fmt.Fprintf(&b, "\nAtalhos que não registraram: %s\n", strings.Join(o.hotkeyErrs, ", "))
	}
	fmt.Fprintf(&b, "\nLog: %s", applog.Path())
	if r := o.eng.Recording(); r != "" {
		fmt.Fprintf(&b, "\nGravando: %s", r)
	}
	return b.String()
}

func (o *Overlay) showMenu() {
	m, _, _ := pCreatePopupMenu.Call()
	defer pDestroyMenu.Call(m)
	add := func(menu uintptr, flags uintptr, id int, text string) {
		pAppendMenuW.Call(menu, flags, uintptr(id), ptr(unsafe.Pointer(u16p(text))))
	}
	sep := func(menu uintptr) { pAppendMenuW.Call(menu, mfSeparator, 0, 0) }
	chk := func(b bool) uintptr {
		if b {
			return mfChecked
		}
		return 0
	}
	gray := func(ok bool) uintptr {
		if ok {
			return 0
		}
		return mfGrayed
	}

	add(m, chk(o.cfg.Metric == 0), cmdTabDps, "Aba: dano (DPS)"+o.hk("tab"))
	add(m, chk(o.cfg.Metric == 1), cmdTabHeal, "Aba: cura (HPS)")
	add(m, chk(o.cfg.Metric == 2), cmdTabTank, "Aba: dano recebido (tank)")
	sep(m)
	add(m, chk(o.cfg.Locked), cmdLock, "Travar (clique passa pro jogo)"+o.hk("lock"))
	add(m, 0, cmdReset, "Zerar medidor"+o.hk("reset"))
	add(m, 0, cmdCopy, "Copiar resumo p/ Discord"+o.hk("copy"))
	sep(m)
	add(m, chk(o.cfg.Mode == int(combat.ViewMainTarget)), cmdModeMain, "Dano: só no chefe / alvo principal"+o.hk("mode"))
	add(m, chk(o.cfg.Mode == int(combat.ViewAll)), cmdModeAll, "Dano: tudo da luta (inclui mobs)")
	add(m, chk(o.cfg.PartyOnly), cmdPartyOnly, "Mostrar só a minha PT"+o.hk("party"))
	sep(m)
	add(m, gray(o.view.Back < o.view.Snap.History-1), cmdPrev, "« Luta anterior\t(roda do mouse)")
	add(m, gray(o.view.Back > 0), cmdNext, "Luta seguinte »")
	add(m, gray(o.view.Back > 0), cmdLive, "Voltar pra luta atual")
	sep(m)

	look, _, _ := pCreatePopupMenu.Call()
	for _, p := range []int{100, 92, 85, 75, 65, 50} {
		add(look, chk(o.cfg.Opacity*100/255 == p || (p == 100 && o.cfg.Opacity == 255)), cmdOpacityBase+p, fmt.Sprintf("Opacidade %d%%", p))
	}
	sep(look)
	for _, p := range []int{85, 100, 115, 130, 150} {
		add(look, chk(int(o.cfg.Scale*100+0.5) == p), cmdScaleBase+p, fmt.Sprintf("Tamanho %d%%", p))
	}
	sep(look)
	for _, n := range []int{5, 8, 12, 16} {
		add(look, chk(o.cfg.MaxRows == n), cmdRowsBase+n, fmt.Sprintf("Até %d jogadores", n))
	}
	pAppendMenuW.Call(m, mfPopup, look, ptr(unsafe.Pointer(u16p("Aparência"))))

	tools, _, _ := pCreatePopupMenu.Call()
	add(tools, chk(o.eng.Recording() != ""), cmdRecord, "Gravar sessão (p/ corrigir após patch)")
	add(tools, 0, cmdDiag, "Diagnóstico…")
	add(tools, 0, cmdOpenConfig, "Editar atalhos (config.json)…")
	add(tools, 0, cmdOpenLogs, "Abrir pasta de logs")
	add(tools, 0, cmdNpcap, "Baixar Npcap")
	pAppendMenuW.Call(m, mfPopup, tools, ptr(unsafe.Pointer(u16p("Ferramentas"))))
	sep(m)
	add(m, 0, cmdHide, "Esconder"+o.hk("hide"))
	add(m, 0, cmdQuit, "Sair")

	var pt point
	pGetCursorPos.Call(ptr(unsafe.Pointer(&pt)))
	pSetForegroundWindow.Call(o.header.hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(m, tpmReturnCmd|tpmRightButton, uintptr(pt.X), uintptr(pt.Y), 0, o.header.hwnd, 0)
	if cmd != 0 {
		o.command(int(cmd))
	}
}

// ---- layout & painting --------------------------------------------------------------

func (o *Overlay) statusText() (string, uint32) {
	if o.startErr != "" {
		return o.startErr, ui.ColBad
	}
	if o.banner != "" {
		return "Aguardando dados…", ui.ColDim
	}
	st := o.eng.CaptureStatus()
	switch st.State {
	case "conectado":
		return "Conectado ao jogo — esperando combate", ui.ColGood
	case "erro":
		return st.Detail, ui.ColBad
	default:
		return "Procurando o jogo… (abra o Aion 2 e entre no mapa)", ui.ColDim
	}
}

func (o *Overlay) refresh() {
	if o.header.hwnd == 0 {
		return
	}
	v := &o.view
	v.Snap = o.eng.Tracker.Snapshot(combat.SnapshotOptions{
		Mode:      combat.ViewMode(o.cfg.Mode),
		Metric:    combat.Metric(o.cfg.Metric),
		PartyOnly: o.cfg.PartyOnly,
		Back:      v.Back,
	})
	if n, c := o.eng.Tracker.SelfIdentity(); n != "" && (n != o.cfg.SelfName || c != o.cfg.SelfClass) {
		o.cfg.SelfName, o.cfg.SelfClass = n, c // remember your character for next time
		o.saveConfig()
	}
	v.Metric = combat.Metric(o.cfg.Metric)
	v.Mode = combat.ViewMode(o.cfg.Mode)
	v.PartyOnly = o.cfg.PartyOnly
	v.Locked = o.cfg.Locked
	v.MaxRows = o.cfg.MaxRows
	v.Scale = o.scale
	v.Width = o.cfg.Width
	v.Banner = o.banner
	v.Recording = o.eng.Recording() != ""
	v.Status, v.StatusCol = o.statusText()
	v.Toast = ""
	if time.Now().Before(o.toastUntil) {
		v.Toast = o.toast
	}
	if v.Detail != 0 {
		found := false
		for _, r := range v.Snap.Rows {
			found = found || r.ID == v.Detail
		}
		if !found {
			v.Detail = 0
		}
	}

	w := o.px(float64(o.cfg.Width))
	o.resize(&o.header, w, v.HeaderHeight(), regionTop)
	o.resize(&o.body, w, v.BodyHeight(), regionBottom)
	if v.PanelOpen() {
		o.resize(&o.panel, o.px(float64(o.cfg.Width)), v.PanelHeight(), regionAll)
		o.placeBody() // position the panel before showing it
		if !o.hidden && !o.panelShown {
			pShowWindow.Call(o.panel.hwnd, swShowNoActive)
			o.panelShown = true
		}
		pInvalidateRect.Call(o.panel.hwnd, 0, 0)
	} else if o.panelShown {
		pShowWindow.Call(o.panel.hwnd, swHide)
		o.panelShown = false
	}
	o.placeBody()
	pInvalidateRect.Call(o.header.hwnd, 0, 0)
	pInvalidateRect.Call(o.body.hwnd, 0, 0)
}

const (
	regionTop = iota
	regionBottom
	regionAll
)

func (o *Overlay) resize(win *window, w, h int, shape int) {
	if w == win.w && h == win.h {
		return
	}
	win.w, win.h = w, h
	pSetWindowPos.Call(win.hwnd, ^uintptr(0) /*HWND_TOPMOST*/, 0, 0, uintptr(w), uintptr(h), swpNoMove|swpNoActivate)
	// rounded outer corners: header rounds the top, body rounds the bottom
	r := o.px(12)
	var rgn uintptr
	switch shape {
	case regionTop:
		rgn, _, _ = pCreateRoundRectRgn.Call(0, 0, uintptr(w+1), uintptr(h+r+1), uintptr(r), uintptr(r))
	case regionBottom:
		rgn, _, _ = pCreateRoundRectRgn.Call(0, uintptr(int32(-r)), uintptr(w+1), uintptr(h+1), uintptr(r), uintptr(r))
	default:
		rgn, _, _ = pCreateRoundRectRgn.Call(0, 0, uintptr(w+1), uintptr(h+1), uintptr(r), uintptr(r))
	}
	pSetWindowRgn.Call(win.hwnd, rgn, 1)
}

// gdiSurface renders shapes with package ui and text with GDI.
type gdiSurface struct {
	r     *ui.Raster
	dc    uintptr
	fonts map[ui.Font]uintptr
}

func (s *gdiSurface) Raster() *ui.Raster { return s.r }
func (s *gdiSurface) Flush()             { pGdiFlush.Call() }

func (s *gdiSurface) Text(str string, x0, y0, x1, y1 int, c uint32, f ui.Font, a ui.Align) {
	if str == "" || x1 <= x0 {
		return
	}
	pSelectObject.Call(s.dc, s.fonts[f])
	u, _ := syscall.UTF16FromString(str)
	flags := uintptr(dtSingleLine | dtVCenter | dtNoPrefix | dtEndEllip)
	switch a {
	case ui.Center:
		flags |= dtCenter
	case ui.Right:
		flags |= dtRight
	}
	// 1px dark shadow keeps text readable over the bars and the game
	pSetTextColor.Call(s.dc, 0)
	sh := rect{int32(x0 + 1), int32(y0 + 1), int32(x1 + 1), int32(y1 + 1)}
	pDrawTextW.Call(s.dc, ptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), ptr(unsafe.Pointer(&sh)), flags)
	pSetTextColor.Call(s.dc, rgb(c))
	r := rect{int32(x0), int32(y0), int32(x1), int32(y1)}
	pDrawTextW.Call(s.dc, ptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), ptr(unsafe.Pointer(&r)), flags)
}

// ensureBuffer (re)creates the window's DIB back buffer.
func (win *window) ensureBuffer(hdc uintptr, w, h int) bool {
	if win.dc != 0 && win.bufW == w && win.bufH == h {
		return true
	}
	win.freeBuffer()
	bmi := bitmapInfo{Header: bitmapInfoHeader{Width: int32(w), Height: int32(-h), Planes: 1, BitCount: 32}}
	bmi.Header.Size = uint32(unsafe.Sizeof(bmi.Header))
	var bits uintptr
	bmp, _, _ := pCreateDIBSection.Call(hdc, ptr(unsafe.Pointer(&bmi)), 0, ptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == 0 {
		return false
	}
	dc, _, _ := pCreateCompatibleDC.Call(hdc)
	old, _, _ := pSelectObject.Call(dc, bmp)
	pSetBkMode.Call(dc, 1) // TRANSPARENT
	win.dc, win.bmp, win.oldBmp = dc, bmp, old
	win.pix = unsafe.Slice((*uint32)(unsafe.Add(nil, bits)), w*h)
	win.bufW, win.bufH = w, h
	return true
}

func (win *window) freeBuffer() {
	if win.dc != 0 {
		pSelectObject.Call(win.dc, win.oldBmp)
		pDeleteDC.Call(win.dc)
		pDeleteObject.Call(win.bmp)
		win.dc, win.bmp, win.pix = 0, 0, nil
	}
}

const (
	paintHeader = iota
	paintBody
	paintPanel
)

func (o *Overlay) paint(win *window, which int) {
	var ps paintStruct
	hdc, _, _ := pBeginPaint.Call(win.hwnd, ptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(win.hwnd, ptr(unsafe.Pointer(&ps)))

	var cr rect
	pGetClientRect.Call(win.hwnd, ptr(unsafe.Pointer(&cr)))
	w, h := int(cr.Right), int(cr.Bottom)
	if w <= 0 || h <= 0 || !win.ensureBuffer(hdc, w, h) {
		return
	}
	s := &gdiSurface{r: &ui.Raster{Pix: win.pix, W: w, H: h}, dc: win.dc, fonts: o.fonts}
	switch which {
	case paintHeader:
		ui.DrawHeader(s, &o.view, &o.hits)
	case paintBody:
		ui.DrawBody(s, &o.view, &o.hits)
	default:
		ui.DrawPanel(s, &o.view)
	}
	pGdiFlush.Call()
	pBitBlt.Call(hdc, 0, 0, uintptr(w), uintptr(h), win.dc, 0, 0, 0x00CC0020)
}
