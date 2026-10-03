package ui

import (
	"fmt"
	"strings"
	"time"

	"aethermeter/internal/combat"
	"aethermeter/internal/glyph"
)

// Palette (0xRRGGBB).
const (
	ColBg      = 0x0F1217
	ColHeader  = 0x151922
	ColSub     = 0x12161D
	ColCard    = 0x1A1F28
	ColCardHi  = 0x222836
	ColText    = 0xECEEF1
	ColDim     = 0xA3ACB7
	ColFaint   = 0x6B7480
	ColAccent  = 0xF2B84B
	ColAether  = 0x7FD4FF
	ColGood    = 0x5BD38A
	ColBad     = 0xE5534B
	ColHpTrack = 0x262B35
	ColHp      = 0xD8464B
	ColHpHi    = 0xF06A6E
	ColGlyphOn = 0x0E1116
)

type Font int

const (
	FontTitle Font = iota // header title / big numbers
	FontBold              // names, values
	FontReg
	FontSmall
	FontSmallBold
)

type Align int

const (
	Left Align = iota
	Center
	Right
)

// Surface is what the drawing code paints on.
type Surface interface {
	Raster() *Raster
	// Text draws single-line text, vertically centred in the box, with
	// ellipsis when it doesn't fit, and a subtle shadow.
	Text(s string, x0, y0, x1, y1 int, c uint32, f Font, a Align)
	// Flush must be called between text drawing and direct pixel access
	// (GDI batches its work).
	Flush()
}

// View is everything the overlay shows.
type View struct {
	Snap      combat.Snapshot
	Metric    combat.Metric
	Mode      combat.ViewMode
	PartyOnly bool
	Back      int
	Locked    bool
	Detail    int // player id shown in detail, 0 = list
	MaxRows   int
	Toast     string
	Status    string
	StatusCol uint32
	Banner    string
	Recording bool
	HoverTab  int // -1 none
	Scale     float64
	Width     int // logical width
}

// Rect is a hit-test area in window pixels.
type Rect struct{ X0, Y0, X1, Y1 int }

func (r Rect) Has(x, y int) bool { return x >= r.X0 && x < r.X1 && y >= r.Y0 && y < r.Y1 }

// Hits are the clickable regions produced while drawing.
type Hits struct {
	Tabs [3]Rect
	Rows []struct {
		Rect
		ID int
	}
}

func (v *View) px(f float64) int     { return int(f*v.Scale + 0.5) }
func (v *View) pf(f float64) float64 { return f * v.Scale }

// ---- layout -------------------------------------------------------------------

const (
	lRowA    = 38
	lHp      = 22
	lRowC    = 24
	lPad     = 8
	lCard    = 42
	lGap     = 4
	lTop     = 5 // transparent gap between the header and the first card
	lSkill   = 28
	lEmpty   = 54
	lTab     = 30
	lTabGap  = 4
	lRadius  = 10
	lHdrRad  = 14
	lBadge   = 30
	lMaxSkil = 14
)

// HeaderHeight in window pixels.
func (v *View) HeaderHeight() int {
	h := lRowA + lRowC + 4
	if v.Snap.TargetMaxHP > 0 {
		h += lHp + 2
	}
	return v.px(float64(h))
}

func (v *View) visibleRows() []combat.Row {
	rows := v.Snap.Rows
	n := min(len(rows), v.MaxRows)
	vis := append([]combat.Row(nil), rows[:n]...)
	for i := n; i < len(rows); i++ { // keep yourself visible
		if rows[i].IsSelf && n > 0 {
			vis[n-1] = rows[i]
			break
		}
	}
	return vis
}

func (v *View) detailRow() *combat.Row {
	for i := range v.Snap.Rows {
		if v.Snap.Rows[i].ID == v.Detail {
			return &v.Snap.Rows[i]
		}
	}
	return nil
}

// BodyHeight in window pixels.
func (v *View) BodyHeight() int {
	n := len(v.visibleRows())
	if n == 0 {
		return v.px(lEmpty)
	}
	return v.px(float64(lTop + n*lCard + (n-1)*lGap))
}

// Opacity of the floating pieces (the rest of the window is fully transparent).
const (
	aHeader = 0.86 // header card
	aCard   = 0.40 // player card background ("quase transparente")
	aFill   = 0.62 // class-coloured damage bar
	aPanel  = 0.90 // skills panel
)

// ---- header -------------------------------------------------------------------

var tabGlyph = [3]string{"tab_dps", "tab_heal", "tab_tank"}

// DrawHeader paints the header (tabs, title, HP bar, info line).
func DrawHeader(s Surface, v *View, h *Hits) {
	r := s.Raster()
	w := r.W
	snap := v.Snap
	r.Clear()
	r.RoundRect(0, 0, float64(w), float64(r.H), v.pf(lHdrRad), ColHeader, aHeader)
	// subtle aether line on top
	r.RoundRect(v.pf(18), v.pf(1), float64(w)-v.pf(18), v.pf(3), v.pf(1), ColAether, 0.6)

	// tabs
	ty := v.px(4)
	for i := 0; i < 3; i++ {
		x := v.px(float64(lPad + 2 + i*(lTab+lTabGap)))
		rc := Rect{x, ty, x + v.px(lTab), ty + v.px(lTab)}
		h.Tabs[i] = rc
		active := int(v.Metric) == i
		switch {
		case active:
			r.RoundRect(float64(rc.X0), float64(rc.Y0), float64(rc.X1), float64(rc.Y1), v.pf(8), 0x2C3445, 1)
			r.RoundRect(float64(rc.X0)+v.pf(6), float64(rc.Y1)-v.pf(3), float64(rc.X1)-v.pf(6), float64(rc.Y1)-v.pf(1), v.pf(1), ColAccent, 1)
		case v.HoverTab == i:
			r.RoundRect(float64(rc.X0), float64(rc.Y0), float64(rc.X1), float64(rc.Y1), v.pf(8), 0x232A38, 1)
		}
		col := uint32(0x8790A0)
		if active {
			col = ColAccent
		} else if v.HoverTab == i {
			col = ColDim
		}
		gs := v.px(21)
		r.Glyph(tabGlyph[i], rc.X0+(rc.X1-rc.X0-gs)/2, rc.Y0+(rc.Y1-rc.Y0-gs)/2-v.px(1), gs, col, 1)
	}

	// title + timer
	rowA := v.px(lRowA)
	tx := v.px(float64(lPad + 2 + 3*(lTab+lTabGap) + 6))
	title := "AetherMeter"
	if snap.TargetName != "" {
		title = snap.TargetName
	}
	if v.Banner != "" {
		title = v.Banner + " · " + title
	}
	right := ""
	if snap.EncounterID != 0 {
		right = combat.Clock(snap.Duration)
	}
	if v.Back > 0 {
		right = fmt.Sprintf("« %d   %s", v.Back, right)
	}
	rightW := v.px(78)
	if v.Back > 0 {
		rightW = v.px(104)
	}
	titleCol := uint32(ColText)
	if snap.IsBoss {
		titleCol = ColAccent
	}
	dot := v.px(7)
	s.Text(title, tx, 0, w-rightW-v.px(lPad), rowA, titleCol, FontTitle, Left)
	s.Text(right, w-rightW-v.px(lPad), 0, w-v.px(lPad)-dot-v.px(8), rowA, ColText, FontBold, Right)
	s.Flush()
	dotCol := uint32(ColFaint)
	if snap.Active {
		dotCol = ColGood
	}
	r.Circle(float64(w-v.px(lPad))-float64(dot)/2, float64(rowA)/2, float64(dot)/2, dotCol, 1)
	y := rowA

	// boss HP bar with full numbers
	if snap.TargetMaxHP > 0 {
		x0, x1 := v.pf(lPad), float64(w)-v.pf(lPad)
		bh := v.pf(lHp)
		r.RoundRect(x0, float64(y), x1, float64(y)+bh, v.pf(bh/v.Scale/2), ColHpTrack, 0.9)
		frac := clamp01(float64(snap.TargetHP) / float64(snap.TargetMaxHP))
		if frac > 0 {
			fw := (x1 - x0) * frac
			r.RoundRect(x0, float64(y), x0+fw, float64(y)+bh, v.pf(bh/v.Scale/2), ColHp, 1)
			r.RoundRect(x0+v.pf(2), float64(y)+v.pf(2), x0+fw-v.pf(2), float64(y)+v.pf(5), v.pf(1.5), ColHpHi, 0.35)
		}
		hpText := combat.Full(snap.TargetHP) + " / " + combat.Full(snap.TargetMaxHP) + "   ·   " + combat.Pct(frac, 1)
		if snap.TargetDead {
			hpText = "derrotado   ·   " + combat.Full(snap.TargetMaxHP)
		}
		s.Text(hpText, int(x0), y, int(x1), y+int(bh), ColText, FontSmallBold, Center)
		y += v.px(lHp + 2)
	}

	// info line
	rc := v.px(lRowC)
	left := ""
	if snap.EncounterID != 0 {
		total := map[combat.Metric]string{combat.MetricDamage: "dano", combat.MetricHeal: "cura", combat.MetricTaken: "recebido"}[v.Metric]
		left = fmt.Sprintf("%s da PT  %s   ·   %s %s", v.Metric.Label(), combat.Short(snap.PartyDPS), total, combat.Short(float64(snap.TotalDamage)))
	}
	leftCol := uint32(ColDim)
	if v.Toast != "" {
		left, leftCol = v.Toast, ColAccent
	}
	var tags []string
	if v.Recording {
		tags = append(tags, "REC")
	}
	if snap.Dungeon > 0 {
		tags = append(tags, "DG "+clockLong(snap.Dungeon))
	}
	if v.Metric == combat.MetricDamage && v.Mode == combat.ViewAll {
		tags = append(tags, "tudo")
	}
	if v.PartyOnly {
		switch {
		case snap.PartyKnown:
			tags = append(tags, "PT")
		case snap.SelfKnown:
			tags = append(tags, "solo")
		default:
			tags = append(tags, "PT?")
		}
	}
	tagText := strings.Join(tags, "  ·  ")
	tagW := v.px(float64(18 + 7*len([]rune(tagText))))
	s.Text(left, v.px(lPad+2), y, w-tagW-v.px(lPad), y+rc, leftCol, FontSmall, Left)
	tagCol := uint32(ColDim)
	if snap.Dungeon > 0 {
		tagCol = ColAether // running
		if snap.DungeonDone {
			tagCol = ColGood // stopped: boss down / left the dungeon
		}
	}
	s.Text(tagText, w-tagW-v.px(lPad), y, w-v.px(lPad+2), y+rc, tagCol, FontSmallBold, Right)
}

func clockLong(d time.Duration) string {
	t := int(d.Seconds())
	if t >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", t/3600, t/60%60, t%60)
	}
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}

// ---- body ------------------------------------------------------------------------

// PanelOpen reports whether the skills side panel should be shown.
func (v *View) PanelOpen() bool { return v.Detail != 0 && v.detailRow() != nil }

// PanelHeight in window pixels (0 when closed).
func (v *View) PanelHeight() int {
	r := v.detailRow()
	if v.Detail == 0 || r == nil {
		return 0
	}
	n := max(1, min(len(r.Skills), pMax))
	return v.px(float64(pPad*2 + pHead + pGap + 2 + n*(pSkill+pGap)))
}

// DrawPanel paints the skills side panel of the selected player.
func DrawPanel(s Surface, v *View) {
	r := s.Raster()
	r.Clear()
	r.RoundRect(0, 0, float64(r.W), float64(r.H), v.pf(12), ColHeader, aPanel)
	if row := v.detailRow(); row != nil {
		drawDetail(s, v, row)
	}
}

// DrawBody paints the player cards or the status message.
func DrawBody(s Surface, v *View, h *Hits) {
	r := s.Raster()
	w := r.W
	r.Clear()
	h.Rows = h.Rows[:0]

	rows := v.visibleRows()
	if len(rows) == 0 {
		r.RoundRect(0, v.pf(lTop), float64(w), float64(r.H), v.pf(lRadius), ColHeader, 0.78)
		s.Text(v.Status, v.px(lPad), v.px(lTop), w-v.px(lPad), r.H, v.StatusCol, FontReg, Center)
		return
	}
	var maxV int64
	for _, x := range v.Snap.Rows {
		maxV = max(maxV, x.Damage)
	}
	rank := map[int]int{}
	for i, x := range v.Snap.Rows {
		rank[x.ID] = i + 1
	}

	for i, row := range rows {
		y0 := v.pf(float64(lTop + i*(lCard+lGap)))
		y1 := y0 + v.pf(lCard)
		x0, x1 := v.pf(1), float64(w)-v.pf(1)
		rad := v.pf(lRadius)
		h.Rows = append(h.Rows, struct {
			Rect
			ID int
		}{Rect{int(x0), int(y0), int(x1), int(y1)}, row.ID})

		card, cardA := uint32(0x0C0F14), aCard
		if row.ID == v.Detail { // the player whose skills are open on the side
			r.RoundRect(x0-v.pf(1), y0-v.pf(1), x1+v.pf(1), y1+v.pf(1), rad+v.pf(1), ColAether, 0.95)
			cardA = 0.7
		}
		r.RoundRect(x0, y0, x1, y1, rad, card, cardA)
		frac := 0.0
		if maxV > 0 {
			frac = float64(row.Damage) / float64(maxV)
		}
		if frac > 0 {
			fw := (x1 - x0) * frac
			fill := Mix(row.Color, 0x000000, 0.35)
			r.RoundRect(x0, y0, x0+fw, y1, rad, fill, aFill)
			// glossy top highlight
			r.RoundRect(x0+v.pf(3), y0+v.pf(2), x0+fw-v.pf(3), y0+v.pf(lCard/2), rad-v.pf(3), 0xFFFFFF, 0.06)
		}
		if row.IsSelf {
			r.RoundRect(x0, y0, x0+v.pf(3), y1, v.pf(1.5), ColAccent, 1)
		}

		// class badge
		bs := v.pf(lBadge)
		bcx, bcy := x0+v.pf(7)+bs/2, (y0+y1)/2
		r.Circle(bcx, bcy, bs/2, row.Color, 1)
		r.Circle(bcx, bcy, bs/2-v.pf(1.5), Mix(row.Color, 0xFFFFFF, 0.12), 1)
		if g := glyph.ClassGlyph(row.ClassID); g != "" {
			gs := v.px(21)
			r.Glyph(g, int(bcx)-gs/2, int(bcy)-gs/2, gs, ColGlyphOn, 0.92)
		} else {
			s.Text("?", int(bcx-bs/2), int(y0), int(bcx+bs/2), int(y1), ColGlyphOn, FontBold, Center)
		}

		tx := int(x0 + v.pf(7) + bs + v.pf(9))
		mid := int((y0 + y1) / 2)
		rightX := int(x1 - v.pf(10))
		valW := v.px(84)
		name := fmt.Sprintf("%d. %s", rank[row.ID], row.Name)
		nameCol := uint32(ColText)
		if row.IsSelf {
			nameCol = ColAccent
		}
		s.Text(name, tx, int(y0)+v.px(2), rightX-valW, mid+v.px(1), nameCol, FontBold, Left)
		s.Text(combat.Short(row.DPS), rightX-valW, int(y0)+v.px(2), rightX, mid+v.px(1), ColText, FontBold, Right)

		detail := ""
		switch v.Metric {
		case combat.MetricDamage:
			detail = fmt.Sprintf("%s  ·  crit %s", combat.Short(float64(row.Damage)), combat.Pct(row.CritRate, 0))
		case combat.MetricHeal:
			detail = fmt.Sprintf("%s curado", combat.Short(float64(row.Damage)))
		case combat.MetricTaken:
			detail = fmt.Sprintf("%s  ·  maior %s", combat.Short(float64(row.Damage)), combat.Short(float64(row.MaxHit)))
			if row.Deaths > 0 {
				detail += fmt.Sprintf("  ·  %d morte(s)", row.Deaths)
			}
		}
		s.Text(detail, tx, mid-v.px(1), rightX-v.px(48), int(y1)-v.px(3), ColDim, FontSmall, Left)
		s.Text(combat.Pct(row.Pct, 1), rightX-v.px(56), mid-v.px(1), rightX, int(y1)-v.px(3), ColDim, FontSmallBold, Right)
		s.Flush()
	}
}

// compact side panel
const (
	pWidth = 290 // logical width of the skills panel
	pPad   = 6
	pHead  = 34
	pSkill = 21
	pGap   = 2
	pMax   = 10
	pBadge = 24
)

// PanelWidth in window pixels.
func (v *View) PanelWidth() int { return v.px(pWidth) }

func drawDetail(s Surface, v *View, row *combat.Row) {
	r := s.Raster()
	w := r.W
	x0, x1 := v.pf(pPad), float64(w)-v.pf(pPad)
	y0 := v.pf(pPad)
	y1 := y0 + v.pf(pHead)
	r.RoundRect(x0, y0, x1, y1, v.pf(9), Mix(row.Color, 0x000000, 0.45), 0.75)

	bs := v.pf(pBadge)
	bcx, bcy := x0+v.pf(5)+bs/2, (y0+y1)/2
	r.Circle(bcx, bcy, bs/2, row.Color, 1)
	if g := glyph.ClassGlyph(row.ClassID); g != "" {
		gs := v.px(16)
		r.Glyph(g, int(bcx)-gs/2, int(bcy)-gs/2, gs, ColGlyphOn, 0.92)
	}
	tx := int(x0 + v.pf(5) + bs + v.pf(7))
	mid := int((y0 + y1) / 2)
	s.Text(row.Name, tx, int(y0)+v.px(1), int(x1)-v.px(24), mid+v.px(1), ColText, FontSmallBold, Left)
	s.Text("×", int(x1)-v.px(22), int(y0), int(x1)-v.px(6), mid+v.px(2), ColDim, FontBold, Right)
	stats := ""
	switch v.Metric {
	case combat.MetricDamage:
		stats = fmt.Sprintf("crit %s  ·  costas %s  ·  maior %s", combat.Pct(row.CritRate, 0), combat.Pct(row.BackRate, 0), combat.Short(float64(row.MaxHit)))
	case combat.MetricHeal:
		stats = fmt.Sprintf("%s curado  ·  maior %s", combat.Short(float64(row.Damage)), combat.Short(float64(row.MaxHit)))
	case combat.MetricTaken:
		stats = fmt.Sprintf("maior %s  ·  mortes %d", combat.Short(float64(row.MaxHit)), row.Deaths)
	}
	s.Text(stats, tx, mid-v.px(1), int(x1)-v.px(6), int(y1)-v.px(2), ColDim, FontSmall, Left)
	s.Flush()

	skills := row.Skills
	if len(skills) > pMax {
		skills = skills[:pMax]
	}
	var maxV int64
	if len(skills) > 0 {
		maxV = skills[0].Damage
	}
	sy := y1 + v.pf(pGap+2)
	for i, sk := range skills {
		a0 := sy + float64(i)*v.pf(pSkill+pGap)
		a1 := a0 + v.pf(pSkill)
		r.RoundRect(x0, a0, x1, a1, v.pf(7), 0x0C0F14, 0.45)
		if maxV > 0 {
			fw := (x1 - x0) * float64(sk.Damage) / float64(maxV)
			r.RoundRect(x0, a0, x0+fw, a1, v.pf(7), Mix(row.Color, 0x000000, 0.4), 0.6)
		}
		name := sk.Name
		if sk.IsDot {
			name += " (DoT)"
		}
		if sk.IsPet {
			name = "› " + name
		}
		info := fmt.Sprintf("×%d", sk.Hits)
		if v.Metric == combat.MetricDamage {
			info = "c" + combat.Pct(sk.CritRate, 0)
		}
		rx := int(x1 - v.pf(7))
		s.Text(name, int(x0+v.pf(7)), int(a0), rx-v.px(126), int(a1), ColText, FontSmallBold, Left)
		s.Text(info, rx-v.px(126), int(a0), rx-v.px(84), int(a1), ColFaint, FontSmall, Right)
		s.Text(combat.Short(float64(sk.Damage)), rx-v.px(82), int(a0), rx-v.px(34), int(a1), ColText, FontSmallBold, Right)
		s.Text(combat.Pct(sk.Pct, 0), rx-v.px(32), int(a0), rx, int(a1), ColDim, FontSmall, Right)
		s.Flush()
	}
}
