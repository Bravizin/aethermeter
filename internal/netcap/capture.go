package netcap

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Device describes a capture interface.
type Device struct {
	Name        string
	Description string
	Loopback    bool
}

// Handle is an open capture interface.
type Handle interface {
	// Next returns the next frame. data == nil with err == nil means timeout.
	Next() (data []byte, ts time.Time, err error)
	LinkType() int
	SetFilter(expr string) error
	Close()
}

// Backend abstracts the OS capture library (Npcap on Windows).
type Backend interface {
	Devices() ([]Device, error)
	Open(name string) (Handle, error)
}

// ErrNoBackend is returned on platforms without a capture implementation.
var ErrNoBackend = errors.New("captura de pacotes não suportada nesta plataforma")

// Logf is the logger signature used by this package.
type Logf func(format string, args ...any)

type adapterKind int

const (
	kindPhysical adapterKind = iota
	kindLoopback
	kindVirtual
)

func classify(d Device) (adapterKind, bool) {
	n, desc := strings.ToLower(d.Name), strings.ToLower(d.Description)
	if d.Loopback || strings.Contains(n, "loopback") || strings.Contains(desc, "loopback") || n == "lo" {
		return kindLoopback, true
	}
	for _, s := range []string{"hyper-v", "vmware", "virtualbox", "wan miniport", "bluetooth"} {
		if strings.Contains(desc, s) {
			return kindVirtual, false
		}
	}
	for _, s := range []string{"tap-windows", "wintun", "tun", "vpn", "wireguard", "exitlag", "gearup", "noping"} {
		if strings.Contains(desc, s) {
			return kindVirtual, true
		}
	}
	return kindPhysical, true
}

// Status is a human-readable state for the UI.
type Status struct {
	State   string // "procurando", "conectado", "erro"
	Detail  string
	Adapter string
	Port    int
}

// Capture finds the game connection and emits the server->client byte stream.
type Capture struct {
	Backend Backend
	Log     Logf

	// OnStream receives in-order stream bytes; stream identifies the TCP connection.
	OnStream func(stream string, data []byte, ts time.Time)
	// OnGap is called when a stream lost bytes (framer must resync).
	OnGap func(stream string)

	DetectionThreshold int
	GraceWindow        time.Duration
	Watchdog           time.Duration

	mu      sync.Mutex
	status  Status
	stop    chan struct{}
	wg      sync.WaitGroup
	segs    chan segMsg
	handles []*openAdapter
	curTS   time.Time // timestamp of the segment being processed (loop goroutine only)
}

type openAdapter struct {
	dev    Device
	kind   adapterKind
	h      Handle
	closed chan struct{}
	once   sync.Once
}

func (a *openAdapter) close() {
	a.once.Do(func() { close(a.closed) })
}

func (c *Capture) logf(f string, a ...any) {
	if c.Log != nil {
		c.Log(f, a...)
	}
}

func (c *Capture) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.status
}

func (c *Capture) setStatus(s Status) {
	c.mu.Lock()
	c.status = s
	c.mu.Unlock()
}

// Start opens adapters and begins detection. It returns an error only when no
// adapter could be opened at all.
func (c *Capture) Start() error {
	if c.Backend == nil {
		return ErrNoBackend
	}
	if c.DetectionThreshold == 0 {
		c.DetectionThreshold = 5
	}
	if c.GraceWindow == 0 {
		c.GraceWindow = 2 * time.Second
	}
	if c.Watchdog == 0 {
		c.Watchdog = 60 * time.Second
	}
	c.stop = make(chan struct{})
	c.segs = make(chan segMsg, 50000)
	if err := c.openAll(); err != nil {
		return err
	}
	c.wg.Add(1)
	go c.loop()
	return nil
}

func (c *Capture) Stop() {
	if c.stop == nil {
		return
	}
	close(c.stop)
	c.closeAll(nil)
	c.wg.Wait()
	c.stop = nil
}

func (c *Capture) openAll() error {
	devs, err := c.Backend.Devices()
	if err != nil {
		return err
	}
	var opened []*openAdapter
	for _, d := range devs {
		kind, ok := classify(d)
		if !ok {
			continue
		}
		h, err := c.Backend.Open(d.Name)
		if err != nil {
			c.logf("[captura] não abriu %s (%s): %v", d.Description, d.Name, err)
			continue
		}
		if err := h.SetFilter("tcp"); err != nil {
			c.logf("[captura] filtro falhou em %s: %v", d.Name, err)
		}
		a := &openAdapter{dev: d, kind: kind, h: h, closed: make(chan struct{})}
		opened = append(opened, a)
		c.logf("[captura] aberto: %s — %s", d.Name, d.Description)
	}
	if len(opened) == 0 {
		return fmt.Errorf("nenhum adaptador de rede pôde ser aberto (o Npcap está instalado?)")
	}
	c.mu.Lock()
	c.handles = opened
	c.mu.Unlock()
	for _, a := range opened {
		c.wg.Add(1)
		go c.reader(a)
	}
	c.setStatus(Status{State: "procurando", Detail: fmt.Sprintf("procurando o jogo em %d adaptador(es)…", len(opened))})
	return nil
}

func (c *Capture) closeAll(except *openAdapter) {
	c.mu.Lock()
	var keep []*openAdapter
	for _, a := range c.handles {
		if a == except {
			keep = append(keep, a)
			continue
		}
		a.close()
	}
	c.handles = keep
	c.mu.Unlock()
}

func (c *Capture) reader(a *openAdapter) {
	defer c.wg.Done()
	defer a.h.Close()
	lt := a.h.LinkType()
	for {
		select {
		case <-a.closed:
			return
		case <-c.stop:
			return
		default:
		}
		data, ts, err := a.h.Next()
		if err != nil {
			c.logf("[captura] erro lendo %s: %v", a.dev.Name, err)
			return
		}
		if data == nil {
			continue
		}
		seg, ok := ParseFrame(lt, data)
		if !ok || len(seg.Payload) == 0 && !seg.SYN {
			continue
		}
		// copy payload (driver buffer is reused)
		seg.Payload = append([]byte(nil), seg.Payload...)
		select {
		case c.segs <- segMsg{a, seg, ts}:
		default: // drop under extreme load
		}
	}
}

// --- central processing --------------------------------------------------

type segMsg struct {
	ad  *openAdapter
	seg Segment
	ts  time.Time
}

type hitKey struct {
	ad   *openAdapter
	port uint16
}

func (c *Capture) loop() {
	defer c.wg.Done()
	var (
		locked     *openAdapter
		gamePort   uint16
		hits       = map[hitKey]int{}
		graceUntil time.Time
		lastBeat   time.Time
		// port switching while locked
		recentHits = map[uint16]int{}
		recentFrom = time.Now()
		streams    = map[string]*Stream{}
	)
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()

	reset := func(reason string) {
		c.logf("[captura] voltando a procurar: %s", reason)
		locked, gamePort = nil, 0
		hits = map[hitKey]int{}
		graceUntil = time.Time{}
		streams = map[string]*Stream{}
		c.closeAll(nil)
		if err := c.openAll(); err != nil {
			c.setStatus(Status{State: "erro", Detail: err.Error()})
		}
	}

	for {
		select {
		case <-c.stop:
			return
		case now := <-tick.C:
			if locked == nil && !graceUntil.IsZero() && now.After(graceUntil) {
				best, bestHits := hitKey{}, 0
				bestKind := adapterKind(-1)
				rank := func(k adapterKind) int {
					switch k {
					case kindLoopback:
						return 3
					case kindPhysical:
						return 2
					default:
						return 1
					}
				}
				for k, h := range hits {
					if h < c.DetectionThreshold {
						continue
					}
					if bestKind < 0 || rank(k.ad.kind) > rank(bestKind) || (k.ad.kind == bestKind && h > bestHits) {
						best, bestHits, bestKind = k, h, k.ad.kind
					}
				}
				graceUntil = time.Time{}
				if best.ad != nil {
					locked, gamePort, lastBeat = best.ad, best.port, now
					recentHits, recentFrom = map[uint16]int{}, now
					c.closeAll(locked)
					c.logf("[captura] TRAVADO em %s (%s) porta %d (%d batidas)", locked.dev.Description, locked.dev.Name, gamePort, bestHits)
					c.setStatus(Status{State: "conectado", Detail: "conectado ao jogo", Adapter: locked.dev.Description, Port: int(gamePort)})
				}
			}
			if locked != nil {
				if now.Sub(lastBeat) > c.Watchdog {
					reset("sem sinal do jogo")
					continue
				}
				for _, s := range streams {
					s.Tick(now)
				}
				if now.Sub(recentFrom) > 10*time.Second {
					// switch port if the game moved to another connection
					if recentHits[gamePort] == 0 {
						var bp uint16
						bh := 0
						for p, h := range recentHits {
							if h > bh {
								bp, bh = p, h
							}
						}
						if bh >= c.DetectionThreshold {
							c.logf("[captura] jogo mudou de conexão: porta %d -> %d", gamePort, bp)
							gamePort = bp
							c.setStatus(Status{State: "conectado", Detail: "conectado ao jogo", Adapter: locked.dev.Description, Port: int(gamePort)})
						}
					}
					recentHits, recentFrom = map[uint16]int{}, now
				}
			}
		case m := <-c.segs:
			c.curTS = m.ts
			beat := bytes.Contains(m.seg.Payload, []byte{0x0E, 0x00, 0x36})
			if locked == nil {
				if !beat {
					continue
				}
				k := hitKey{m.ad, m.seg.SrcPort}
				hits[k]++
				if hits[k] >= c.DetectionThreshold && graceUntil.IsZero() {
					graceUntil = time.Now().Add(c.GraceWindow)
					c.logf("[captura] padrão do jogo visto em %s porta %d, aguardando janela…", m.ad.dev.Description, m.seg.SrcPort)
				}
				continue
			}
			if m.ad != locked {
				continue
			}
			if beat {
				recentHits[m.seg.SrcPort]++
				if m.seg.SrcPort == gamePort {
					lastBeat = time.Now()
				}
			}
			if m.seg.SrcPort != gamePort {
				continue
			}
			key := m.seg.StreamKey()
			st := streams[key]
			if st == nil {
				k := key
				st = NewStream(
					func(b []byte) {
						if c.OnStream != nil {
							c.OnStream(k, b, c.curTS)
						}
					},
					func() {
						if c.OnGap != nil {
							c.OnGap(k)
						}
					})
				streams[key] = st
			}
			if m.seg.RST || m.seg.FIN {
				st.Push(m.seg, time.Now())
				delete(streams, key)
				continue
			}
			st.Push(m.seg, time.Now())
		}
	}
}
