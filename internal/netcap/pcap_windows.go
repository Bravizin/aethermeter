//go:build windows

package netcap

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

// Npcap backend: loads wpcap.dll at runtime (no cgo needed).

var (
	kernel32       = syscall.NewLazyDLL("kernel32.dll")
	procSetDllDirW = kernel32.NewProc("SetDllDirectoryW")
	wpcap          *syscall.DLL
	pFindAllDevs   *syscall.Proc
	pFreeAllDevs   *syscall.Proc
	pCreate        *syscall.Proc
	pSetSnaplen    *syscall.Proc
	pSetPromisc    *syscall.Proc
	pSetTimeout    *syscall.Proc
	pSetImmediate  *syscall.Proc
	pSetBufferSize *syscall.Proc
	pActivate      *syscall.Proc
	pDatalink      *syscall.Proc
	pCompile       *syscall.Proc
	pSetFilter     *syscall.Proc
	pFreeCode      *syscall.Proc
	pNextEx        *syscall.Proc
	pClose         *syscall.Proc
	pGetErr        *syscall.Proc
	loadErr        error
)

// ErrNpcapMissing is returned when wpcap.dll can't be found.
var ErrNpcapMissing = errors.New("Npcap não encontrado — instale em https://npcap.com (marque \"WinPcap API-compatible mode\")")

func loadNpcap() error {
	if wpcap != nil || loadErr != nil {
		return loadErr
	}
	sys := os.Getenv("SystemRoot")
	if sys == "" {
		sys = `C:\Windows`
	}
	dir := filepath.Join(sys, "System32", "Npcap")
	if p, err := syscall.UTF16PtrFromString(dir); err == nil {
		procSetDllDirW.Call(uintptr(unsafe.Pointer(p)))
	}
	d, err := syscall.LoadDLL("wpcap.dll")
	if err != nil {
		loadErr = ErrNpcapMissing
		return loadErr
	}
	wpcap = d
	must := func(name string) *syscall.Proc {
		p, err := d.FindProc(name)
		if err != nil && loadErr == nil {
			loadErr = fmt.Errorf("wpcap.dll sem %s: %w", name, err)
		}
		return p
	}
	pFindAllDevs = must("pcap_findalldevs")
	pFreeAllDevs = must("pcap_freealldevs")
	pCreate = must("pcap_create")
	pSetSnaplen = must("pcap_set_snaplen")
	pSetPromisc = must("pcap_set_promisc")
	pSetTimeout = must("pcap_set_timeout")
	pSetImmediate = must("pcap_set_immediate_mode")
	pSetBufferSize = must("pcap_set_buffer_size")
	pActivate = must("pcap_activate")
	pDatalink = must("pcap_datalink")
	pCompile = must("pcap_compile")
	pSetFilter = must("pcap_setfilter")
	pFreeCode = must("pcap_freecode")
	pNextEx = must("pcap_next_ex")
	pClose = must("pcap_close")
	pGetErr = must("pcap_geterr")
	return loadErr
}

// C structs (64-bit Windows layout).
type pcapIf struct {
	next        *pcapIf
	name        *byte
	description *byte
	addresses   uintptr
	flags       uint32
}

type pcapPkthdr struct {
	tvSec  int32 // Windows 'long' is 32-bit
	tvUsec int32
	caplen uint32
	length uint32
}

type bpfProgram struct {
	bfLen   uint32
	_       uint32
	bfInsns uintptr
}

const pcapIfLoopback = 0x00000001

func goString(p *byte) string {
	if p == nil {
		return ""
	}
	var b []byte
	for ptr := unsafe.Pointer(p); ; ptr = unsafe.Add(ptr, 1) {
		c := *(*byte)(ptr)
		if c == 0 {
			break
		}
		b = append(b, c)
		if len(b) > 4096 {
			break
		}
	}
	return string(b)
}

type npcap struct{}

// NewBackend returns the Npcap backend.
func NewBackend() (Backend, error) {
	if err := loadNpcap(); err != nil {
		return nil, err
	}
	return npcap{}, nil
}

func (npcap) Devices() ([]Device, error) {
	var all *pcapIf
	errbuf := make([]byte, 512)
	r, _, _ := pFindAllDevs.Call(uintptr(unsafe.Pointer(&all)), uintptr(unsafe.Pointer(&errbuf[0])))
	if int32(r) != 0 {
		return nil, fmt.Errorf("pcap_findalldevs: %s", goString(&errbuf[0]))
	}
	defer pFreeAllDevs.Call(uintptr(unsafe.Pointer(all)))
	var out []Device
	for d := all; d != nil; d = d.next {
		out = append(out, Device{
			Name:        goString(d.name),
			Description: goString(d.description),
			Loopback:    d.flags&pcapIfLoopback != 0,
		})
	}
	return out, nil
}

type npcapHandle struct {
	p  uintptr
	lt int
}

func (npcap) Open(name string) (Handle, error) {
	errbuf := make([]byte, 512)
	cname, err := syscall.BytePtrFromString(name)
	if err != nil {
		return nil, err
	}
	p, _, _ := pCreate.Call(uintptr(unsafe.Pointer(cname)), uintptr(unsafe.Pointer(&errbuf[0])))
	if p == 0 {
		return nil, fmt.Errorf("pcap_create: %s", goString(&errbuf[0]))
	}
	pSetSnaplen.Call(p, 65535)
	pSetPromisc.Call(p, 0)
	pSetTimeout.Call(p, 100)
	pSetImmediate.Call(p, 1)
	pSetBufferSize.Call(p, 32*1024*1024)
	if r, _, _ := pActivate.Call(p); int32(r) < 0 {
		msg := handleErr(p)
		pClose.Call(p)
		return nil, fmt.Errorf("pcap_activate: %s", msg)
	}
	lt, _, _ := pDatalink.Call(p)
	return &npcapHandle{p: p, lt: int(int32(lt))}, nil
}

func handleErr(p uintptr) string {
	r, _, _ := pGetErr.Call(p)
	if r == 0 {
		return "?"
	}
	return goString((*byte)(unsafe.Add(nil, r)))
}

func (h *npcapHandle) LinkType() int { return h.lt }

func (h *npcapHandle) SetFilter(expr string) error {
	var prog bpfProgram
	cexpr, err := syscall.BytePtrFromString(expr)
	if err != nil {
		return err
	}
	const netmaskUnknown = 0xFFFFFFFF
	r, _, _ := pCompile.Call(h.p, uintptr(unsafe.Pointer(&prog)), uintptr(unsafe.Pointer(cexpr)), 1, netmaskUnknown)
	if int32(r) < 0 {
		return fmt.Errorf("pcap_compile: %s", handleErr(h.p))
	}
	defer pFreeCode.Call(uintptr(unsafe.Pointer(&prog)))
	r, _, _ = pSetFilter.Call(h.p, uintptr(unsafe.Pointer(&prog)))
	if int32(r) < 0 {
		return fmt.Errorf("pcap_setfilter: %s", handleErr(h.p))
	}
	return nil
}

func (h *npcapHandle) Next() ([]byte, time.Time, error) {
	var hdr *pcapPkthdr
	var data *byte
	r, _, _ := pNextEx.Call(h.p, uintptr(unsafe.Pointer(&hdr)), uintptr(unsafe.Pointer(&data)))
	switch int32(r) {
	case 1:
		if hdr == nil || data == nil {
			return nil, time.Time{}, nil
		}
		n := int(hdr.caplen)
		ts := time.Unix(int64(hdr.tvSec), int64(hdr.tvUsec)*1000)
		return unsafe.Slice(data, n), ts, nil
	case 0:
		return nil, time.Time{}, nil // timeout
	case -2:
		return nil, time.Time{}, errors.New("fim da captura")
	default:
		return nil, time.Time{}, errors.New(handleErr(h.p))
	}
}

func (h *npcapHandle) Close() {
	if h.p != 0 {
		pClose.Call(h.p)
		h.p = 0
	}
}
