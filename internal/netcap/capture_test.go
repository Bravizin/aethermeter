package netcap

import (
	"bytes"
	"sync"
	"testing"
	"time"
)

type fakeHandle struct {
	mu     sync.Mutex
	frames [][]byte
}

func (h *fakeHandle) Next() ([]byte, time.Time, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.frames) == 0 {
		h.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		h.mu.Lock()
		return nil, time.Time{}, nil
	}
	f := h.frames[0]
	h.frames = h.frames[1:]
	return f, time.Now(), nil
}
func (h *fakeHandle) LinkType() int          { return DLTEthernet }
func (h *fakeHandle) SetFilter(string) error { return nil }
func (h *fakeHandle) Close()                 {}

type fakeBackend struct{ handles map[string]*fakeHandle }

func (b *fakeBackend) Devices() ([]Device, error) {
	return []Device{{Name: "eth0", Description: "Realtek Ethernet"}, {Name: "vm", Description: "VMware Virtual Ethernet"}}, nil
}
func (b *fakeBackend) Open(name string) (Handle, error) { return b.handles[name], nil }

func TestCaptureDetectsGameAndEmitsStream(t *testing.T) {
	hb := []byte{0x0E, 0x00, 0x36, 0, 0, 0, 0, 0, 0, 0, 0}
	var frames [][]byte
	seq := uint32(1000)
	noiseSeq := uint32(5)
	for i := 0; i < 8; i++ {
		frames = append(frames, ethIPv4TCP(443, 50001, noiseSeq, []byte("https noise")))
		noiseSeq += 11
		frames = append(frames, ethIPv4TCP(7777, 50000, seq, hb))
		seq += uint32(len(hb))
	}
	h := &fakeHandle{frames: frames}
	be := &fakeBackend{handles: map[string]*fakeHandle{"eth0": h, "vm": {}}}

	var mu sync.Mutex
	var got bytes.Buffer
	c := &Capture{
		Backend:     be,
		GraceWindow: 50 * time.Millisecond,
		OnStream: func(stream string, data []byte, ts time.Time) {
			mu.Lock()
			got.Write(data)
			mu.Unlock()
		},
	}
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer c.Stop()

	deadline := time.Now().Add(3 * time.Second)
	for c.Status().State != "conectado" {
		if time.Now().After(deadline) {
			t.Fatalf("never locked: %+v", c.Status())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c.Status().Port != 7777 {
		t.Fatalf("locked on wrong port %d", c.Status().Port)
	}
	// after locking, game data flows; noise doesn't
	h.mu.Lock()
	h.frames = append(h.frames,
		ethIPv4TCP(443, 50001, noiseSeq, []byte("more noise")),
		ethIPv4TCP(7777, 50000, seq, []byte("GAMEDATA")))
	h.mu.Unlock()
	for {
		mu.Lock()
		ok := bytes.Contains(got.Bytes(), []byte("GAMEDATA"))
		noisy := bytes.Contains(got.Bytes(), []byte("noise"))
		mu.Unlock()
		if noisy {
			t.Fatal("non-game traffic leaked into the stream")
		}
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("game data not delivered")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
