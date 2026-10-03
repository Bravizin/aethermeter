// Package lz4 implements a minimal LZ4 *block* decoder (no frame format),
// which is what the Aion 2 server uses for its compressed packet containers.
package lz4

import "errors"

var ErrCorrupt = errors.New("lz4: corrupt input")

// DecodeBlock decompresses src into dst and returns the number of bytes written.
func DecodeBlock(src, dst []byte) (int, error) {
	si, di := 0, 0
	for si < len(src) {
		token := src[si]
		si++

		// literals
		litLen := int(token >> 4)
		if litLen == 15 {
			for {
				if si >= len(src) {
					return 0, ErrCorrupt
				}
				b := src[si]
				si++
				litLen += int(b)
				if b != 255 {
					break
				}
			}
		}
		if si+litLen > len(src) || di+litLen > len(dst) {
			return 0, ErrCorrupt
		}
		copy(dst[di:], src[si:si+litLen])
		si += litLen
		di += litLen

		if si >= len(src) {
			break // last sequence has only literals
		}

		// match
		if si+2 > len(src) {
			return 0, ErrCorrupt
		}
		offset := int(src[si]) | int(src[si+1])<<8
		si += 2
		if offset == 0 || offset > di {
			return 0, ErrCorrupt
		}
		matchLen := int(token & 0x0F)
		if matchLen == 15 {
			for {
				if si >= len(src) {
					return 0, ErrCorrupt
				}
				b := src[si]
				si++
				matchLen += int(b)
				if b != 255 {
					break
				}
			}
		}
		matchLen += 4
		if di+matchLen > len(dst) {
			return 0, ErrCorrupt
		}
		// byte-by-byte copy: matches may overlap
		start := di - offset
		for i := 0; i < matchLen; i++ {
			dst[di+i] = dst[start+i]
		}
		di += matchLen
	}
	return di, nil
}
