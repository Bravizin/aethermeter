package netcap

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

func ethIPv4TCP(srcPort, dstPort uint16, seq uint32, payload []byte) []byte {
	eth := make([]byte, 14)
	binary.BigEndian.PutUint16(eth[12:], 0x0800)
	ip := make([]byte, 20)
	ip[0] = 0x45
	binary.BigEndian.PutUint16(ip[2:], uint16(20+20+len(payload)))
	ip[9] = 6
	copy(ip[12:], []byte{10, 0, 0, 1})
	copy(ip[16:], []byte{192, 168, 0, 10})
	tcp := make([]byte, 20)
	binary.BigEndian.PutUint16(tcp[0:], srcPort)
	binary.BigEndian.PutUint16(tcp[2:], dstPort)
	binary.BigEndian.PutUint32(tcp[4:], seq)
	tcp[12] = 5 << 4
	tcp[13] = 0x18
	out := append(append(append(eth, ip...), tcp...), payload...)
	return append(out, 0, 0, 0) // ethernet padding must be trimmed
}

func TestParseFrame(t *testing.T) {
	seg, ok := ParseFrame(DLTEthernet, ethIPv4TCP(7777, 50000, 1000, []byte("hello")))
	if !ok || seg.SrcPort != 7777 || seg.DstPort != 50000 || seg.Seq != 1000 || string(seg.Payload) != "hello" {
		t.Fatalf("bad parse %+v ok=%v", seg, ok)
	}
	if seg.SrcIP != "10.0.0.1" {
		t.Fatalf("src ip %s", seg.SrcIP)
	}
	// loopback (DLT_NULL) carries a 4-byte family header instead of Ethernet
	f := ethIPv4TCP(1, 2, 3, []byte("x"))
	lo := append([]byte{2, 0, 0, 0}, f[14:]...)
	if seg, ok := ParseFrame(DLTNull, lo); !ok || string(seg.Payload) != "x" {
		t.Fatalf("loopback parse failed")
	}
}

func TestStreamReorderAndDup(t *testing.T) {
	var out bytes.Buffer
	gaps := 0
	s := NewStream(func(b []byte) { out.Write(b) }, func() { gaps++ })
	now := time.Now()
	s.Push(Segment{Seq: 100, Payload: []byte("AAAA")}, now)
	s.Push(Segment{Seq: 108, Payload: []byte("CCCC")}, now) // out of order
	s.Push(Segment{Seq: 104, Payload: []byte("BBBB")}, now)
	s.Push(Segment{Seq: 102, Payload: []byte("AABB")}, now) // overlapping retransmit
	s.Push(Segment{Seq: 112, Payload: []byte("DD")}, now)
	if out.String() != "AAAABBBBCCCCDD" || gaps != 0 {
		t.Fatalf("got %q gaps=%d", out.String(), gaps)
	}
}

func TestStreamGapTimeout(t *testing.T) {
	var out bytes.Buffer
	gaps := 0
	s := NewStream(func(b []byte) { out.Write(b) }, func() { gaps++ })
	now := time.Now()
	s.Push(Segment{Seq: 0, Payload: []byte("AA")}, now)
	s.Push(Segment{Seq: 10, Payload: []byte("ZZ")}, now) // bytes 2..9 never arrive
	if out.String() != "AA" {
		t.Fatalf("emitted too early: %q", out.String())
	}
	s.Tick(now.Add(time.Second))
	if out.String() != "AAZZ" || gaps != 1 {
		t.Fatalf("got %q gaps=%d", out.String(), gaps)
	}
}

func TestSeqWrap(t *testing.T) {
	var out bytes.Buffer
	s := NewStream(func(b []byte) { out.Write(b) }, nil)
	now := time.Now()
	s.Push(Segment{Seq: 0xFFFFFFFE, Payload: []byte("ab")}, now)
	s.Push(Segment{Seq: 0, Payload: []byte("cd")}, now)
	if out.String() != "abcd" {
		t.Fatalf("wrap failed: %q", out.String())
	}
}
