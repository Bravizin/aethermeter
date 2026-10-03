package engine

import (
	"path/filepath"
	"testing"
	"time"

	"aethermeter/internal/combat"
	tu "aethermeter/internal/testutil"
)

// Full pipeline: raw stream bytes split at awkward places -> framer -> LZ4 ->
// decoder -> tracker, then record and replay the same stream.
func TestPipelineRecordReplay(t *testing.T) {
	e := New()
	dir := t.TempDir()
	rec := filepath.Join(dir, "x.pmrec")
	if err := e.StartRecording(rec); err != nil {
		t.Fatal(err)
	}
	stream := tu.Cat(
		[]byte{1, 2, 3}, // mid-stream garbage before sync
		tu.Heartbeat(),
		tu.SelfPlayer(1, "Leo"),
		tu.OtherPlayer(2, "Ana"),
		tu.Compressed(
			tu.Damage(tu.Hit{Target: 50, Actor: 1, Skill: 11010000, Damage: 1000}),
			tu.Damage(tu.Hit{Target: 50, Actor: 2, Skill: 15010000, Damage: 3000, Crit: true}),
		),
		tu.Dot(50, 2, 15010000, 500),
	)
	now := time.Now()
	for i := 0; i < len(stream); i += 7 {
		end := min(i+7, len(stream))
		e.Feed("s1", stream[i:end], now)
	}
	e.StopRecording()

	check := func(e *Engine) {
		s := e.Tracker.Snapshot(combat.SnapshotOptions{Mode: combat.ViewAll})
		if len(s.Rows) != 2 {
			t.Fatalf("rows=%d", len(s.Rows))
		}
		if s.Rows[0].Name != "Ana" || s.Rows[1].Name != "Leo" || !s.Rows[1].IsSelf {
			t.Fatalf("rows: %+v", s.Rows)
		}
		// the DoT is only counted when its id is in the dot list; direct hits always count
		if s.Rows[1].Damage != 1000 || s.Rows[0].Damage < 3000 {
			t.Fatalf("damage: %d %d", s.Rows[0].Damage, s.Rows[1].Damage)
		}
	}
	check(e)

	e2 := New()
	if err := e2.Replay(rec, false); err != nil {
		t.Fatal(err)
	}
	check(e2)
	if e2.Packets.Load() != e.Packets.Load() {
		t.Fatalf("replay packets %d != %d", e2.Packets.Load(), e.Packets.Load())
	}
}
