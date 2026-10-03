package proto

import "bytes"

// Heartbeat is the byte pattern of the periodic keep-alive packet the server
// sends (len varint 0x0E, opcode 00 36). It is used both to detect which TCP
// connection is the game connection and to (re)synchronise the framer.
var Heartbeat = []byte{0x0E, 0x00, 0x36}

const (
	maxPacketSize  = 40 * 1024
	maxBufferSize  = 10 * 1024 * 1024
	heartbeatBytes = 11
)

// PacketSize returns the full on-wire size of the packet that starts at b[0],
// derived from its leading varint (size = value + varintLen - 4).
func PacketSize(b []byte, off int) (size int, varLen int, ok bool) {
	v, n := VarInt(b, off)
	if n <= 0 {
		return 0, 0, false
	}
	return v + n - 4, n, true
}

// Framer turns the server->client TCP byte stream into individual game packets.
type Framer struct {
	buf    []byte
	synced bool

	Resyncs int // diagnostics
}

// Reset drops buffered bytes and forces a resynchronisation.
func (f *Framer) Reset() {
	f.buf = f.buf[:0]
	f.synced = false
}

// Push appends stream bytes and calls emit for each complete packet.
// The slice passed to emit is only valid during the callback.
func (f *Framer) Push(data []byte, emit func(pkt []byte)) {
	if len(f.buf)+len(data) > maxBufferSize {
		f.Reset()
	}
	f.buf = append(f.buf, data...)

	// walk with a read offset and compact once at the end (avoids O(n²) copying on bursts)
	off := 0
	defer func() { f.compact(off) }()
	for off < len(f.buf) {
		b := f.buf[off:]
		if !f.synced {
			i := bytes.Index(b, Heartbeat)
			if i < 0 {
				// keep the last 2 bytes in case the pattern is split
				if len(b) > 2 {
					off += len(b) - 2
				}
				return
			}
			off += i
			f.synced = true
			f.Resyncs++
			continue
		}

		size, _, ok := PacketSize(b, 0)
		if !ok {
			if len(b) >= 5 { // a 5-byte varint that still doesn't end is garbage
				f.synced = false
				off++
				continue
			}
			return // need more bytes for the varint
		}
		if size <= 0 || size > maxPacketSize {
			f.synced = false
			off++
			continue
		}
		if len(b) < size {
			return
		}
		pkt := b[:size]
		if !(size == heartbeatBytes && bytes.HasPrefix(pkt, Heartbeat)) {
			emit(pkt)
		}
		off += size
	}
}

func (f *Framer) compact(n int) {
	if n <= 0 {
		return
	}
	if n >= len(f.buf) {
		f.buf = f.buf[:0]
		return
	}
	copy(f.buf, f.buf[n:])
	f.buf = f.buf[:len(f.buf)-n]
}
