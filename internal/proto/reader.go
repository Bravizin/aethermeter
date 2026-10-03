package proto

import (
	"encoding/binary"
	"errors"
)

var ErrShort = errors.New("proto: packet too short")

// VarInt reads a protobuf-style little-endian base-128 varint at off.
// Returns value and number of bytes consumed; n <= 0 means failure.
func VarInt(b []byte, off int) (val int, n int) {
	shift := 0
	for i := off; i < len(b); i++ {
		c := b[i]
		val |= int(c&0x7F) << shift
		n++
		if c&0x80 == 0 {
			return val, n
		}
		shift += 7
		if shift >= 32 {
			return -1, -1
		}
	}
	return -1, -1
}

// VarInt64 is VarInt for 64-bit values.
func VarInt64(b []byte, off int) (val int64, n int) {
	shift := 0
	for i := off; i < len(b); i++ {
		c := b[i]
		val |= int64(c&0x7F) << shift
		n++
		if c&0x80 == 0 {
			return val, n
		}
		shift += 7
		if shift >= 64 {
			return -1, -1
		}
	}
	return -1, -1
}

// Reader is a bounds-checked little-endian cursor. The first failed read sets
// Err and every subsequent read returns zero, so parsers can read a whole
// structure and check Err once at the end.
type Reader struct {
	b   []byte
	pos int
	Err error

	bitBuf  byte
	bitLeft int
}

func NewReader(b []byte) *Reader { return &Reader{b: b} }

func (r *Reader) Pos() int       { return r.pos }
func (r *Reader) Remaining() int { return len(r.b) - r.pos }

func (r *Reader) need(n int) bool {
	if r.Err != nil {
		return false
	}
	if n < 0 || r.pos+n > len(r.b) {
		r.Err = ErrShort
		return false
	}
	return true
}

func (r *Reader) U8() byte {
	if !r.need(1) {
		return 0
	}
	v := r.b[r.pos]
	r.pos++
	return v
}

func (r *Reader) U16() uint16 {
	if !r.need(2) {
		return 0
	}
	v := binary.LittleEndian.Uint16(r.b[r.pos:])
	r.pos += 2
	return v
}

func (r *Reader) U32() uint32 {
	if !r.need(4) {
		return 0
	}
	v := binary.LittleEndian.Uint32(r.b[r.pos:])
	r.pos += 4
	return v
}

func (r *Reader) U64() uint64 {
	if !r.need(8) {
		return 0
	}
	v := binary.LittleEndian.Uint64(r.b[r.pos:])
	r.pos += 8
	return v
}

func (r *Reader) VarInt() uint32 {
	var result uint32
	shift := 0
	for {
		if shift >= 35 {
			r.Err = errors.New("proto: varint too long")
			return 0
		}
		c := r.U8()
		if r.Err != nil {
			return 0
		}
		result |= uint32(c&0x7F) << shift
		shift += 7
		if c&0x80 == 0 {
			return result
		}
	}
}

func (r *Reader) Skip(n int) {
	if r.need(n) {
		r.pos += n
	}
}

func (r *Reader) Bytes(n int) []byte {
	if !r.need(n) {
		return nil
	}
	v := r.b[r.pos : r.pos+n]
	r.pos += n
	return v
}

// LPString reads [u8 len][utf8 bytes].
func (r *Reader) LPString() string {
	n := int(r.U8())
	return string(r.Bytes(n))
}

// Bit reads bits LSB-first from a byte buffer that is refilled on demand.
func (r *Reader) Bit() bool {
	if r.bitLeft == 0 {
		r.bitBuf = r.U8()
		r.bitLeft = 8
	}
	v := r.bitBuf&1 != 0
	r.bitBuf >>= 1
	r.bitLeft--
	return v
}
