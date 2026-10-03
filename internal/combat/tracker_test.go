package combat

import (
	"strings"
	"testing"
	"time"

	"aethermeter/internal/gamedata"
	"aethermeter/internal/proto"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }

func newT() (*Tracker, *clock) {
	c := &clock{t: time.Date(2026, 10, 1, 20, 0, 0, 0, time.UTC)}
	tr := NewTracker(gamedata.Get())
	tr.Now = c.now
	return tr, c
}

func hit(actor, target, skill int, dmg int64) proto.DamageEvent {
	return proto.DamageEvent{ActorID: actor, TargetID: target, SkillCode: skill, Damage: dmg}
}

func TestDpsAndShares(t *testing.T) {
	tr, c := newT()
	tr.Handle(proto.PlayerInfoEvent{EntityID: 1, Name: "Leo", IsSelf: true})
	tr.Handle(proto.PlayerInfoEvent{EntityID: 2, Name: "Ana"})
	tr.Handle(hit(1, 100, 11010000, 1000))
	tr.Handle(hit(2, 100, 15000001, 3000))
	c.add(10 * time.Second)
	tr.Handle(hit(1, 100, 11010000, 1000))

	s := tr.Snapshot(SnapshotOptions{})
	if len(s.Rows) != 2 {
		t.Fatalf("rows=%d", len(s.Rows))
	}
	if s.Rows[0].Name != "Ana" || s.Rows[0].Damage != 3000 {
		t.Fatalf("top row wrong: %+v", s.Rows[0])
	}
	if s.Duration != 10*time.Second {
		t.Fatalf("duration %v", s.Duration)
	}
	if s.Rows[0].DPS != 300 || s.Rows[1].DPS != 200 {
		t.Fatalf("dps %v %v", s.Rows[0].DPS, s.Rows[1].DPS)
	}
	if !s.Rows[1].IsSelf || s.Rows[1].ClassName != "Gladiador" {
		t.Fatalf("self row: %+v", s.Rows[1])
	}
	if s.Rows[1].Skills[0].Name != "Rending Blow" {
		t.Fatalf("skill name: %q", s.Rows[1].Skills[0].Name)
	}
	if !strings.Contains(Summary(s), "Ana") {
		t.Fatal("summary missing player")
	}
}

func TestIdleSplitsEncounters(t *testing.T) {
	tr, c := newT()
	tr.Handle(hit(1, 100, 11010000, 500))
	c.add(30 * time.Second)
	tr.Handle(hit(1, 101, 11010000, 700))
	s := tr.Snapshot(SnapshotOptions{})
	if s.TotalDamage != 700 || s.History != 2 {
		t.Fatalf("expected new encounter: total=%d history=%d", s.TotalDamage, s.History)
	}
	prev := tr.Snapshot(SnapshotOptions{Back: 1})
	if prev.TotalDamage != 500 {
		t.Fatalf("previous encounter total=%d", prev.TotalDamage)
	}
}

func TestSummonMergedIntoOwner(t *testing.T) {
	tr, _ := newT()
	tr.Handle(proto.PlayerInfoEvent{EntityID: 7, Name: "Ele"})
	tr.Handle(hit(7, 100, 16000001, 1000))
	tr.Handle(hit(88, 100, 100001, 400)) // spirit hits before we know who owns it
	tr.Handle(proto.SummonEvent{SummonID: 88, OwnerID: 7})
	s := tr.Snapshot(SnapshotOptions{})
	if len(s.Rows) != 1 || s.Rows[0].Damage != 1400 {
		t.Fatalf("summon not merged: %+v", s.Rows)
	}
	pet := false
	for _, sk := range s.Rows[0].Skills {
		pet = pet || sk.IsPet
	}
	if !pet {
		t.Fatal("pet skill not flagged")
	}
}

func TestMainTargetPrefersBoss(t *testing.T) {
	tr, _ := newT()
	tr.Handle(proto.MobInfoEvent{EntityID: 500, MobCode: 2090175, MaxHP: 1_000_000})
	tr.Handle(hit(1, 400, 11010000, 9000)) // trash mob, more damage
	tr.Handle(hit(1, 500, 11010000, 1000)) // boss
	s := tr.Snapshot(SnapshotOptions{Mode: ViewMainTarget})
	if s.TargetName != "Fediv Wraith" || s.TotalDamage != 1000 || !s.IsBoss {
		t.Fatalf("main target: %+v", s)
	}
	all := tr.Snapshot(SnapshotOptions{Mode: ViewAll})
	if all.TotalDamage != 10000 {
		t.Fatalf("all total %d", all.TotalDamage)
	}
}

func TestPartyFilter(t *testing.T) {
	tr, _ := newT()
	tr.Handle(proto.PlayerInfoEvent{EntityID: 1, Name: "Leo", IsSelf: true})
	tr.Handle(proto.PlayerInfoEvent{EntityID: 2, Name: "Ana"})
	tr.Handle(proto.PlayerInfoEvent{EntityID: 3, Name: "Random"})
	tr.Handle(proto.PartyEvent{Members: []proto.PartyMember{{GlobalID: 9001, Name: "Leo"}, {GlobalID: 9002, Name: "Ana"}}})
	for _, id := range []int{1, 2, 3} {
		tr.Handle(hit(id, 100, 11010000, 100))
	}
	s := tr.Snapshot(SnapshotOptions{PartyOnly: true})
	if len(s.Rows) != 2 {
		t.Fatalf("party filter rows=%d", len(s.Rows))
	}
	if n := len(tr.Snapshot(SnapshotOptions{}).Rows); n != 3 {
		t.Fatalf("unfiltered rows=%d", n)
	}
}

func TestBossDeathEndsEncounter(t *testing.T) {
	tr, _ := newT()
	tr.Handle(proto.MobInfoEvent{EntityID: 500, MobCode: 2090175, MaxHP: 1_000_000})
	tr.Handle(hit(1, 500, 11010000, 1000))
	tr.Handle(proto.DeathEvent{EntityID: 500})
	s := tr.Snapshot(SnapshotOptions{})
	if s.Active || !s.TargetDead {
		t.Fatalf("expected finished encounter: %+v", s)
	}
}

func TestShort(t *testing.T) {
	if Full(1_950_000) != "1.950.000" || Full(950) != "950" || Full(1000) != "1.000" {
		t.Errorf("Full: %s %s %s", Full(1_950_000), Full(950), Full(1000))
	}
	for in, want := range map[float64]string{950: "950", 12345: "12,3k", 4_560_000: "4,56M", 1_200_000_000: "1,20B"} {
		if got := Short(in); got != want {
			t.Errorf("Short(%v)=%s want %s", in, got, want)
		}
	}
}

// Killing one mob and moving to the next must give the next mob a fresh meter.
func TestNextMobStartsFresh(t *testing.T) {
	tr, c := newT()
	tr.Handle(hit(1, 100, 11010000, 50_000))
	c.add(5 * time.Second)
	tr.Handle(hit(1, 100, 11010000, 50_000))
	tr.Handle(proto.HpEvent{EntityID: 100, HP: 0}) // mob A dies
	c.add(500 * time.Millisecond)
	tr.Handle(hit(1, 100, 11010000, 999)) // trailing packet on the corpse: ignored
	c.add(3 * time.Second)
	tr.Handle(hit(1, 200, 11010000, 1000)) // mob B, far less damage so far
	s := tr.Snapshot(SnapshotOptions{Mode: ViewMainTarget})
	if s.TotalDamage != 1000 || !s.Active {
		t.Fatalf("mob B should have its own meter: total=%d active=%v target=%q", s.TotalDamage, s.Active, s.TargetName)
	}
	prev := tr.Snapshot(SnapshotOptions{Back: 1})
	if prev.TotalDamage != 100_000 {
		t.Fatalf("mob A result lost: %d", prev.TotalDamage)
	}
}

// Without any death info, the overlay must still follow the mob being hit now.
func TestMainTargetFollowsCurrentMob(t *testing.T) {
	tr, c := newT()
	tr.Handle(hit(1, 100, 11010000, 90_000))
	c.add(6 * time.Second)
	tr.Handle(hit(1, 200, 11010000, 1_000))
	s := tr.Snapshot(SnapshotOptions{Mode: ViewMainTarget})
	if s.TotalDamage != 1000 {
		t.Fatalf("should focus the mob being hit now, got total=%d (%s)", s.TotalDamage, s.TargetName)
	}
}

// In an AoE pull, one mob dying doesn't end the fight while others are hit.
func TestAoEPullContinues(t *testing.T) {
	tr, c := newT()
	tr.Handle(hit(1, 100, 11010000, 500))
	tr.Handle(hit(1, 101, 11010000, 500))
	c.add(time.Second)
	tr.Handle(proto.HpEvent{EntityID: 100, HP: 0})
	s := tr.Snapshot(SnapshotOptions{Mode: ViewAll})
	if !s.Active || s.TotalDamage != 1000 {
		t.Fatalf("pull should continue: %+v", s)
	}
}

func TestHealAndTankTabs(t *testing.T) {
	tr, c := newT()
	tr.Handle(proto.PlayerInfoEvent{EntityID: 1, Name: "Maki"})    // tank
	tr.Handle(proto.PlayerInfoEvent{EntityID: 2, Name: "Inumaki"}) // healer
	tr.Handle(proto.MobInfoEvent{EntityID: 500, MobCode: 2090175, MaxHP: 1_000_000})
	tr.Handle(proto.DamageEvent{ActorID: 2, TargetID: 1, SkillCode: 17100000, Damage: 800}) // heal before the fight: ignored
	tr.Handle(hit(1, 500, 12010000, 1000))
	tr.Handle(proto.DamageEvent{ActorID: 500, TargetID: 1, SkillCode: 2_001_234, Damage: 5000, NPC: true})
	tr.Handle(proto.DamageEvent{ActorID: 500, TargetID: 2, SkillCode: 2_001_234, Damage: 1000, NPC: true})
	tr.Handle(proto.DamageEvent{ActorID: 500, TargetID: 9999, SkillCode: 2_001_234, Damage: 7777, NPC: true}) // unknown target: ignored
	c.add(10 * time.Second)
	tr.Handle(proto.DamageEvent{ActorID: 2, TargetID: 1, SkillCode: 17100000, Damage: 3000, Crit: true})
	tr.Handle(proto.DamageEvent{ActorID: 2, TargetID: 2, SkillCode: 17100000, Damage: 1000}) // self-heal counts

	heal := tr.Snapshot(SnapshotOptions{Metric: MetricHeal})
	if len(heal.Rows) != 1 || heal.Rows[0].Name != "Inumaki" || heal.Rows[0].Damage != 4000 {
		t.Fatalf("heal rows: %+v", heal.Rows)
	}
	if heal.Rows[0].ClassName != "Clérigo" || heal.Rows[0].DPS != 400 {
		t.Fatalf("heal row details: %+v", heal.Rows[0])
	}
	tank := tr.Snapshot(SnapshotOptions{Metric: MetricTaken})
	if len(tank.Rows) != 2 || tank.Rows[0].Name != "Maki" || tank.Rows[0].Damage != 5000 {
		t.Fatalf("tank rows: %+v", tank.Rows)
	}
	if tank.Rows[0].Skills[0].Name != "Fediv Wraith" {
		t.Fatalf("tank source: %+v", tank.Rows[0].Skills)
	}
	dps := tr.Snapshot(SnapshotOptions{})
	if dps.TotalDamage != 1000 {
		t.Fatalf("heals/taken leaked into dps: %d", dps.TotalDamage)
	}
}

func TestDungeonTimer(t *testing.T) {
	tr, c := newT()
	tr.Handle(proto.PartyEvent{DungeonID: 0, Members: []proto.PartyMember{{GlobalID: 1, Name: "Leo"}}})
	if s := tr.Snapshot(SnapshotOptions{}); s.Dungeon != 0 {
		t.Fatalf("no dungeon yet: %v", s.Dungeon)
	}
	tr.Handle(proto.PartyEvent{DungeonID: 777, Members: []proto.PartyMember{{GlobalID: 1, Name: "Leo"}}})
	c.add(90 * time.Second)
	tr.Handle(proto.PartyEvent{DungeonID: 777, Members: []proto.PartyMember{{GlobalID: 1, Name: "Leo"}}}) // resend: no reset
	if s := tr.Snapshot(SnapshotOptions{}); s.Dungeon != 90*time.Second {
		t.Fatalf("dungeon time %v", s.Dungeon)
	}
}

func TestDungeonTimerStops(t *testing.T) {
	party := func(id uint32) proto.PartyEvent {
		return proto.PartyEvent{DungeonID: id, Members: []proto.PartyMember{{GlobalID: 1, Name: "Leo"}}}
	}
	tr, c := newT()
	tr.Handle(party(777))
	tr.Handle(proto.MobInfoEvent{EntityID: 500, MobCode: 2090175, MaxHP: 1_000_000})
	c.add(2 * time.Minute)
	tr.Handle(hit(1, 500, 11010000, 1000))
	c.add(30 * time.Second)
	tr.Handle(proto.DeathEvent{EntityID: 500}) // boss down at 2:30
	c.add(time.Minute)
	if s := tr.Snapshot(SnapshotOptions{}); s.Dungeon != 150*time.Second || !s.DungeonDone {
		t.Fatalf("timer should stop at the boss kill: %v done=%v", s.Dungeon, s.DungeonDone)
	}
	// fighting again in the same run resumes the timer (time in between counts)
	tr.Handle(hit(1, 600, 11010000, 1000))
	if s := tr.Snapshot(SnapshotOptions{}); s.Dungeon != 210*time.Second || s.DungeonDone {
		t.Fatalf("timer should resume: %v done=%v", s.Dungeon, s.DungeonDone)
	}
	// leaving the dungeon (new connection) ends the run for good
	c.add(10 * time.Second)
	tr.ZoneChanged()
	c.add(5 * time.Minute)
	if s := tr.Snapshot(SnapshotOptions{}); s.Dungeon != 220*time.Second || !s.DungeonDone {
		t.Fatalf("timer should stop when leaving: %v done=%v", s.Dungeon, s.DungeonDone)
	}
	c.add(10 * time.Minute)
	if s := tr.Snapshot(SnapshotOptions{}); s.Dungeon != 0 {
		t.Fatalf("finished run should disappear after a while: %v", s.Dungeon)
	}
	// idle in the dungeon without fighting: pauses at the last fight
	tr.Handle(party(888))
	c.add(time.Minute)
	tr.Handle(hit(1, 700, 11010000, 10))
	c.add(10 * time.Minute)
	if s := tr.Snapshot(SnapshotOptions{}); s.Dungeon != time.Minute || !s.DungeonDone {
		t.Fatalf("idle should pause at the last fight: %v done=%v", s.Dungeon, s.DungeonDone)
	}
	// party packet with no dungeon = left
	tr.Handle(party(0))
	if s := tr.Snapshot(SnapshotOptions{}); !s.DungeonDone {
		t.Fatalf("dungeon id 0 should stop the timer")
	}
}

// Solo, no name packets: our cooldown packets tell us which player we are,
// and "só a minha PT" then shows just us (with the name from last session).
func TestSoloSelfFromCooldowns(t *testing.T) {
	tr, c := newT()
	tr.Remember("Leo", 11)
	// three strangers and us (id 7) hitting mobs around
	for i := 0; i < 4; i++ {
		tr.Handle(hit(7, 100, 11010231, 1000)) // us: Rending Blow (specialised variant)
		if i == 0 {
			tr.Handle(hit(8, 101, 11010000, 900)) // another gladiator happens to use it at the same moment once
		}
		tr.Handle(proto.SkillCdEvent{Skills: []int{11010000}})
		c.add(3 * time.Second)
		tr.Handle(hit(8, 101, 11010000, 900))
		tr.Handle(hit(9, 102, 15010000, 800))
		c.add(3 * time.Second)
	}
	s := tr.Snapshot(SnapshotOptions{Mode: ViewAll, PartyOnly: true})
	if !s.SelfKnown || len(s.Rows) != 1 || s.Rows[0].ID != 7 || s.Rows[0].Name != "Leo" || !s.Rows[0].IsSelf {
		t.Fatalf("solo filter should show only us named Leo: %+v", s.Rows)
	}
	if all := tr.Snapshot(SnapshotOptions{Mode: ViewAll}); len(all.Rows) != 3 {
		t.Fatalf("without the filter everyone shows: %d", len(all.Rows))
	}
}

func TestLeavingPartyGoesSolo(t *testing.T) {
	tr, _ := newT()
	tr.Handle(proto.PlayerInfoEvent{EntityID: 1, Name: "Leo", IsSelf: true})
	tr.Handle(proto.PlayerInfoEvent{EntityID: 2, Name: "Ana"})
	tr.Handle(proto.PartyEvent{Members: []proto.PartyMember{{GlobalID: 1, Name: "Leo"}, {GlobalID: 2, Name: "Ana"}}})
	tr.Handle(proto.PartyEvent{}) // left the party
	tr.Handle(hit(1, 100, 11010000, 100))
	tr.Handle(hit(2, 100, 15010000, 100))
	s := tr.Snapshot(SnapshotOptions{PartyOnly: true})
	if s.PartyKnown || len(s.Rows) != 1 || s.Rows[0].Name != "Leo" {
		t.Fatalf("after leaving the party only we should show: %+v", s.Rows)
	}
	if n, c := tr.SelfIdentity(); n != "Leo" || c != 11 {
		t.Fatalf("self identity %q %d", n, c)
	}
}
