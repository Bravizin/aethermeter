package netcap

import (
	"sort"
	"time"
)

// seqLess compares TCP sequence numbers with wrap-around.
func seqLess(a, b uint32) bool { return int32(a-b) < 0 }

type pending struct {
	seq  uint32
	data []byte
	at   time.Time
}

// Stream reassembles one direction of a TCP connection into an ordered byte
// stream. Out-of-order segments are held briefly; if a hole doesn't fill in
// time the stream skips ahead and reports a gap so the framer can resync.
type Stream struct {
	OnData func(b []byte)
	OnGap  func()

	MaxPending int
	MaxWait    time.Duration

	started bool
	next    uint32
	queue   []pending

	Gaps int
}

func NewStream(onData func([]byte), onGap func()) *Stream {
	return &Stream{OnData: onData, OnGap: onGap, MaxPending: 128, MaxWait: 300 * time.Millisecond}
}

func (s *Stream) Push(seg Segment, now time.Time) {
	payload := seg.Payload
	seq := seg.Seq
	if seg.SYN {
		s.started = true
		s.next = seq + 1
		s.queue = nil
		if len(payload) == 0 {
			return
		}
		seq++
	}
	if len(payload) == 0 {
		return
	}
	if !s.started {
		s.started = true
		s.next = seq
	}

	// copy: capture buffers are reused by the driver
	data := append([]byte(nil), payload...)
	s.insert(pending{seq: seq, data: data, at: now})
	s.flush(now)
}

func (s *Stream) insert(p pending) {
	s.queue = append(s.queue, p)
	sort.Slice(s.queue, func(i, j int) bool { return seqLess(s.queue[i].seq, s.queue[j].seq) })
}

func (s *Stream) flush(now time.Time) {
	for {
		progressed := false
		for len(s.queue) > 0 {
			p := s.queue[0]
			end := p.seq + uint32(len(p.data))
			if !seqLess(s.next, end) { // entirely old / duplicate
				s.queue = s.queue[1:]
				progressed = true
				continue
			}
			if seqLess(s.next, p.seq) { // hole before this segment
				break
			}
			// overlaps or exactly at next: emit the new part
			skip := int(s.next - p.seq)
			s.OnData(p.data[skip:])
			s.next = end
			s.queue = s.queue[1:]
			progressed = true
		}
		if len(s.queue) == 0 {
			return
		}
		// hole: wait unless it's been too long or too much is buffered
		oldest := s.queue[0].at
		for _, q := range s.queue {
			if q.at.Before(oldest) {
				oldest = q.at
			}
		}
		if len(s.queue) > s.MaxPending || now.Sub(oldest) > s.MaxWait {
			s.Gaps++
			if s.OnGap != nil {
				s.OnGap()
			}
			s.next = s.queue[0].seq
			continue
		}
		if !progressed {
			return
		}
	}
}

// Tick lets the stream give up on holes even when no new segments arrive.
func (s *Stream) Tick(now time.Time) {
	if len(s.queue) > 0 {
		s.flush(now)
	}
}
