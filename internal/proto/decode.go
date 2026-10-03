package proto

import (
	"bytes"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Opcodes (little-endian u16 read right after the length varint).
const (
	OpServerTime   uint16 = 0x3603
	OpDamage       uint16 = 0x3804
	OpDotDamage    uint16 = 0x3805
	OpCompressed   uint16 = 0xFFFF
	OpRemainHp     uint16 = 0x8D00
	OpMobSummon    uint16 = 0x3641
	OpPlayerInfo   uint16 = 0x3633
	OpOtherPlayers uint16 = 0x3645
	OpGlobalLink   uint16 = 0x3620
	OpPartyInfo    uint16 = 0x9702
	OpEntityDeath  uint16 = 0x8D04
	OpSkillCd      uint16 = 0x3847 // cooldowns of *your own* skills
)

// ---- Events ---------------------------------------------------------------

type Event interface{ isEvent() }

// DamageEvent is a direct hit (opcode 04 38) or a DoT tick (05 38).
type DamageEvent struct {
	ActorID, TargetID int
	SkillCode         int // raw code from the packet (DoT already divided by 100)
	Damage            int64
	DamageType        int
	Crit, Back, Front bool
	Parry, Perfect    bool
	Double, Dot       bool
	NPC               bool // dealt by a monster (skill in the NPC range)
}

// PlayerInfoEvent: a player entity got a name (self via 33 36, others via 45 36).
type PlayerInfoEvent struct {
	EntityID    int
	Name        string
	ServerID    int
	CombatPower int
	IsSelf      bool
}

// GlobalLinkEvent links a session entity id to a global (character db) id.
type GlobalLinkEvent struct{ GlobalID, SessionID int }

type PartyMember struct {
	GlobalID    int
	ServerID    int
	Name        string
	Level       int
	GearScore   int
	CombatPower int64
}

type PartyEvent struct {
	Name      string
	DungeonID uint32
	Members   []PartyMember
}

type SummonEvent struct{ SummonID, OwnerID int }

type MobInfoEvent struct {
	EntityID int
	MobCode  int
	MaxHP    int64
}

type HpEvent struct {
	EntityID int
	HP       int64
}

type DeathEvent struct{ EntityID int }

// SkillCdEvent lists skills that just went on cooldown for the local player.
// Only your own client gets these, which is how we recognise "you" when the
// server never sent your name.
type SkillCdEvent struct{ Skills []int }

func (DamageEvent) isEvent()     {}
func (PlayerInfoEvent) isEvent() {}
func (GlobalLinkEvent) isEvent() {}
func (PartyEvent) isEvent()      {}
func (SummonEvent) isEvent()     {}
func (MobInfoEvent) isEvent()    {}
func (HpEvent) isEvent()         {}
func (DeathEvent) isEvent()      {}
func (SkillCdEvent) isEvent()    {}

// SkillKind classifies a raw skill code found in a damage packet.
type SkillKind int

const (
	SkillInvalid SkillKind = iota // garbage / mis-parse: drop the packet
	SkillPlayer                   // player, pet or theostone skill
	SkillNPC                      // monster skill (used for "damage taken")
)

// SkillFilter classifies raw skill codes. It is injected so that the decoder
// stays independent of game data. nil accepts everything as a player skill.
type SkillFilter func(code int) SkillKind

func classify(f SkillFilter, code int) SkillKind {
	if f == nil {
		return SkillPlayer
	}
	return f(code)
}

// Decode parses one plain game packet into zero or more events.
// It never panics on malformed input.
func Decode(pkt []byte, plausible SkillFilter) (evs []Event) {
	defer func() {
		if r := recover(); r != nil {
			evs = nil
		}
	}()
	op, ok := Opcode(pkt)
	if !ok {
		return nil
	}
	switch op {
	case OpDamage:
		if e, ok := decodeDamage(pkt, plausible); ok {
			return []Event{e}
		}
	case OpDotDamage:
		if e, ok := decodeDot(pkt, plausible); ok {
			return []Event{e}
		}
	case OpPlayerInfo:
		if e, ok := decodeSelfInfo(pkt); ok {
			return []Event{e}
		}
	case OpOtherPlayers:
		if e, ok := decodeOtherInfo(pkt); ok {
			return []Event{e}
		}
	case OpGlobalLink:
		r := NewReader(pkt)
		r.VarInt()
		r.U16()
		r.Skip(2)
		sid := int(r.VarInt())
		r.Skip(4)
		gid := int(int32(r.U32()))
		if r.Err == nil && sid > 0 {
			return []Event{GlobalLinkEvent{GlobalID: gid, SessionID: sid}}
		}
	case OpPartyInfo:
		if e, ok := decodeParty(pkt); ok {
			return []Event{e}
		}
	case OpMobSummon:
		if e, ok := decodeSummon(pkt); ok {
			evs = append(evs, e)
		}
		if e, ok := decodeMobInfo(pkt); ok {
			evs = append(evs, e)
		}
		return evs
	case OpRemainHp:
		_, n := VarInt(pkt, 0)
		off := n + 2
		id, l := VarInt(pkt, off)
		if l <= 0 {
			return nil
		}
		off += l
		for i := 0; i < 3; i++ {
			_, l = VarInt(pkt, off)
			if l <= 0 {
				return nil
			}
			off += l
		}
		if off+8 > len(pkt) {
			return nil
		}
		hp := int64(le64(pkt, off))
		return []Event{HpEvent{EntityID: id, HP: hp}}
	case OpSkillCd:
		r := NewReader(pkt)
		r.VarInt()
		r.U16()
		n := int(r.U8())
		var ev SkillCdEvent
		for i := 0; i < n && r.Err == nil; i++ {
			sk := int(r.U32())
			r.VarInt() // ms left
			if r.Err == nil && sk > 0 {
				ev.Skills = append(ev.Skills, sk)
			}
		}
		if len(ev.Skills) > 0 {
			return []Event{ev}
		}
	case OpEntityDeath:
		r := NewReader(pkt)
		r.VarInt()
		r.U16()
		id := int(r.VarInt())
		if r.Err == nil {
			return []Event{DeathEvent{EntityID: id}}
		}
	}
	return nil
}

func le64(b []byte, off int) uint64 {
	var v uint64
	for i := 0; i < 8; i++ {
		v |= uint64(b[off+i]) << (8 * i)
	}
	return v
}

// ---- damage ---------------------------------------------------------------

const critDamageType = 3

func decodeDamage(pkt []byte, plausible SkillFilter) (DamageEvent, bool) {
	r := NewReader(pkt)
	r.VarInt()
	r.U16()

	target := int(r.VarInt())
	sw := int(r.VarInt())
	if sw > 255 {
		return DamageEvent{}, false
	}
	sw &= 0x0F
	if sw < 4 || sw > 7 {
		return DamageEvent{}, false
	}
	r.VarInt() // unknown flag
	actor := int(r.VarInt())
	if r.Err != nil {
		return DamageEvent{}, false
	}
	skill := int(r.U32())
	kind := classify(plausible, skill)
	if kind == SkillInvalid {
		return DamageEvent{}, false
	}
	r.U8()
	dmgType := int(r.VarInt())

	var flagByte, dir byte
	if sw == 4 {
		if r.Remaining() < 8 {
			return DamageEvent{}, false
		}
	} else {
		if r.Remaining() < 12 {
			return DamageEvent{}, false
		}
		flagByte = r.U8()
		r.U8()
		dir = r.U8()
	}
	r.U32()
	r.Skip(4)
	r.VarInt() // unknown
	dmg := int64(r.VarInt())
	if r.Err != nil {
		return DamageEvent{}, false
	}
	return DamageEvent{
		ActorID: actor, TargetID: target, SkillCode: skill, Damage: dmg, DamageType: dmgType,
		Crit:    dmgType == critDamageType,
		Back:    dir&0x01 != 0,
		Front:   dir&0x02 != 0,
		Parry:   flagByte&0x02 != 0,
		Perfect: flagByte&0x04 != 0,
		Double:  flagByte&0x08 != 0,
		NPC:     kind == SkillNPC,
	}, true
}

func decodeDot(pkt []byte, plausible SkillFilter) (DamageEvent, bool) {
	r := NewReader(pkt)
	r.VarInt()
	r.U16()
	target := int(r.VarInt())
	effect := r.U8()
	if effect&0x02 == 0 {
		return DamageEvent{}, false
	}
	actor := int(r.VarInt())
	r.VarInt()
	skill := int(int32(r.U32())) / 100
	dmg := int64(r.VarInt())
	if r.Err != nil {
		return DamageEvent{}, false
	}
	kind := classify(plausible, skill)
	if kind == SkillInvalid {
		return DamageEvent{}, false
	}
	return DamageEvent{ActorID: actor, TargetID: target, SkillCode: skill, Damage: dmg, Dot: true, NPC: kind == SkillNPC}, true
}

// ---- players --------------------------------------------------------------

const maxNameLen = 72

func readPlayerName(data []byte, start int) (string, int, bool) {
	start += 4
	if start >= len(data) || data[start]&0x01 == 0 {
		return "", 0, false
	}
	pos := start + 1
	n, l := VarInt(data, pos)
	if l <= 0 || n < 1 || n > maxNameLen {
		return "", 0, false
	}
	pos += l
	if pos+n > len(data) {
		return "", 0, false
	}
	name := decodeGameString(data[pos : pos+n])
	if name == "" || isAllDigits(name) {
		return "", 0, false
	}
	return name, pos + n, true
}

// decodeGameString handles the game's small back-reference scheme
// (bytes < 32 repeat that many bytes from the start of the output) and strips
// anything that isn't a letter or digit.
func decodeGameString(b []byte) string {
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		if c == 0 {
			break
		}
		if c < 32 {
			n := int(c)
			if n > len(out) {
				n = len(out)
			}
			out = append(out, out[:n]...)
			continue
		}
		out = append(out, c)
	}
	var sb strings.Builder
	for len(out) > 0 {
		r, size := utf8.DecodeRune(out)
		out = out[size:]
		if r != utf8.RuneError && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func decodeSelfInfo(pkt []byte) (PlayerInfoEvent, bool) {
	_, n := VarInt(pkt, 0)
	pos := n + 2
	id, l := VarInt(pkt, pos)
	if l <= 0 || id < 1 {
		return PlayerInfoEvent{}, false
	}
	pos += l
	name, after, ok := readPlayerName(pkt, pos)
	if !ok {
		return PlayerInfoEvent{}, false
	}
	server := -1
	if after+2 <= len(pkt) {
		server = int(pkt[after]) | int(pkt[after+1])<<8
	}
	return PlayerInfoEvent{EntityID: id, Name: name, ServerID: server, CombatPower: scanCombatPower(pkt), IsSelf: true}, true
}

func decodeOtherInfo(pkt []byte) (PlayerInfoEvent, bool) {
	_, n := VarInt(pkt, 0)
	pos := n + 2
	id, l := VarInt(pkt, pos)
	if l <= 0 || id < 1 {
		return PlayerInfoEvent{}, false
	}
	pos += l
	name, after, ok := readPlayerName(pkt, pos)
	if !ok {
		return PlayerInfoEvent{}, false
	}
	if v, l := VarInt(pkt, after); l > 0 && v >= 1 {
		after += l
	}
	server := -1
	for i := after; i+1 < len(pkt); i++ {
		c := int(pkt[i]) | int(pkt[i+1])<<8
		if KnownServer(c) {
			server = c
			break
		}
	}
	return PlayerInfoEvent{EntityID: id, Name: name, ServerID: server}, true
}

// scanCombatPower looks for two adjacent u64 values (current, highest) near the
// end of the self-info packet that both look like a combat-power value.
func scanCombatPower(data []byte) int {
	const pair = 16
	if len(data) < pair {
		return 0
	}
	start := len(data) - pair
	min := start - 255
	if min < 0 {
		min = 0
	}
	for off := start; off >= min; off-- {
		cur := le64(data, off)
		hi := le64(data, off+8)
		if cur >= 10_000 && cur <= 2_000_000 && hi >= 10_000 && hi <= 2_000_000 && cur <= hi {
			return int(cur)
		}
	}
	return 0
}

func decodeParty(pkt []byte) (PartyEvent, bool) {
	r := NewReader(pkt)
	r.VarInt()
	r.U16()
	r.U32() // party key
	name := r.LPString()
	r.U8() // size
	dungeon := r.U32()
	r.U8()
	r.U8()
	r.U64() // leader dbid
	r.Bit()
	r.U8()
	r.U8()
	count := int(r.VarInt())
	if r.Err != nil || count > 64 {
		return PartyEvent{}, false
	}
	ev := PartyEvent{Name: name, DungeonID: dungeon}
	for i := 0; i < count; i++ {
		mask := r.U8()
		r.U8() // slot
		dbid := r.U64()
		nick := r.LPString()
		r.U32()
		level := r.U32()
		if mask&0x01 != 0 {
			r.U32()
		}
		gear := r.U32()
		if mask&0x02 != 0 {
			r.Bit()
		}
		r.Bit()
		if mask&0x04 != 0 {
			r.U16()
		}
		if mask&0x08 != 0 {
			r.U16()
		}
		r.U8()
		cp := r.U64()
		tickets := int(r.VarInt())
		if tickets > 256 {
			r.Err = ErrShort
		}
		for j := 0; j < tickets && r.Err == nil; j++ {
			r.U8()
			r.U32()
		}
		if mask&0x10 != 0 {
			r.U64()
		}
		r.U8()
		r.U8()
		if r.Err != nil {
			break
		}
		if mask != 0 {
			ev.Members = append(ev.Members, PartyMember{
				GlobalID:    int(int32(uint32(dbid & 0xFFFFFFFF))),
				ServerID:    int((dbid >> 48) & 0xFFFF),
				Name:        nick,
				Level:       int(level),
				GearScore:   int(gear),
				CombatPower: int64(cp),
			})
		}
	}
	// an empty member list (cleanly parsed) means we left the party
	return ev, len(ev.Members) > 0 || r.Err == nil
}

// ---- mobs / summons ---------------------------------------------------------

var (
	summonBoundary = []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
	summonHdr1     = []byte{0x07, 0x02, 0x06}
	summonHdr2     = []byte{0x07, 0x02, 0x01}
	summonHdrB     = []byte{0x07, 0x02}
)

func decodeSummon(pkt []byte) (SummonEvent, bool) {
	_, n := VarInt(pkt, 0)
	pos := n + 2
	if pos >= len(pkt) {
		return SummonEvent{}, false
	}
	pet, l := VarInt(pkt, pos)
	if l <= 0 {
		return SummonEvent{}, false
	}
	span := pkt[pos:]
	bi := bytes.Index(span, summonBoundary)
	if bi < 0 {
		return SummonEvent{}, false
	}
	after := bi + len(summonBoundary)
	if after >= len(span) {
		return SummonEvent{}, false
	}
	rest := span[after:]
	hi := bytes.Index(rest, summonHdr1)
	if hi < 0 {
		hi = bytes.Index(rest, summonHdr2)
	}
	if hi < 0 {
		hi = bytes.Index(rest, summonHdrB) + 1 // mirrors reference behaviour (-1 -> 0)
	}
	ao := after + hi
	if ao+5 > len(span) {
		return SummonEvent{}, false
	}
	owner := int(span[ao+3]) | int(span[ao+4])<<8
	if owner <= 1 {
		return SummonEvent{}, false
	}
	return SummonEvent{SummonID: pet, OwnerID: owner}, true
}

func decodeMobInfo(pkt []byte) (MobInfoEvent, bool) {
	_, n := VarInt(pkt, 0)
	off := n + 2
	end := len(pkt)
	if off >= end {
		return MobInfoEvent{}, false
	}
	mobID, l := VarInt(pkt, off)
	if l <= 0 {
		return MobInfoEvent{}, false
	}
	length := end - off
	// scan for the "00 ?? 02" marker that follows the 24-bit mob code
	limit := off + 60
	if limit > end-2 {
		limit = end - 2
	}
	marker := -1
	for i := off; i < limit; i++ {
		num := off + i + 2
		if num < end && num >= off+2 && pkt[num-2] == 0 && pkt[num-1]&0xBF == 0 && pkt[num] == 2 {
			marker = i + 2
			break
		}
	}
	if marker < 0 {
		return MobInfoEvent{}, false
	}
	codeRel := marker - 2
	if off > codeRel-3 {
		return MobInfoEvent{}, false
	}
	codeAbs := off + codeRel - 3
	if codeAbs < off || codeAbs+3 > end {
		return MobInfoEvent{}, false
	}
	code := int(pkt[codeAbs]) | int(pkt[codeAbs+1])<<8 | int(pkt[codeAbs+2])<<16

	hpFrom := codeRel + 3
	hpLimit := codeRel + 64
	if hpLimit > length-2 {
		hpLimit = length - 2
	}
	for rel := hpFrom; rel < hpLimit; rel++ {
		abs := off + rel
		if abs >= end {
			break
		}
		if pkt[abs] != 1 {
			continue
		}
		p := abs + 1
		if p >= end {
			break
		}
		maxHP, ml := VarInt64(pkt, p)
		if ml <= 0 || maxHP <= 0 {
			continue
		}
		cur, cl := VarInt64(pkt, p) // reference implementation reads the same offset twice
		if cl <= 0 {
			continue
		}
		if cur >= maxHP {
			return MobInfoEvent{EntityID: mobID, MobCode: code, MaxHP: maxHP}, true
		}
		break
	}
	return MobInfoEvent{}, false
}
