// Package engine wires capture -> framing -> decoding -> combat tracking, and
// provides record/replay of the raw game stream for debugging patches.
package engine

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"aethermeter/internal/combat"
	"aethermeter/internal/gamedata"
	"aethermeter/internal/netcap"
	"aethermeter/internal/proto"
)

type Engine struct {
	Tracker *combat.Tracker
	Log     func(format string, args ...any)

	mu      sync.Mutex
	framers map[string]*proto.Framer
	rec     *Recorder

	capture *netcap.Capture

	Packets  atomic.Int64
	Events   atomic.Int64
	Damage   atomic.Int64
	opcounts sync.Map // uint16 -> *atomic.Int64
}

func New() *Engine {
	return &Engine{
		Tracker: combat.NewTracker(gamedata.Get()),
		framers: map[string]*proto.Framer{},
	}
}

func (e *Engine) logf(f string, a ...any) {
	if e.Log != nil {
		e.Log(f, a...)
	}
}

// Feed pushes in-order stream bytes for one connection.
func (e *Engine) Feed(stream string, data []byte, ts time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.rec != nil {
		e.rec.write(recData, stream, data, ts)
	}
	f := e.framers[stream]
	if f == nil {
		if len(e.framers) > 0 {
			// the game opened a new connection: zone / instance change
			e.Tracker.ZoneChanged()
		}
		f = &proto.Framer{}
		e.framers[stream] = f
	}
	f.Push(data, e.handlePacket)
}

// Gap tells the framer of a stream to resynchronise.
func (e *Engine) Gap(stream string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.rec != nil {
		e.rec.write(recGap, stream, nil, time.Now())
	}
	if f := e.framers[stream]; f != nil {
		f.Reset()
	}
}

func (e *Engine) handlePacket(pkt []byte) {
	e.Packets.Add(1)
	for _, p := range proto.Unpack(pkt) {
		if op, ok := proto.Opcode(p); ok {
			v, _ := e.opcounts.LoadOrStore(op, new(atomic.Int64))
			v.(*atomic.Int64).Add(1)
		}
		for _, ev := range proto.Decode(p, gamedata.Classify) {
			e.Events.Add(1)
			if _, ok := ev.(proto.DamageEvent); ok {
				e.Damage.Add(1)
			}
			e.Tracker.Handle(ev)
		}
	}
}

// OpcodeCounts returns a copy of the opcode histogram (diagnostics).
func (e *Engine) OpcodeCounts() map[uint16]int64 {
	out := map[uint16]int64{}
	e.opcounts.Range(func(k, v any) bool {
		out[k.(uint16)] = v.(*atomic.Int64).Load()
		return true
	})
	return out
}

// StartCapture begins live capture using the platform backend.
func (e *Engine) StartCapture() error {
	be, err := netcap.NewBackend()
	if err != nil {
		return err
	}
	e.capture = &netcap.Capture{
		Backend:  be,
		Log:      e.Log,
		OnStream: e.Feed,
		OnGap:    e.Gap,
	}
	return e.capture.Start()
}

func (e *Engine) StopCapture() {
	if e.capture != nil {
		e.capture.Stop()
	}
	e.StopRecording()
}

func (e *Engine) CaptureStatus() netcap.Status {
	if e.capture == nil {
		return netcap.Status{State: "parado"}
	}
	return e.capture.Status()
}

// ---- recording / replay -----------------------------------------------------

const (
	recMagic = "PMREC1\n"
	recData  = 1
	recGap   = 2
)

type Recorder struct {
	f       *os.File
	w       *bufio.Writer
	streams map[string]uint16
	Path    string
}

func (e *Engine) StartRecording(path string) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.rec != nil {
		return nil
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 256*1024)
	w.WriteString(recMagic)
	e.rec = &Recorder{f: f, w: w, streams: map[string]uint16{}, Path: path}
	e.logf("[gravação] iniciada: %s", path)
	return nil
}

func (e *Engine) StopRecording() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.rec == nil {
		return
	}
	e.rec.w.Flush()
	e.rec.f.Close()
	e.logf("[gravação] salva: %s", e.rec.Path)
	e.rec = nil
}

func (e *Engine) Recording() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.rec == nil {
		return ""
	}
	return e.rec.Path
}

// record layout: kind u8 | stream u16 | unixMicro i64 | len u32 | data
// a new stream id is announced with kind 0 and the stream name as data.
func (r *Recorder) write(kind byte, stream string, data []byte, ts time.Time) {
	id, ok := r.streams[stream]
	if !ok {
		id = uint16(len(r.streams) + 1)
		r.streams[stream] = id
		r.writeRaw(0, id, []byte(stream), ts)
	}
	r.writeRaw(kind, id, data, ts)
}

func (r *Recorder) writeRaw(kind byte, id uint16, data []byte, ts time.Time) {
	var hdr [15]byte
	hdr[0] = kind
	binary.LittleEndian.PutUint16(hdr[1:], id)
	binary.LittleEndian.PutUint64(hdr[3:], uint64(ts.UnixMicro()))
	binary.LittleEndian.PutUint32(hdr[11:], uint32(len(data)))
	r.w.Write(hdr[:])
	r.w.Write(data)
}

// Replay feeds a recording through the pipeline. If realtime is true the
// original timing is reproduced (useful to watch the overlay); otherwise the
// tracker clock follows the recorded timestamps.
func (e *Engine) Replay(path string, realtime bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReaderSize(f, 256*1024)
	magic := make([]byte, len(recMagic))
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != recMagic {
		return errors.New("arquivo de gravação inválido")
	}
	names := map[uint16]string{}
	var first time.Time
	start := time.Now()
	var clock time.Time
	if !realtime {
		e.Tracker.Now = func() time.Time { return clock }
	}
	for {
		var hdr [15]byte
		if _, err := io.ReadFull(r, hdr[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		kind := hdr[0]
		id := binary.LittleEndian.Uint16(hdr[1:])
		ts := time.UnixMicro(int64(binary.LittleEndian.Uint64(hdr[3:])))
		n := binary.LittleEndian.Uint32(hdr[11:])
		if n > 64*1024*1024 {
			return fmt.Errorf("registro corrompido (%d bytes)", n)
		}
		data := make([]byte, n)
		if _, err := io.ReadFull(r, data); err != nil {
			return err
		}
		if first.IsZero() {
			first = ts
		}
		if realtime {
			if d := ts.Sub(first) - time.Since(start); d > 0 {
				time.Sleep(d)
			}
		} else {
			clock = ts
		}
		switch kind {
		case 0:
			names[id] = string(data)
		case recData:
			e.Feed(names[id], data, ts)
		case recGap:
			e.Gap(names[id])
		}
	}
}
