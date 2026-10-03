package combat

import (
	"fmt"
	"strings"
	"time"
)

// Short formats big numbers compactly, Brazilian style: 950, 12,3k, 4,56M, 1,20B.
func Short(v float64) string {
	a := v
	if a < 0 {
		a = -a
	}
	var s string
	switch {
	case a >= 1e9:
		s = fmt.Sprintf("%.2fB", v/1e9)
	case a >= 1e6:
		s = fmt.Sprintf("%.2fM", v/1e6)
	case a >= 1e4:
		s = fmt.Sprintf("%.1fk", v/1e3)
	default:
		s = fmt.Sprintf("%.0f", v)
	}
	return strings.Replace(s, ".", ",", 1)
}

// Full writes an integer with Brazilian thousands separators: 1.950.000.
func Full(v int64) string {
	neg := v < 0
	if neg {
		v = -v
	}
	d := fmt.Sprintf("%d", v)
	var b strings.Builder
	for i, c := range d {
		if i > 0 && (len(d)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// Pct formats a 0..1 fraction as "61,2%".
func Pct(f float64, decimals int) string {
	return strings.Replace(fmt.Sprintf("%.*f%%", decimals, f*100), ".", ",", 1)
}

func Clock(d time.Duration) string {
	s := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// Summary renders a Discord-friendly text block of a snapshot.
func Summary(s Snapshot) string {
	var b strings.Builder
	title := s.TargetName
	if title == "" {
		title = "Encontro"
	}
	what := map[Metric]string{MetricDamage: "Dano", MetricHeal: "Cura", MetricTaken: "Dano recebido"}[s.Metric]
	fmt.Fprintf(&b, "**%s** — %s — %s — %s da PT: %s\n", title, Clock(s.Duration), what, s.Metric.Label(), Short(s.PartyDPS))
	b.WriteString("```\n")
	for i, r := range s.Rows {
		fmt.Fprintf(&b, "%2d. %-16s %-13s %9s %-4s %9s  %6s",
			i+1, trunc(r.Name, 16), trunc(r.ClassName, 13), Short(r.DPS), s.Metric.Label(), Short(float64(r.Damage)), Pct(r.Pct, 1))
		switch s.Metric {
		case MetricDamage:
			fmt.Fprintf(&b, "  crit %s", Pct(r.CritRate, 0))
		case MetricTaken:
			fmt.Fprintf(&b, "  maior %s", Short(float64(r.MaxHit)))
		}
		b.WriteString("\n")
	}
	b.WriteString("```")
	return b.String()
}

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}
