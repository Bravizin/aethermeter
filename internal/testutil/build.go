// Package testutil builds synthetic game packets for tests and the demo mode.
package testutil

import (
	"encoding/binary"
)

func VarInt(v uint64) []byte {
	var out []byte
	for {
		b := byte(v & 0x7F)
		v >>= 7
		if v != 0 {
			out = append(out, b|0x80)
		} else {
			return append(out, b)
		}
	}
}

// Frame prepends the length varint so that size = value + varintLen - 4.
// With size = varintLen + len(body), the encoded value is always len(body)+4.
func Frame(body []byte) []byte {
	return append(VarInt(uint64(len(body)+4)), body...)
}

func u16(v uint16) []byte { b := make([]byte, 2); binary.LittleEndian.PutUint16(b, v); return b }
func u32(v uint32) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, v); return b }
func u64(v uint64) []byte { b := make([]byte, 8); binary.LittleEndian.PutUint64(b, v); return b }

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// Heartbeat returns the 11-byte keep-alive packet.
func Heartbeat() []byte {
	return Frame(cat([]byte{0x00, 0x36}, make([]byte, 8)))
}

type Hit struct {
	Target, Actor int
	Skill         uint32
	Damage        uint64
	Crit, Back    bool
}

// Damage builds an opcode 04 38 packet (switch value 5 => 3 flag bytes present).
func Damage(h Hit) []byte {
	dmgType := uint64(1)
	if h.Crit {
		dmgType = 3
	}
	dir := byte(0)
	if h.Back {
		dir = 1
	}
	body := cat(
		u16(0x3804),
		VarInt(uint64(h.Target)),
		VarInt(5),
		VarInt(0),
		VarInt(uint64(h.Actor)),
		u32(h.Skill),
		[]byte{0},
		VarInt(dmgType),
		[]byte{0, 0, dir},
		u32(0),
		make([]byte, 4),
		VarInt(0),
		VarInt(h.Damage),
	)
	return Frame(body)
}

// Dot builds an opcode 05 38 packet. skill is the base code (multiplied by 100 on the wire).
func Dot(target, actor int, skill uint32, dmg uint64) []byte {
	return Frame(cat(u16(0x3805), VarInt(uint64(target)), []byte{0x02}, VarInt(uint64(actor)), VarInt(0), u32(skill*100), VarInt(dmg)))
}

// OtherPlayer builds an opcode 45 36 packet with a name.
func OtherPlayer(id int, name string) []byte {
	return Frame(cat(u16(0x3645), VarInt(uint64(id)), make([]byte, 4), []byte{0x01}, VarInt(uint64(len(name))), []byte(name), VarInt(0), u16(1001)))
}

// SelfPlayer builds an opcode 33 36 packet.
func SelfPlayer(id int, name string) []byte {
	return Frame(cat(u16(0x3633), VarInt(uint64(id)), make([]byte, 4), []byte{0x01}, VarInt(uint64(len(name))), []byte(name), u16(1001), []byte{11}, make([]byte, 16), u64(50_000), u64(60_000)))
}

// Death builds an opcode 04 8D packet.
func Death(id int) []byte { return Frame(cat(u16(0x8D04), VarInt(uint64(id)))) }

// LZ4Literals produces a valid LZ4 block containing only literals.
func LZ4Literals(src []byte) []byte {
	n := len(src)
	var out []byte
	if n < 15 {
		out = append(out, byte(n<<4))
	} else {
		out = append(out, 0xF0)
		rem := n - 15
		for rem >= 255 {
			out = append(out, 255)
			rem -= 255
		}
		out = append(out, byte(rem))
	}
	return append(out, src...)
}

// Compressed wraps inner packets in an FF FF LZ4 container.
func Compressed(inner ...[]byte) []byte {
	payload := cat(inner...)
	return Frame(cat(u16(0xFFFF), u32(uint32(len(payload))), LZ4Literals(payload)))
}

func Cat(parts ...[]byte) []byte { return cat(parts...) }

// SkillCd builds an opcode 47 38 packet (your own skill cooldowns).
func SkillCd(skills ...uint32) []byte {
	body := cat(u16(0x3847), []byte{byte(len(skills))})
	for _, s := range skills {
		body = cat(body, u32(s), VarInt(8000))
	}
	return Frame(body)
}
