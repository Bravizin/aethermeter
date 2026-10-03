package demo

import (
	"math/rand"
	"time"

	"aethermeter/internal/engine"
	"aethermeter/internal/proto"
	tu "aethermeter/internal/testutil"
)

// Run simulates a 6-player boss fight by generating real wire-format
// packets and pushing them through the full pipeline (framer -> LZ4 -> decoder).
func Run(eng *engine.Engine) {
	type member struct {
		id    int
		name  string
		class uint32 // skill code prefix = class id
		power float64
	}
	party := []member{
		{1, "Sukuna", 11, 1.25},  // Gladiador (você)
		{2, "Megumi", 15, 1.10},  // Feiticeiro
		{3, "Nobara", 14, 1.00},  // Ranger
		{4, "Yuji", 13, 0.95},    // Assassino
		{5, "Maki", 12, 0.55},    // Templário
		{6, "Inumaki", 17, 0.35}, // Clérigo
	}
	const boss = 9000
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// pretend we're inside a dungeon with this party (drives the "DG" timer and the PT filter)
	var members []proto.PartyMember
	for _, m := range party {
		members = append(members, proto.PartyMember{GlobalID: 5000 + m.id, Name: m.name})
	}
	eng.Tracker.Handle(proto.PartyEvent{Name: "demo", DungeonID: 1001, Members: members})

	for {
		stream := "demo"
		send := func(pkts ...[]byte) { eng.Feed(stream, tu.Cat(pkts...), time.Now()) }
		send(tu.Heartbeat(), tu.SelfPlayer(1, party[0].name))
		for _, m := range party[1:] {
			send(tu.OtherPlayer(m.id, m.name))
		}
		maxHP := int64(180_000_000)
		hp := maxHP
		eng.Tracker.Handle(proto.MobInfoEvent{EntityID: boss, MobCode: 2090175, MaxHP: maxHP})

		for hp > 0 {
			var batch [][]byte
			for _, m := range party {
				if rng.Float64() < 0.75 {
					dmg := uint64((18_000 + rng.Float64()*22_000) * m.power)
					crit := rng.Float64() < 0.3
					if crit {
						dmg = dmg * 3 / 2
					}
					batch = append(batch, tu.Damage(tu.Hit{Target: boss, Actor: m.id, Skill: m.class*1_000_000 + uint32(1+rng.Intn(4))*10_000, Damage: dmg, Crit: crit, Back: rng.Float64() < 0.2}))
					hp -= int64(dmg)
				}
			}
			// the boss hits back: mostly the templar (tank), sometimes someone else
			victim := 5
			if rng.Float64() < 0.25 {
				victim = 1 + rng.Intn(6)
			}
			batch = append(batch, tu.Damage(tu.Hit{Target: victim, Actor: boss, Skill: 2_001_000 + uint32(rng.Intn(5)), Damage: uint64(9_000 + rng.Intn(16_000))}))
			// the cleric heals whoever is hurt (Healing Light), now and then a chanter heal
			if rng.Float64() < 0.6 {
				batch = append(batch, tu.Damage(tu.Hit{Target: []int{5, 5, 1, 2}[rng.Intn(4)], Actor: 6, Skill: 17100000, Damage: uint64(12_000 + rng.Intn(14_000)), Crit: rng.Float64() < 0.2}))
			}
			send(tu.Heartbeat(), tu.Compressed(batch...))
			if hp < 0 {
				hp = 0
			}
			eng.Tracker.Handle(proto.HpEvent{EntityID: boss, HP: hp})
			time.Sleep(250 * time.Millisecond)
		}
		send(tu.Death(boss))
		time.Sleep(15 * time.Second)
	}
}
