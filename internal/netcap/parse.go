// Package netcap captures the game's TCP stream (Npcap on Windows) and
// reassembles it into an ordered byte stream for the protocol framer.
package netcap

import (
	"encoding/binary"
	"fmt"
)

// Link-layer types we understand (pcap DLT_* values).
const (
	DLTNull     = 0 // BSD loopback (Npcap loopback adapter)
	DLTEthernet = 1
	DLTRaw      = 12  // raw IP (some VPN/TUN adapters)
	DLTRawAlt   = 101 // LINKTYPE_RAW
	DLTLinuxSLL = 113
)

// Segment is one TCP segment with payload.
type Segment struct {
	SrcIP, DstIP     string
	SrcPort, DstPort uint16
	Seq              uint32
	SYN, FIN, RST    bool
	Payload          []byte
}

func (s *Segment) StreamKey() string {
	return fmt.Sprintf("%s:%d>%s:%d", s.SrcIP, s.SrcPort, s.DstIP, s.DstPort)
}

// ParseFrame extracts the TCP segment from a captured frame.
// ok=false for anything that isn't IPv4/IPv6 TCP.
func ParseFrame(linkType int, data []byte) (seg Segment, ok bool) {
	var ip []byte
	switch linkType {
	case DLTNull:
		if len(data) < 4 {
			return seg, false
		}
		ip = data[4:]
	case DLTEthernet:
		if len(data) < 14 {
			return seg, false
		}
		et := binary.BigEndian.Uint16(data[12:])
		off := 14
		for et == 0x8100 || et == 0x88A8 { // VLAN tags
			if len(data) < off+4 {
				return seg, false
			}
			et = binary.BigEndian.Uint16(data[off+2:])
			off += 4
		}
		if et != 0x0800 && et != 0x86DD {
			return seg, false
		}
		ip = data[off:]
	case DLTRaw, DLTRawAlt:
		ip = data
	case DLTLinuxSLL:
		if len(data) < 16 {
			return seg, false
		}
		ip = data[16:]
	default:
		return seg, false
	}
	if len(ip) < 1 {
		return seg, false
	}

	var tcp []byte
	switch ip[0] >> 4 {
	case 4:
		if len(ip) < 20 {
			return seg, false
		}
		ihl := int(ip[0]&0x0F) * 4
		total := int(binary.BigEndian.Uint16(ip[2:]))
		if ip[9] != 6 || ihl < 20 || len(ip) < ihl {
			return seg, false
		}
		// fragments (other than the first with DF) are not expected for game traffic
		if fo := binary.BigEndian.Uint16(ip[6:]) & 0x1FFF; fo != 0 {
			return seg, false
		}
		end := len(ip)
		if total >= ihl && total < end { // trim Ethernet padding
			end = total
		}
		seg.SrcIP = fmt.Sprintf("%d.%d.%d.%d", ip[12], ip[13], ip[14], ip[15])
		seg.DstIP = fmt.Sprintf("%d.%d.%d.%d", ip[16], ip[17], ip[18], ip[19])
		tcp = ip[ihl:end]
	case 6:
		if len(ip) < 40 || ip[6] != 6 { // no extension header support (not used by game)
			return seg, false
		}
		plen := int(binary.BigEndian.Uint16(ip[4:]))
		end := len(ip)
		if 40+plen < end {
			end = 40 + plen
		}
		seg.SrcIP = fmt.Sprintf("[%x]", ip[8:24])
		seg.DstIP = fmt.Sprintf("[%x]", ip[24:40])
		tcp = ip[40:end]
	default:
		return seg, false
	}

	if len(tcp) < 20 {
		return seg, false
	}
	doff := int(tcp[12]>>4) * 4
	if doff < 20 || len(tcp) < doff {
		return seg, false
	}
	seg.SrcPort = binary.BigEndian.Uint16(tcp[0:])
	seg.DstPort = binary.BigEndian.Uint16(tcp[2:])
	seg.Seq = binary.BigEndian.Uint32(tcp[4:])
	flags := tcp[13]
	seg.FIN = flags&0x01 != 0
	seg.SYN = flags&0x02 != 0
	seg.RST = flags&0x04 != 0
	seg.Payload = tcp[doff:]
	return seg, true
}
