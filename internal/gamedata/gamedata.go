// Package gamedata holds the static skill / class / mob tables (embedded JSON)
// and the rules for turning raw packet skill codes into known skills.
package gamedata

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"sync"

	"aethermeter/internal/proto"
)

//go:embed assets/*.json
var assets embed.FS

type Class struct {
	ID    int
	Name  string // English name from the data files
	Short string // short PT-BR label used in the overlay
	Color uint32 // 0xRRGGBB
}

type Skill struct {
	ID      int
	ClassID int
	Name    string
}

type Mob struct {
	Name    string `json:"name"`
	IsBoss  bool   `json:"isBoss"`
	IsDummy bool   `json:"isDummy"`
}

type DB struct {
	Classes map[int]*Class
	skills  map[int]*Skill
	dots    map[int]bool
	heals   map[int]bool
	Mobs    map[int]Mob
}

var (
	once sync.Once
	db   *DB
	err  error
)

// Get returns the singleton database, loading it on first use.
func Get() *DB {
	once.Do(func() { db, err = load() })
	if err != nil {
		panic(err)
	}
	return db
}

// Overlay presentation for each class.
var classStyle = map[int]struct {
	short string
	color uint32
}{
	10: {"Espírito", 0x7FD1C7},
	11: {"Gladiador", 0xC9864B},
	12: {"Templário", 0xD4B25A},
	13: {"Assassino", 0xB06BD8},
	14: {"Ranger", 0x6DBE5A},
	15: {"Feiticeiro", 0x5B8DEF},
	16: {"Elementalista", 0x3FC1C9},
	17: {"Clérigo", 0xEDEDED},
	18: {"Chanter", 0x9BD86B},
	19: {"Brawler", 0xE0655B},
}

func load() (*DB, error) {
	d := &DB{
		Classes: map[int]*Class{},
		skills:  map[int]*Skill{},
		dots:    map[int]bool{},
		heals:   map[int]bool{},
		Mobs:    map[int]Mob{},
	}

	// classes
	var cf struct {
		Classes []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"classes"`
	}
	if err := readJSON("assets/classes.json", &cf); err != nil {
		return nil, err
	}
	for _, c := range cf.Classes {
		st := classStyle[c.ID]
		if st.short == "" {
			st.short, st.color = c.Name, 0xAAAAAA
		}
		d.Classes[c.ID] = &Class{ID: c.ID, Name: c.Name, Short: st.short, Color: st.color}
	}

	// skills: order matters (normalisation consults already-loaded entries),
	// so walk the JSON object token by token instead of into a Go map.
	raw, err := assets.ReadFile("assets/skills.json")
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil { // {
		return nil, err
	}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var name string
		if err := dec.Decode(&name); err != nil {
			return nil, err
		}
		full, err := strconv.Atoi(kt.(string))
		if err != nil {
			continue
		}
		var prefix int
		if full/1_000_000 == 30 { // theostone
			prefix = full / 10
		} else {
			prefix = d.Normalize(full)
		}
		if _, dup := d.skills[prefix]; dup {
			continue
		}
		d.skills[prefix] = &Skill{ID: prefix, ClassID: full / 1_000_000, Name: name}
	}

	var dots []int
	if err := readJSON("assets/dot_skill_ids.json", &dots); err != nil {
		return nil, err
	}
	for _, id := range dots {
		d.dots[id] = true
	}
	var heals []int
	if err := readJSON("assets/healing_skill_ids.json", &heals); err != nil {
		return nil, err
	}
	for _, id := range heals {
		d.heals[d.Normalize(id)] = true
	}
	// Heal skills of the support classes, recognised by name, complement the
	// short hand-made list above.
	for id, sk := range d.skills {
		c := sk.ClassID
		if (c == 16 || c == 17 || c == 18) && healName.MatchString(sk.Name) && !notHealName.MatchString(sk.Name) {
			d.heals[id] = true
		}
	}

	mobs := map[string]Mob{}
	if err := readJSON("assets/mobs.json", &mobs); err != nil {
		return nil, err
	}
	for k, v := range mobs {
		if id, err := strconv.Atoi(k); err == nil {
			d.Mobs[id] = v
		}
	}
	return d, nil
}

var (
	healName    = regexp.MustCompile(`(?i)\bheal(ing)?\b|recover|recuperat|regenerat|salvation|light of protection|spirit communion|benediction`)
	notHealName = regexp.MustCompile(`(?i)block|enhance|increase|boost|\(unused\)`)
)

func readJSON(name string, v any) error {
	b, err := assets.ReadFile(name)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

// ---- skill code rules -------------------------------------------------------

func IsTheostone(code int) bool { return code >= 3_000_000 && code <= 3_099_999 }

// Classify tells the decoder whether a raw skill code belongs to a player
// (incl. pets/theostones), to a monster, or is garbage.
func Classify(code int) proto.SkillKind {
	if Plausible(code) {
		return proto.SkillPlayer
	}
	if code >= 1_000_000 && code <= 9_999_999 {
		return proto.SkillNPC
	}
	return proto.SkillInvalid
}

// Plausible mirrors the reference validation of raw damage skill codes.
func Plausible(code int) bool {
	if code < 1 || code > 299_999_999 {
		return false
	}
	if IsTheostone(code) {
		return true
	}
	if code >= 1_000_000 && code <= 9_999_999 { // NPC skills
		return false
	}
	if code >= 100_000 && code < 200_000 { // pets / spirits
		return true
	}
	if code >= 11_000_000 && code < 20_000_000 {
		return true
	}
	for _, off := range []int{0, 10, 20, 30, 40, 50, 120, 130, 140, 150, 230, 240, 250, 340, 350, 450} {
		b := code - off
		if (b >= 11_000_000 && b < 19_000_000) || (b >= 100_000 && b < 200_000) {
			return true
		}
	}
	return false
}

func inRange(code int) bool {
	c := uint32(code)
	return (c >= 11_000_000 && c < 20_000_000) ||
		(c >= 1_000_000 && c < 10_000_000) ||
		(c >= 100_000 && c < 200_000) ||
		(c >= 29_000_000 && c < 30_000_000)
}

func (d *DB) has(code int) bool { _, ok := d.skills[code]; return ok }

func (d *DB) toBase(code int) int {
	if code < 29_000_000 || code >= 30_000_000 {
		num := code / 10000 * 10000
		if num != code && d.has(num) {
			if !d.has(code) {
				return num
			}
			if d.skills[num].Name == d.skills[code].Name {
				return num
			}
		}
	}
	return code
}

// Normalize maps a raw packet skill code to the base skill id (0 if unknown range).
func (d *DB) Normalize(raw int) int {
	if raw <= 0 {
		return 0
	}
	if c1 := int64(raw)*10 + 1; c1 < 1<<31 && d.has(int(c1)) {
		if n := d.toBase(int(c1)); inRange(n) {
			return n
		}
	}
	if c2 := int64(raw) * 10; c2 < 1<<31 && d.has(int(c2)) {
		if n := d.toBase(int(c2)); inRange(n) {
			return n
		}
	}
	if n := d.toBase(raw); inRange(n) {
		return n
	}
	if raw%100 == 0 {
		if n := d.toBase(raw / 100); inRange(n) {
			return n
		}
	}
	return 0
}

// SkillName returns a display name for a raw skill code.
func (d *DB) SkillName(raw int) (id int, name string) {
	if IsTheostone(raw) {
		if s, ok := d.skills[raw]; ok {
			return raw, s.Name
		}
		return raw, fmt.Sprintf("Theostone %d", raw)
	}
	p := d.Normalize(raw)
	if s, ok := d.skills[p]; ok {
		return p, s.Name
	}
	if p == 0 {
		p = raw
	}
	return p, fmt.Sprintf("Skill %d", raw)
}

// ClassBySkill: the class id is the first two decimal digits of the raw code.
func (d *DB) ClassBySkill(raw int) *Class {
	s := strconv.Itoa(raw)
	if len(s) < 2 {
		return nil
	}
	id, _ := strconv.Atoi(s[:2])
	return d.Classes[id]
}

func (d *DB) IsHealing(raw int) bool { return d.heals[d.Normalize(raw)] }
func (d *DB) IsDot(raw int) bool     { return d.dots[raw] }

func (d *DB) Mob(code int) (Mob, bool) { m, ok := d.Mobs[code]; return m, ok }
