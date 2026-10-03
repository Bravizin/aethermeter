package glyph

import "testing"

func TestMasks(t *testing.T) {
	for _, n := range []string{"tab_dps", "tab_heal", "tab_tank", "class_10", "class_11", "class_12", "class_13", "class_14", "class_15", "class_16", "class_17", "class_18", "class_19"} {
		for _, sz := range []int{14, 20, 32} {
			m := Mask(n, sz)
			if len(m) != sz*sz {
				t.Fatalf("%s@%d: len %d", n, sz, len(m))
			}
			var on int
			for _, v := range m {
				if v > 128 {
					on++
				}
			}
			if on < sz*sz/20 || on > sz*sz*3/4 {
				t.Errorf("%s@%d looks wrong: %d/%d pixels on", n, sz, on, sz*sz)
			}
		}
	}
	if Mask("nope", 10) != nil {
		t.Fatal("unknown glyph should be nil")
	}
}
