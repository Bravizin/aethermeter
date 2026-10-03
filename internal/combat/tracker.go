// Package combat turns decoded game events into encounters and DPS snapshots.
package combat

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"aethermeter/internal/gamedata"
	"aethermeter/internal/proto"
)

// ViewMode selects which damage the snapshot aggregates.
type ViewMode int

const (
	ViewMainTarget ViewMode = iota // damage on the main target (boss if any)
	ViewAll                        // all damage in the encounter
)

func (m ViewMode) String() string {
	if m == ViewAll {
		return "Tudo"
	}
	return "Alvo principal"
}

type Player struct {
	ID          int
	GlobalID    int
	Name        string
	ServerID    int
	ClassID     int
	CombatPower int64
	Identified  bool
	IsSelf      bool
}

type MobState struct {
	ID      int
	Code    int
	MaxHP   int64
	HP      int64
	Dead    bool
	DiedAt  time.Time
	Name    string
	IsBoss  bool
	IsDummy bool
}

type skillAgg struct {
	Name   string
	IsDot  bool
	IsPet  bool
	Hits   int
	Crits  int
	Damage int64
	MaxHit int64
}

type actorAgg struct {
	Damage  int64
	Hits    int
	Crits   int
	Back    int
	Perfect int
	Double  int
	MaxHit  int64
	Skills  map[int]*skillAgg
}

type targetAgg struct {
	ID       int
	FirstHit time.Time
	LastHit  time.Time
	Total    int64
	Actors   map[int]*actorAgg // raw actor id (summons resolved at snapshot time)
}

type Encounter struct {
	ID       int
	Start    time.Time
	LastHit  time.Time // last damage dealt or taken (drives the idle timeout)
	LastAny  time.Time // also includes heals
	Targets  map[int]*targetAgg
	Heals    map[int]*actorAgg // healer raw id -> heals (skills = heal skills)
	Taken    map[int]*actorAgg // victim player id -> damage taken (skills = sources)
	Deaths   map[int]int       // player id -> deaths
	Ended    bool
	EndCause string
}

// Tracker is safe for concurrent use: events come from the capture goroutine,
// snapshots are taken by the UI.
type Tracker struct {
	mu sync.Mutex
	db *gamedata.DB

	IdleTimeout time.Duration
	Now         func() time.Time

	players   map[int]*Player // session id -> player
	globals   map[int]*Player // global id -> identity
	summons   map[int]int     // summon id -> owner session id
	mobs      map[int]*MobState
	party     map[int]bool    // global ids of party members
	partyName map[string]bool // lower-cased names of party members
	selfName  string

	// recognising "you" without a name packet (see SkillCdEvent)
	selfID          int
	selfVotes       map[int]int
	recentCasts     map[int]time.Time // skill family -> when it went on cooldown
	recentHits      []castHit
	rememberedName  string // your name from a previous session
	rememberedClass int

	// dungeon timer
	dungeonID     uint32    // 0 = not in a dungeon (as far as we know)
	dungeonSince  time.Time // when the current/last run started
	dungeonFrozen time.Time // non-zero: timer stopped here (boss killed / left the dungeon)
	dungeonLeft   bool      // the run is over (left the dungeon); keep showing the final time for a while
	lastCombat    time.Time

	cur     *Encounter
	history []*Encounter // finished encounters, newest last
	nextID  int

	EventsSeen int
}

type castHit struct {
	actor, family int
	at            time.Time
}

func NewTracker(db *gamedata.DB) *Tracker {
	return &Tracker{
		selfVotes:   map[int]int{},
		recentCasts: map[int]time.Time{},
		db:          db,
		IdleTimeout: 12 * time.Second,
		Now:         time.Now,
		players:     map[int]*Player{},
		globals:     map[int]*Player{},
		summons:     map[int]int{},
		mobs:        map[int]*MobState{},
		party:       map[int]bool{},
		partyName:   map[string]bool{},
	}
}

// Reset clears the current encounter and the history (keeps known players).
func (t *Tracker) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.cur = nil
	t.history = nil
	t.summons = map[int]int{}
}

// Handle applies one decoded event.
func (t *Tracker) Handle(ev proto.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.EventsSeen++
	now := t.Now()

	switch e := ev.(type) {
	case proto.DamageEvent:
		t.onDamage(e, now)
	case proto.PlayerInfoEvent:
		t.onPlayerInfo(e)
	case proto.GlobalLinkEvent:
		p := t.player(e.SessionID)
		p.GlobalID = e.GlobalID
		if g, ok := t.globals[e.GlobalID]; ok {
			applyIdentity(p, g)
		} else {
			cp := *p
			cp.ID = e.GlobalID
			t.globals[e.GlobalID] = &cp
		}
	case proto.PartyEvent:
		if e.DungeonID != t.dungeonID {
			if e.DungeonID == 0 {
				t.leaveDungeon(now)
			} else {
				t.dungeonID = e.DungeonID
				t.dungeonSince, t.dungeonFrozen, t.dungeonLeft = now, time.Time{}, false
			}
		}
		t.party = map[int]bool{}
		t.partyName = map[string]bool{}
		for _, m := range e.Members {
			t.party[m.GlobalID] = true
			t.partyName[strings.ToLower(m.Name)] = true
			g, ok := t.globals[m.GlobalID]
			if !ok {
				g = &Player{ID: m.GlobalID}
				t.globals[m.GlobalID] = g
			}
			g.Name, g.ServerID, g.CombatPower, g.Identified = m.Name, m.ServerID, m.CombatPower, true
			for _, p := range t.players {
				if p.GlobalID == m.GlobalID || (p.GlobalID == 0 && p.Name == m.Name) {
					p.GlobalID = m.GlobalID
					applyIdentity(p, g)
				}
			}
		}
	case proto.SkillCdEvent:
		for _, sk := range e.Skills {
			f := skillFamily(sk)
			t.recentCasts[f] = now
			for _, h := range t.recentHits {
				if h.family == f && absDur(now.Sub(h.at)) <= castWindow {
					t.voteSelf(h.actor)
				}
			}
		}
	case proto.SummonEvent:
		if e.SummonID != e.OwnerID {
			t.summons[e.SummonID] = e.OwnerID
		}
	case proto.MobInfoEvent:
		m := t.mob(e.EntityID)
		m.Code = e.MobCode
		if e.MaxHP > 0 {
			m.MaxHP = e.MaxHP
			m.HP = e.MaxHP
		}
		m.Dead = false
		if info, ok := t.db.Mob(e.MobCode); ok {
			m.Name, m.IsBoss, m.IsDummy = info.Name, info.IsBoss, info.IsDummy
		}
	case proto.HpEvent:
		m, ok := t.mobs[e.EntityID]
		if !ok {
			inFight := t.cur != nil && t.cur.Targets[e.EntityID] != nil
			if e.HP < 1_000_000 && !inFight {
				return
			}
			m = t.mob(e.EntityID)
		}
		m.HP = e.HP
		if m.HP > m.MaxHP {
			m.MaxHP = m.HP
		}
		if e.HP <= 0 {
			t.onTargetDeath(e.EntityID, now)
		}
	case proto.DeathEvent:
		if _, isPlayer := t.players[e.EntityID]; isPlayer {
			if t.cur != nil {
				t.cur.Deaths[e.EntityID]++
			}
			return
		}
		t.onTargetDeath(e.EntityID, now)
	}
}

// deadGrace: late hits on a target that just died are ignored for this long,
// so trailing packets don't open a new, almost empty encounter.
const deadGrace = 2 * time.Second

// recentWindow: a target hit within this window counts as "still being fought".
const recentWindow = 4 * time.Second

func (t *Tracker) onTargetDeath(id int, now time.Time) {
	inFight := t.cur != nil && t.cur.Targets[id] != nil
	m, known := t.mobs[id]
	if !known {
		if !inFight {
			return
		}
		m = t.mob(id)
	}
	if m.Dead {
		return
	}
	m.Dead, m.HP, m.DiedAt = true, 0, now
	if !inFight {
		return
	}
	if m.IsBoss || m.IsDummy {
		if m.IsBoss && t.dungeonID != 0 && t.dungeonFrozen.IsZero() {
			t.dungeonFrozen = now // resumes if the party fights again in the same run
		}
		t.endCurrent("chefe derrotado")
		return
	}
	// Close the fight when nothing else alive is still being hit, so the next
	// mob starts a fresh meter instead of being buried under the previous one.
	for tid, tg := range t.cur.Targets {
		if tid == id {
			continue
		}
		if o, ok := t.mobs[tid]; ok && o.Dead {
			continue
		}
		if now.Sub(tg.LastHit) <= recentWindow {
			return
		}
	}
	t.endCurrent("alvo derrotado")
}

func applyIdentity(p, g *Player) {
	if g.Identified {
		p.Name = g.Name
		p.Identified = true
	}
	if g.ServerID > 0 {
		p.ServerID = g.ServerID
	}
	if g.CombatPower > 0 {
		p.CombatPower = g.CombatPower
	}
	if p.ClassID == 0 {
		p.ClassID = g.ClassID
	}
	p.IsSelf = p.IsSelf || g.IsSelf
	p.GlobalID = g.ID
}

func (t *Tracker) player(id int) *Player {
	p, ok := t.players[id]
	if !ok {
		p = &Player{ID: id}
		t.players[id] = p
	}
	return p
}

func (t *Tracker) mob(id int) *MobState {
	m, ok := t.mobs[id]
	if !ok {
		m = &MobState{ID: id}
		t.mobs[id] = m
	}
	return m
}

func (t *Tracker) onPlayerInfo(e proto.PlayerInfoEvent) {
	if old, ok := t.players[e.EntityID]; ok && old.Identified && old.Name != e.Name {
		delete(t.players, e.EntityID) // id was reused by someone else
	}
	p := t.player(e.EntityID)
	p.Name = e.Name
	p.Identified = true
	if e.ServerID > 0 {
		p.ServerID = e.ServerID
	}
	if e.CombatPower > 0 {
		p.CombatPower = int64(e.CombatPower)
	}
	if e.IsSelf {
		t.selfName = e.Name
		t.selfID = e.EntityID
		for _, q := range t.players {
			q.IsSelf = q.Name == e.Name
		}
	}
	if p.Name == t.selfName {
		p.IsSelf = true
	}
	// fallback link by name
	if p.GlobalID == 0 {
		for _, g := range t.globals {
			if g.Identified && g.Name == p.Name {
				applyIdentity(p, g)
				break
			}
		}
	}
}

const castWindow = 2 * time.Second

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// skillFamily strips the specialisation suffix so a cooldown entry and the
// damage it caused compare equal (11010231 and 11010000 -> 1101).
func skillFamily(code int) int {
	if code >= 10_000_000 {
		return code / 10_000
	}
	return code
}

// noteHit remembers who used which skill, for matching with our cooldowns.
func (t *Tracker) noteHit(actor, code int, now time.Time) {
	if t.selfID != 0 && t.selfName != "" {
		return // the server told us who we are
	}
	f := skillFamily(code)
	if at, ok := t.recentCasts[f]; ok && absDur(now.Sub(at)) <= castWindow {
		t.voteSelf(actor)
		return
	}
	// keep a short history for cooldown packets that arrive after the hit
	keep := t.recentHits[:0]
	for _, h := range t.recentHits {
		if now.Sub(h.at) <= castWindow {
			keep = append(keep, h)
		}
	}
	t.recentHits = append(keep, castHit{actor, f, now})
	if len(t.recentHits) > 512 {
		t.recentHits = t.recentHits[len(t.recentHits)-512:]
	}
}

// voteSelf: an actor's hit matched one of our own cooldowns. After a few
// matches, and clearly ahead of anyone else, that actor is us.
func (t *Tracker) voteSelf(actor int) {
	if _, summon := t.summons[actor]; summon {
		return
	}
	t.selfVotes[actor]++
	second := 0
	for id, v := range t.selfVotes {
		if id != actor {
			second = max(second, v)
		}
	}
	best := t.selfVotes[actor]
	if best >= 3 && best >= 2*second && t.selfID != actor {
		t.setSelf(actor)
	}
}

func (t *Tracker) setSelf(id int) {
	t.selfID = id
	for pid, q := range t.players {
		q.IsSelf = pid == id || (t.selfName != "" && q.Name == t.selfName)
	}
	p := t.player(id)
	p.IsSelf = true
	t.applyRememberedName(p)
}

// applyRememberedName gives "you" your name from the last session when the
// server hasn't sent it yet (it only does on teleport / dungeon entry).
func (t *Tracker) applyRememberedName(p *Player) {
	if p.Identified || t.rememberedName == "" {
		return
	}
	if t.rememberedClass != 0 && p.ClassID != 0 && p.ClassID != t.rememberedClass {
		return // a different character of yours
	}
	p.Name = t.rememberedName
}

// Remember seeds the name/class of your character from a previous session.
func (t *Tracker) Remember(name string, classID int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rememberedName, t.rememberedClass = name, classID
}

// SelfIdentity returns your character's name and class once the server told us.
func (t *Tracker) SelfIdentity() (string, int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.selfID == 0 {
		return "", 0
	}
	p := t.players[t.selfID]
	if p == nil || !p.Identified {
		return "", 0
	}
	return p.Name, p.ClassID
}

// leaveDungeon stops the dungeon timer for good (the run is over).
func (t *Tracker) leaveDungeon(now time.Time) {
	if t.dungeonID == 0 {
		return
	}
	if t.dungeonFrozen.IsZero() {
		end := now
		// if the last fight was a while ago, the run really ended then
		if !t.lastCombat.IsZero() && t.lastCombat.After(t.dungeonSince) && now.Sub(t.lastCombat) > dungeonIdle {
			end = t.lastCombat
		}
		t.dungeonFrozen = end
	}
	t.dungeonID = 0
	t.dungeonLeft = true
}

// ZoneChanged is called when the game switches to another server connection
// (teleport, entering / leaving an instance). Leaving a dungeon ends the run.
func (t *Tracker) ZoneChanged() {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.Now()
	// the connection switch that *enters* the dungeon can arrive just after
	// the party packet that announced it: don't end a run that just started
	if t.dungeonID != 0 && now.Sub(t.dungeonSince) < time.Minute {
		return
	}
	t.leaveDungeon(now)
}

const (
	dungeonIdle     = 3 * time.Minute  // no fighting this long: timer pauses at the last fight
	dungeonShowDone = 10 * time.Minute // a finished run stays on screen this long
)

// dungeonTime returns what the DG timer shows: elapsed time, whether it's
// stopped, and whether to show it at all.
func (t *Tracker) dungeonTime(now time.Time) (time.Duration, bool, bool) {
	if t.dungeonSince.IsZero() {
		return 0, false, false
	}
	if t.dungeonLeft {
		if now.Sub(t.dungeonFrozen) > dungeonShowDone {
			return 0, false, false
		}
		return t.dungeonFrozen.Sub(t.dungeonSince), true, true
	}
	if !t.dungeonFrozen.IsZero() {
		return t.dungeonFrozen.Sub(t.dungeonSince), true, true
	}
	if !t.lastCombat.IsZero() && t.lastCombat.After(t.dungeonSince) && now.Sub(t.lastCombat) > dungeonIdle {
		return t.lastCombat.Sub(t.dungeonSince), true, true
	}
	return now.Sub(t.dungeonSince), false, true
}

// isRealPlayer: a known player character (not a summon / spirit, not a mob).
func (t *Tracker) isRealPlayer(id int) bool {
	p, ok := t.players[id]
	if !ok {
		return false
	}
	if _, summon := t.summons[id]; summon {
		return false
	}
	return p.Identified || (p.ClassID != 0 && p.ClassID != 10)
}

// encounter returns the running encounter, closing it if idle and opening a
// new one when create is true.
func (t *Tracker) encounter(now time.Time, create bool) *Encounter {
	if create {
		t.lastCombat = now
		if t.dungeonID != 0 && !t.dungeonFrozen.IsZero() && now.Sub(t.dungeonFrozen) > deadGrace {
			t.dungeonFrozen = time.Time{} // next boss in the same run: keep counting
		}
	}
	if t.cur != nil && now.Sub(t.cur.LastHit) > t.IdleTimeout {
		t.endCurrent("ocioso")
	}
	if t.cur == nil && create {
		t.nextID++
		t.cur = &Encounter{
			ID: t.nextID, Start: now, LastHit: now, LastAny: now,
			Targets: map[int]*targetAgg{}, Heals: map[int]*actorAgg{},
			Taken: map[int]*actorAgg{}, Deaths: map[int]int{},
		}
	}
	return t.cur
}

func addHit(m map[int]*actorAgg, id int, e proto.DamageEvent, key int, name string, isPet bool) {
	a := m[id]
	if a == nil {
		a = &actorAgg{Skills: map[int]*skillAgg{}}
		m[id] = a
	}
	a.Damage += e.Damage
	a.Hits++
	if e.Crit {
		a.Crits++
	}
	if e.Back {
		a.Back++
	}
	if e.Perfect {
		a.Perfect++
	}
	if e.Double {
		a.Double++
	}
	if e.Damage > a.MaxHit {
		a.MaxHit = e.Damage
	}
	s := a.Skills[key]
	if s == nil {
		s = &skillAgg{Name: name, IsDot: e.Dot, IsPet: isPet}
		a.Skills[key] = s
	}
	s.Hits++
	s.Damage += e.Damage
	if e.Crit {
		s.Crits++
	}
	if e.Damage > s.MaxHit {
		s.MaxHit = e.Damage
	}
}

// sourceName names whoever dealt damage to a player (for the tank tab).
func (t *Tracker) sourceName(actor int) (int, string) {
	if m, ok := t.mobs[actor]; ok && m.Code != 0 {
		if m.Name != "" {
			return m.Code, m.Name
		}
		return m.Code, fmt.Sprintf("Monstro %d", m.Code)
	}
	if p, ok := t.players[actor]; ok && p.Identified {
		return 2_000_000_000 + actor%100_000_000, p.Name + " (PvP)"
	}
	return 0, "Monstros"
}

func (t *Tracker) onDamage(e proto.DamageEvent, now time.Time) {
	if e.Damage <= 0 {
		return
	}
	db := t.db

	// ---- damage taken from monsters
	if e.NPC {
		if !t.isRealPlayer(e.TargetID) {
			return
		}
		enc := t.encounter(now, true)
		enc.LastHit, enc.LastAny = now, now
		key, name := t.sourceName(e.ActorID)
		addHit(enc.Taken, e.TargetID, e, key, name, false)
		return
	}

	theo := gamedata.IsTheostone(e.SkillCode)

	// ---- healing (only inside a running fight)
	if !theo && db.IsHealing(e.SkillCode) {
		enc := t.encounter(now, false)
		if enc == nil {
			return
		}
		enc.LastAny = now
		healer := e.ActorID
		isPet := false
		if owner, ok := t.summons[healer]; ok {
			healer, isPet = owner, true
		}
		if c := db.ClassBySkill(e.SkillCode); c != nil && c.ID != 10 {
			if p := t.player(healer); p.ClassID == 0 {
				p.ClassID = c.ID
			}
		}
		sid, sname := db.SkillName(e.SkillCode)
		if e.Dot {
			sid = -sid
		}
		addHit(enc.Heals, e.ActorID, e, sid, sname, isPet)
		return
	}

	if e.ActorID == e.TargetID {
		return
	}
	if !theo && e.Dot && !db.IsDot(e.SkillCode) {
		return
	}

	// resolve class for the actor
	actor := e.ActorID
	isPet := false
	if owner, ok := t.summons[actor]; ok {
		actor, isPet = owner, true
	}
	p := t.player(actor)
	if !theo {
		c := db.ClassBySkill(e.SkillCode)
		if c == nil {
			return
		}
		if c.ID == 10 && !isPet { // spirit skill from an unregistered summon
			isPet = true
		}
		if p.ClassID == 0 && c.ID != 10 && !isPet {
			p.ClassID = c.ID
		}
	}

	if !isPet {
		t.noteHit(e.ActorID, e.SkillCode, now)
	}
	if p.IsSelf {
		t.applyRememberedName(p)
	}

	// hits on a freshly dead target are trailing packets; later ones mean the id was reused
	if m, ok := t.mobs[e.TargetID]; ok && m.Dead {
		if now.Sub(m.DiedAt) < deadGrace {
			return
		}
		m.Dead = false
	}

	enc := t.encounter(now, true)
	enc.LastHit, enc.LastAny = now, now

	tg := enc.Targets[e.TargetID]
	if tg == nil {
		tg = &targetAgg{ID: e.TargetID, FirstHit: now, Actors: map[int]*actorAgg{}}
		enc.Targets[e.TargetID] = tg
	}
	tg.LastHit = now
	tg.Total += e.Damage

	sid, sname := db.SkillName(e.SkillCode)
	if e.Dot {
		sid = -sid // keep DoT ticks separate from the direct hit
	}
	addHit(tg.Actors, e.ActorID, e, sid, sname, isPet)

	// PvP: a player hit by a player also shows up in the tank tab
	if t.isRealPlayer(e.TargetID) {
		key, name := t.sourceName(e.ActorID)
		addHit(enc.Taken, e.TargetID, e, key, name, false)
	}
}

func (t *Tracker) endCurrent(cause string) {
	if t.cur == nil {
		return
	}
	t.cur.Ended = true
	t.cur.EndCause = cause
	t.history = append(t.history, t.cur)
	if len(t.history) > 20 {
		t.history = t.history[len(t.history)-20:]
	}
	t.cur = nil
}

// ---- snapshots --------------------------------------------------------------

// Metric selects what the rows measure.
type Metric int

const (
	MetricDamage Metric = iota // damage dealt (DPS)
	MetricHeal                 // healing done (HPS)
	MetricTaken                // damage taken (tank)
)

func (m Metric) Label() string {
	switch m {
	case MetricHeal:
		return "HPS"
	case MetricTaken:
		return "DTPS"
	}
	return "DPS"
}

type SkillRow struct {
	Name     string
	Damage   int64 // amount (damage, heal or damage taken)
	Hits     int
	CritRate float64
	MaxHit   int64
	Pct      float64
	IsDot    bool
	IsPet    bool
}

type Row struct {
	ID          int
	Name        string
	ServerName  string
	ClassID     int
	ClassName   string
	Color       uint32
	Damage      int64   // amount for the selected metric
	DPS         float64 // amount per second for the selected metric
	Pct         float64
	Hits        int
	CritRate    float64
	BackRate    float64
	MaxHit      int64
	Deaths      int
	CombatPower int64
	IsSelf      bool
	InParty     bool
	Skills      []SkillRow
}

type Snapshot struct {
	EncounterID int
	Active      bool
	Mode        ViewMode
	Metric      Metric
	TargetName  string
	TargetHP    int64
	TargetMaxHP int64
	TargetDead  bool
	IsBoss      bool
	Duration    time.Duration
	TotalDamage int64   // total for the selected metric
	PartyDPS    float64 // total per second for the selected metric
	Rows        []Row
	PartyKnown  bool
	SelfKnown   bool // we know which player is you
	SelfName    string
	History     int           // number of encounters available
	HistoryIdx  int           // 0 = live/latest, 1 = previous, ...
	Dungeon     time.Duration // time since entering the current dungeon (0 = unknown)
	DungeonDone bool          // timer stopped (boss killed, idle or left the dungeon)
}

// SnapshotOptions control what the UI wants to see.
type SnapshotOptions struct {
	Mode      ViewMode
	Metric    Metric
	PartyOnly bool
	Back      int // 0 = current (or last finished), 1 = one before, ...
}

type merged struct {
	a      actorAgg
	skills map[int]*skillAgg
}

func mergeInto(byOwner map[int]*merged, id, rawID int, a *actorAgg) {
	m := byOwner[id]
	if m == nil {
		m = &merged{skills: map[int]*skillAgg{}}
		byOwner[id] = m
	}
	m.a.Damage += a.Damage
	m.a.Hits += a.Hits
	m.a.Crits += a.Crits
	m.a.Back += a.Back
	if a.MaxHit > m.a.MaxHit {
		m.a.MaxHit = a.MaxHit
	}
	for sid, s := range a.Skills {
		key := sid
		if id != rawID {
			key = sid + 1_000_000_000 // keep pet skills apart
		}
		ms := m.skills[key]
		if ms == nil {
			cp := *s
			cp.Hits, cp.Damage, cp.Crits, cp.MaxHit = 0, 0, 0, 0
			cp.IsPet = cp.IsPet || id != rawID
			ms = &cp
			m.skills[key] = ms
		}
		ms.Hits += s.Hits
		ms.Damage += s.Damage
		ms.Crits += s.Crits
		if s.MaxHit > ms.MaxHit {
			ms.MaxHit = s.MaxHit
		}
	}
}

// Snapshot builds a view of the current (or a past) encounter.
func (t *Tracker) Snapshot(o SnapshotOptions) Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.Now()

	if t.cur != nil && now.Sub(t.cur.LastHit) > t.IdleTimeout {
		t.endCurrent("ocioso")
	}

	var list []*Encounter
	list = append(list, t.history...)
	if t.cur != nil {
		list = append(list, t.cur)
	}
	snap := Snapshot{Mode: o.Mode, Metric: o.Metric, SelfName: t.selfName, PartyKnown: len(t.party) > 0, History: len(list)}
	if d, stopped, show := t.dungeonTime(now); show {
		snap.Dungeon, snap.DungeonDone = max(d, time.Second), stopped
	}
	if len(list) == 0 {
		return snap
	}
	idx := len(list) - 1 - o.Back
	if idx < 0 {
		idx = 0
	}
	snap.HistoryIdx = len(list) - 1 - idx
	enc := list[idx]
	snap.EncounterID = enc.ID
	snap.Active = !enc.Ended

	main := t.mainTarget(enc)
	if main != nil {
		if m, ok := t.mobs[main.ID]; ok {
			snap.TargetName = m.Name
			snap.TargetHP, snap.TargetMaxHP, snap.TargetDead = m.HP, m.MaxHP, m.Dead
			snap.IsBoss = m.IsBoss || m.IsDummy
		}
		if snap.TargetName == "" {
			if p, ok := t.players[main.ID]; ok && p.Identified {
				snap.TargetName = p.Name
			} else {
				snap.TargetName = fmt.Sprintf("Alvo #%d", main.ID)
			}
		}
	}

	byOwner := map[int]*merged{}
	var dur time.Duration
	switch o.Metric {
	case MetricDamage:
		var targets []*targetAgg
		if o.Mode == ViewMainTarget {
			if main != nil {
				targets = []*targetAgg{main}
			}
		} else {
			for _, tg := range enc.Targets {
				targets = append(targets, tg)
			}
		}
		var first, last time.Time
		for _, tg := range targets {
			if first.IsZero() || tg.FirstHit.Before(first) {
				first = tg.FirstHit
			}
			if tg.LastHit.After(last) {
				last = tg.LastHit
			}
		}
		dur = last.Sub(first)
		for _, tg := range targets {
			for rawID, a := range tg.Actors {
				id := rawID
				if owner, ok := t.summons[rawID]; ok {
					id = owner
				}
				mergeInto(byOwner, id, rawID, a)
			}
		}
	case MetricHeal, MetricTaken:
		src := enc.Heals
		if o.Metric == MetricTaken {
			src = enc.Taken
		}
		for rawID, a := range src {
			id := rawID
			if owner, ok := t.summons[rawID]; ok && o.Metric == MetricHeal {
				id = owner
			}
			mergeInto(byOwner, id, rawID, a)
		}
		dur = enc.LastAny.Sub(enc.Start)
	}
	if dur < time.Second {
		dur = time.Second
	}
	snap.Duration = dur

	selfKnown := false
	for _, p := range t.players {
		selfKnown = selfKnown || p.IsSelf
	}
	snap.SelfKnown = selfKnown

	var total int64
	for id, m := range byOwner {
		p := t.players[id]
		if p == nil {
			p = &Player{ID: id}
		}
		inParty := p.IsSelf || (p.GlobalID != 0 && t.party[p.GlobalID]) || (p.Name != "" && t.partyName[strings.ToLower(p.Name)])
		if o.PartyOnly {
			switch {
			case len(t.party) > 0 && !inParty:
				continue
			case len(t.party) == 0 && selfKnown && !p.IsSelf: // solo: just you
				continue
			}
		}
		row := Row{
			ID: id, Damage: m.a.Damage, Hits: m.a.Hits, MaxHit: m.a.MaxHit, Deaths: enc.Deaths[id],
			IsSelf: p.IsSelf, InParty: inParty, ClassID: p.ClassID, CombatPower: p.CombatPower,
			ServerName: proto.ServerName(p.ServerID),
		}
		if m.a.Hits > 0 {
			row.CritRate = float64(m.a.Crits) / float64(m.a.Hits)
			row.BackRate = float64(m.a.Back) / float64(m.a.Hits)
		}
		if c := t.db.Classes[p.ClassID]; c != nil {
			row.ClassName, row.Color = c.Short, c.Color
		} else {
			row.ClassName, row.Color = "?", 0x9AA0A6
		}
		switch {
		case (p.Identified || p.IsSelf) && p.Name != "":
			row.Name = p.Name
		case p.IsSelf:
			row.Name = "Você"
		case row.ClassName != "?":
			row.Name = fmt.Sprintf("%s #%d", row.ClassName, id)
		default:
			row.Name = fmt.Sprintf("Jogador #%d", id)
		}
		for _, s := range m.skills {
			sr := SkillRow{Name: s.Name, Damage: s.Damage, Hits: s.Hits, MaxHit: s.MaxHit, IsDot: s.IsDot, IsPet: s.IsPet}
			if s.Hits > 0 {
				sr.CritRate = float64(s.Crits) / float64(s.Hits)
			}
			if m.a.Damage > 0 {
				sr.Pct = float64(s.Damage) / float64(m.a.Damage)
			}
			row.Skills = append(row.Skills, sr)
		}
		sort.Slice(row.Skills, func(i, j int) bool { return row.Skills[i].Damage > row.Skills[j].Damage })
		total += row.Damage
		snap.Rows = append(snap.Rows, row)
	}
	secs := dur.Seconds()
	for i := range snap.Rows {
		r := &snap.Rows[i]
		r.DPS = float64(r.Damage) / secs
		if total > 0 {
			r.Pct = float64(r.Damage) / float64(total)
		}
	}
	sort.Slice(snap.Rows, func(i, j int) bool {
		if snap.Rows[i].Damage != snap.Rows[j].Damage {
			return snap.Rows[i].Damage > snap.Rows[j].Damage
		}
		return snap.Rows[i].ID < snap.Rows[j].ID
	})
	snap.TotalDamage = total
	snap.PartyDPS = float64(total) / secs
	return snap
}

// mainTarget picks what the overlay focuses on: a boss / training dummy if
// there is one; otherwise a target that is alive and being hit right now
// (most damage among those); otherwise the most damaged target.
func (t *Tracker) mainTarget(enc *Encounter) *targetAgg {
	rank := func(tg *targetAgg) int {
		m := t.mobs[tg.ID]
		if m != nil && (m.IsBoss || m.IsDummy) {
			return 2
		}
		alive := m == nil || !m.Dead
		if alive && enc.LastHit.Sub(tg.LastHit) <= recentWindow {
			return 1
		}
		return 0
	}
	var best *targetAgg
	bestRank := -1
	for _, tg := range enc.Targets {
		r := rank(tg)
		if r > bestRank || (r == bestRank && (tg.Total > best.Total || (tg.Total == best.Total && tg.ID < best.ID))) {
			best, bestRank = tg, r
		}
	}
	return best
}
