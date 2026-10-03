package config

import "testing"

func TestParseHotkey(t *testing.T) {
	cases := map[string][2]uint32{
		"Ctrl+Alt+L":   {ModCtrl | ModAlt, 'L'},
		"ctrl + alt+1": {ModCtrl | ModAlt, '1'},
		"Alt+F9":       {ModAlt, 0x78},
		"Ctrl+Num5":    {ModCtrl, 0x65},
		"Pause":        {0, 0x13},
	}
	for in, want := range cases {
		m, vk, err := ParseHotkey(in)
		if err != nil || m != want[0] || vk != want[1] {
			t.Errorf("%q -> %x %x %v, want %x %x", in, m, vk, err, want[0], want[1])
		}
	}
	for _, bad := range []string{"", "Ctrl+", "Ctrl+Banana", "L+Ctrl"} {
		if _, _, err := ParseHotkey(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}
