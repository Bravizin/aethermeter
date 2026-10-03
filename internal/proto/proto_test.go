package proto_test

import (
	"bytes"
	"testing"

	"aethermeter/internal/gamedata"
	"aethermeter/internal/lz4"
	"aethermeter/internal/proto"
	tu "aethermeter/internal/testutil"
)

func TestLZ4Match(t *testing.T) {
	// "abcd" literal + match offset 4 len 8 => "abcdabcdabcd" + literals "xyz"
	src := []byte{0x44, 'a', 'b', 'c', 'd', 0x04, 0x00, 0x30, 'x', 'y', 'z'}
	dst := make([]byte, 64)
	n, err := lz4.DecodeBlock(src, dst)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(dst[:n]); got != "abcdabcdabcdxyz" {
		t.Fatalf("got %q", got)
	}
	if _, err := lz4.DecodeBlock([]byte{0x14, 'a', 0x09, 0x00}, dst); err == nil {
		t.Fatal("expected error for bad offset")
	}
}

func TestFramerSyncAndSplit(t *testing.T) {
	hit := tu.Damage(tu.Hit{Target: 500, Actor: 42, Skill: 11010000, Damage: 12345, Crit: true})
	stream := tu.Cat([]byte{0x99, 0x01, 0x02}, tu.Heartbeat(), hit, tu.Heartbeat(), hit)

	var got [][]byte
	var f proto.Framer
	for i := 0; i < len(stream); i += 3 { // feed in tiny chunks
		end := i + 3
		if end > len(stream) {
			end = len(stream)
		}
		f.Push(stream[i:end], func(p []byte) { got = append(got, append([]byte(nil), p...)) })
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 packets (heartbeats filtered), got %d", len(got))
	}
	if !bytes.Equal(got[0], hit) {
		t.Fatalf("packet mismatch\n%x\n%x", got[0], hit)
	}
}

func TestFramerResyncAfterGarbage(t *testing.T) {
	hit := tu.Damage(tu.Hit{Target: 1, Actor: 2, Skill: 11010000, Damage: 7})
	var f proto.Framer
	n := 0
	f.Push(tu.Cat(tu.Heartbeat(), hit), func([]byte) { n++ })
	f.Reset()
	// garbage that would decode to an absurd length, then a heartbeat
	f.Push(tu.Cat([]byte{0xFF, 0xFF, 0xFF, 0x7F, 0x00}, tu.Heartbeat(), hit), func([]byte) { n++ })
	if n != 2 {
		t.Fatalf("expected 2, got %d", n)
	}
}

func decodeAll(pkt []byte) []proto.Event {
	var out []proto.Event
	for _, p := range proto.Unpack(pkt) {
		out = append(out, proto.Decode(p, gamedata.Classify)...)
	}
	return out
}

func TestDecodeDamage(t *testing.T) {
	evs := decodeAll(tu.Damage(tu.Hit{Target: 900, Actor: 77, Skill: 11010000, Damage: 987654, Crit: true, Back: true}))
	if len(evs) != 1 {
		t.Fatalf("got %d events", len(evs))
	}
	d := evs[0].(proto.DamageEvent)
	if d.TargetID != 900 || d.ActorID != 77 || d.SkillCode != 11010000 || d.Damage != 987654 || !d.Crit || !d.Back {
		t.Fatalf("bad decode: %+v", d)
	}
}

func TestDecodeNpcAndGarbageSkills(t *testing.T) {
	evs := decodeAll(tu.Damage(tu.Hit{Target: 1, Actor: 2, Skill: 2_000_123, Damage: 5}))
	if len(evs) != 1 || !evs[0].(proto.DamageEvent).NPC {
		t.Fatalf("npc skill should decode as NPC damage, got %+v", evs)
	}
	if evs := decodeAll(tu.Damage(tu.Hit{Target: 1, Actor: 2, Skill: 250_000_000, Damage: 5})); len(evs) != 0 {
		t.Fatalf("garbage skill should be dropped, got %+v", evs)
	}
}

func TestDecodeCompressedContainer(t *testing.T) {
	a := tu.Damage(tu.Hit{Target: 10, Actor: 20, Skill: 11010000, Damage: 100})
	b := tu.Dot(10, 21, 11010000, 50)
	c := tu.OtherPlayer(20, "Sukuna")
	evs := decodeAll(tu.Compressed(a, b, c))
	if len(evs) != 3 {
		t.Fatalf("expected 3 events, got %d: %+v", len(evs), evs)
	}
	var names, dmg int
	for _, e := range evs {
		switch v := e.(type) {
		case proto.PlayerInfoEvent:
			if v.Name != "Sukuna" || v.EntityID != 20 || v.ServerID != 1001 {
				t.Fatalf("bad player: %+v", v)
			}
			names++
		case proto.DamageEvent:
			dmg++
		}
	}
	if names != 1 || dmg != 2 {
		t.Fatalf("names=%d dmg=%d", names, dmg)
	}
}

func TestDecodeSelfInfo(t *testing.T) {
	evs := decodeAll(tu.SelfPlayer(5, "Leo"))
	if len(evs) != 1 {
		t.Fatalf("got %d", len(evs))
	}
	p := evs[0].(proto.PlayerInfoEvent)
	if !p.IsSelf || p.Name != "Leo" || p.CombatPower != 50_000 || p.ServerID != 1001 {
		t.Fatalf("bad self info %+v", p)
	}
}

func TestDecodeNeverPanics(t *testing.T) {
	base := tu.Compressed(tu.Damage(tu.Hit{Target: 10, Actor: 20, Skill: 11010000, Damage: 100}), tu.OtherPlayer(1, "Abc"))
	for i := 0; i < len(base); i++ {
		for _, v := range []byte{0x00, 0xFF, 0x80, 0x7F} {
			mut := append([]byte(nil), base...)
			mut[i] = v
			decodeAll(mut)
			decodeAll(mut[:i])
		}
	}
}

func TestDecodeSkillCd(t *testing.T) {
	evs := decodeAll(tu.SkillCd(11010000, 11020000))
	if len(evs) != 1 {
		t.Fatalf("got %+v", evs)
	}
	if sk := evs[0].(proto.SkillCdEvent).Skills; len(sk) != 2 || sk[1] != 11020000 {
		t.Fatalf("skills %+v", sk)
	}
}
