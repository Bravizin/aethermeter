package proto

import (
	"encoding/binary"

	"aethermeter/internal/lz4"
)

// Opcode reads the 16-bit opcode that follows the length varint.
func Opcode(pkt []byte) (uint16, bool) {
	_, n := VarInt(pkt, 0)
	if n <= 0 || len(pkt) < n+2 {
		return 0, false
	}
	return uint16(pkt[n]) | uint16(pkt[n+1])<<8, true
}

func isCompressed(pkt []byte) bool {
	op, ok := Opcode(pkt)
	return ok && op == OpCompressed
}

// Unpack expands a framed packet into the list of plain game packets it carries.
// Non-compressed packets are returned as-is; compressed containers (opcode FFFF)
// are LZ4-decoded recursively and split into their inner frames.
func Unpack(pkt []byte) [][]byte {
	if len(pkt) < 3 {
		return nil
	}
	if !isCompressed(pkt) {
		return [][]byte{pkt}
	}

	var out [][]byte
	type work struct{ buf []byte }
	stack := []work{{pkt}}
	depth := 0
	for len(stack) > 0 && depth < 64 {
		depth++
		w := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, fr := range scanFrames(w.buf) {
			if dec, ok := tryDecompress(w.buf, fr); ok {
				stack = append(stack, work{dec})
				continue
			}
			dataLen := fr.payloadLen - fr.varLen
			if dataLen <= 0 {
				continue
			}
			out = append(out, w.buf[fr.base:fr.base+fr.payloadLen])
		}
	}
	return out
}

type frameInfo struct{ base, payloadLen, varLen int }

func scanFrames(data []byte) []frameInfo {
	var frames []frameInfo
	pos := 0
	for pos < len(data) {
		if data[pos] == 0x00 { // padding between frames
			pos++
			continue
		}
		v, n := VarInt(data, pos)
		if n <= 0 || v > 2_000_000 {
			break
		}
		plen := v + n - 4
		if plen <= 0 {
			pos++
			continue
		}
		end := pos + plen
		if end > len(data) {
			break
		}
		frames = append(frames, frameInfo{pos, plen, n})
		pos = end
	}
	return frames
}

func tryDecompress(raw []byte, fr frameInfo) ([]byte, bool) {
	hdr := fr.varLen
	if hdr < fr.payloadLen {
		flag := raw[fr.base+hdr]
		if flag&0xF0 == 0xF0 && flag != 0xFF {
			hdr++
		}
	}
	if fr.payloadLen < hdr+6 {
		return nil, false
	}
	b := fr.base + hdr
	if raw[b] != 0xFF || raw[b+1] != 0xFF {
		return nil, false
	}
	size := int(binary.LittleEndian.Uint32(raw[b+2:]))
	if size <= 0 || size > 10_000_000 {
		return nil, false
	}
	comp := raw[fr.base+hdr+6 : fr.base+fr.payloadLen]
	if len(comp) == 0 {
		return nil, false
	}
	out := make([]byte, size)
	n, err := lz4.DecodeBlock(comp, out)
	if err != nil || n <= 0 {
		return nil, false
	}
	return out[:n], true
}
