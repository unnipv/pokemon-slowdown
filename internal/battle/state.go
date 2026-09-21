// Package battle owns the deterministic battle domain model. It consumes
// typed showdown events and produces a BattleState that the UI renders. It has
// no knowledge of the terminal, of rendering, or of the network.
package battle

import (
	"strconv"
	"strings"
)

// State is the complete, deterministic state of one battle room.
type State struct {
	RoomID   string
	Format   string
	Tier     string
	Gen      int
	GameType string
	Rated    bool
	RatedMsg string
	Rules    []string

	Turn int

	P1 *Side
	P2 *Side

	// Me is the player ID this client controls ("p1"/"p2"), or "" when
	// spectating.
	Me string

	Weather    string
	Terrain    string
	Field      map[string]bool
	PseudoWx   map[string]bool
	SideConds  map[string]map[string]int
	Conditions map[string]int

	// Request is the most recent choice request, or nil.
	Request *Request

	// AwaitingChoice is true while the server expects a choice from us.
	AwaitingChoice bool

	TimerOn      bool
	TimerMsg     string
	TimerSeconds int

	Ended  bool
	Winner string
	Tie    bool

	// TeamPreview is true during team preview.
	TeamPreview bool

	Log []LogEntry
}

// LogEntry is one human-readable line of battle history.
type LogEntry struct {
	Turn int
	Kind string
	Text string
}

// Side is one player's half of the field.
type Side struct {
	ID         string
	Name       string
	Avatar     string
	Rating     string
	TeamSize   int
	Party      []*Pokemon
	Conditions map[string]int
}

// Pokemon is a single Pokémon on a side.
type Pokemon struct {
	Ident         string
	SideID        string
	Slot          string // "a", "b", ... while active
	Position      int    // slot index while active, -1 otherwise
	Name          string
	Species       string
	Level         int
	Gender        string
	Shiny         bool
	TeraType      string
	Terastallized bool

	HP        int
	MaxHP     int
	HPPercent int
	Status    string
	Fainted   bool
	Active    bool

	Item        string
	Ability     string
	BaseAbility string

	// Stats are the absolute, current stat values as reported by the server
	// for our own Pokémon (after level, nature, EVs, IVs and any item or
	// ability that modifies them at switch-in). Empty for the opponent, whose
	// stats are not revealed.
	Stats map[string]int
	// SeenMoves records moves this Pokémon has actually used. For the opponent
	// this is the only legitimate source of move information.
	SeenMoves []string
	// MoveIDs is the team's known move IDs from a choice request. The server
	// lists every party member's moves this way, but only the active Pokémon
	// get PP and display names; MoveIDs lets the inspect overlay show a bench
	// Pokémon's moveset too.
	MoveIDs []string

	Boosts    map[string]int
	Volatiles map[string]bool
	Moves     []Move

	// Revealed is false until the Pokémon is seen by this client.
	Revealed bool
}

// Move is one of our own Pokémon's moves, as reported by a choice request.
type Move struct {
	ID             string
	Name           string
	PP             int
	MaxPP          int
	Target         string
	Disabled       bool
	DisabledReason string
}

// NewState returns an empty battle state ready to consume events.
func NewState(roomID string) *State {
	return &State{
		RoomID:     roomID,
		Field:      map[string]bool{},
		PseudoWx:   map[string]bool{},
		SideConds:  map[string]map[string]int{},
		Conditions: map[string]int{},
		P1:         newSide("p1"),
		P2:         newSide("p2"),
	}
}

func newSide(id string) *Side {
	return &Side{ID: id, Conditions: map[string]int{}}
}

// Side returns the side for a player ID ("p1"/"p2"), or nil.
func (s *State) Side(id string) *Side {
	switch id {
	case "p1":
		return s.P1
	case "p2":
		return s.P2
	}
	return nil
}

// MySide returns the side this client controls, or nil when spectating.
func (s *State) MySide() *Side { return s.Side(s.Me) }

// Opponent returns the opposing side, or nil.
func (s *State) Opponent() *Side {
	switch s.Me {
	case "p1":
		return s.P2
	case "p2":
		return s.P1
	}
	return nil
}

// Find returns the Pokémon identified by a protocol ident such as
// "p1a: Gengar", or nil.
func (s *State) Find(ident string) *Pokemon {
	sideID, _, name := parseIdent(ident)
	side := s.Side(sideID)
	if side == nil {
		return nil
	}
	for _, p := range side.Party {
		if p.Ident == ident {
			return p
		}
	}
	// Fall back to matching by the visible name.
	for _, p := range side.Party {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// ensure returns the Pokémon for an ident, creating a party slot if the
// Pokémon has not been seen before.
func (s *State) ensure(ident string) *Pokemon {
	if p := s.Find(ident); p != nil {
		return p
	}
	sideID, slot, name := parseIdent(ident)
	side := s.Side(sideID)
	if side == nil {
		return nil
	}
	p := &Pokemon{
		Ident:     ident,
		SideID:    sideID,
		Slot:      slot,
		Position:  slotIndex(slot),
		Name:      name,
		Species:   name,
		Level:     100,
		Boosts:    map[string]int{},
		Volatiles: map[string]bool{},
		Active:    slot != "",
		Revealed:  true,
	}
	side.Party = append(side.Party, p)
	return p
}

// SetIdent re-points a Pokémon at a new ident, used when a Pokémon becomes
// active and gains a position letter.
func (p *Pokemon) SetIdent(ident string) {
	sideID, slot, name := parseIdent(ident)
	p.Ident = ident
	p.SideID = sideID
	p.Slot = slot
	p.Position = slotIndex(slot)
	p.Active = slot != ""
	if name != "" {
		p.Name = name
	}
}

// ActiveParty returns the active Pokémon in slot order.
func (s *Side) ActiveParty() []*Pokemon {
	if s == nil {
		return nil
	}
	var out []*Pokemon
	for _, p := range s.Party {
		if p.Active {
			out = append(out, p)
		}
	}
	return out
}

// ActiveAt returns the active Pokémon in a slot, or nil.
func (s *Side) ActiveAt(i int) *Pokemon {
	if s == nil {
		return nil
	}
	for _, p := range s.Party {
		if p.Active && p.Position == i {
			return p
		}
	}
	return nil
}

// Party returns the non-active Pokémon that have not fainted.
func (s *Side) Bench() []*Pokemon {
	if s == nil {
		return nil
	}
	var out []*Pokemon
	for _, p := range s.Party {
		if !p.Active && !p.Fainted {
			out = append(out, p)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Parsing helpers
// ---------------------------------------------------------------------------

func parseIdent(s string) (side, slot, name string) {
	i := strings.Index(s, ": ")
	if i < 0 {
		return "", "", s
	}
	left := s[:i]
	name = s[i+2:]
	if len(left) >= 2 {
		side = left[:2]
		if len(left) > 2 {
			slot = left[2:]
		}
	}
	return
}

func slotIndex(slot string) int {
	if slot == "" {
		return -1
	}
	return int(slot[0] - 'a')
}

func parseDetails(s string) (species string, level int, gender string, shiny bool, tera string) {
	level = 100
	parts := strings.Split(s, ", ")
	species = strings.TrimSuffix(parts[0], "-*")
	for _, p := range parts[1:] {
		switch {
		case strings.HasPrefix(p, "L"):
			if n, err := strconv.Atoi(p[1:]); err == nil {
				level = n
			}
		case p == "M" || p == "F":
			gender = p
		case p == "shiny":
			shiny = true
		case strings.HasPrefix(p, "tera:"):
			tera = p[len("tera:"):]
		}
	}
	return
}

// parseHP parses "100/100", "62%" or "/48" into current, maximum and percent.
func parseHP(s string) (cur, max, pct int) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, 0
	}
	if strings.HasSuffix(s, "%") {
		pct = atoi(strings.TrimSuffix(s, "%"))
		return pct, 100, pct
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		cur = atoi(s[:i])
		max = atoi(s[i+1:])
		if max > 0 {
			pct = cur * 100 / max
		}
		return cur, max, pct
	}
	return 0, 0, 0
}

// setCondition applies a protocol condition string such as "227/227",
// "62% par" or "0 fnt".
func (p *Pokemon) setCondition(cond string) {
	if cond == "" {
		return
	}
	hp, status := splitHP(cond)
	if hp != "" {
		cur, max, pct := parseHP(hp)
		if max > 0 {
			p.HP, p.MaxHP = cur, max
			if cur > 0 {
				p.Fainted = false
			} else {
				p.Fainted = true
				p.Status = "fnt"
			}
		}
		p.HPPercent = pct
	}
	if status != "" {
		p.Status = status
		if status == "fnt" {
			p.Fainted = true
		}
	}
}

func splitHP(s string) (hp, status string) {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i], strings.TrimSpace(s[i+1:])
	}
	return s, ""
}

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// BoostTotal returns the sum of all positive boosts, used for compact display.
func (p *Pokemon) BoostTotal() int {
	total := 0
	for _, v := range p.Boosts {
		total += v
	}
	return total
}

// RememberMove records a move this Pokémon has used, ignoring duplicates.
func (p *Pokemon) RememberMove(name string) {
	if name == "" {
		return
	}
	for _, m := range p.SeenMoves {
		if m == name {
			return
		}
	}
	p.SeenMoves = append(p.SeenMoves, name)
}

// BoostMultiplier converts a stat stage (-6..+6) into the multiplier the game
// applies to the stat itself.
func BoostMultiplier(stage int) float64 {
	if stage > 6 {
		stage = 6
	}
	if stage < -6 {
		stage = -6
	}
	if stage >= 0 {
		return float64(2+stage) / 2
	}
	return 2 / float64(2-stage)
}

// StatRange returns the minimum and maximum possible value of a stat for a
// species at a level, matching Pokémon Showdown's foe tooltip. The minimum
// assumes 0 IVs and 0 EVs with a hindering nature; the maximum assumes 31 IVs
// and 252 EVs with a beneficial nature. HP ignores nature. Random-battle
// formats use neutral natures, so pass random to drop the 0.9/1.1 multipliers.
func StatRange(base, level int, hp, random bool) (int, int) {
	if base <= 0 {
		return 0, 0
	}
	if level <= 0 {
		level = 100
	}
	if hp {
		min := 2*base*level/100 + level + 10
		max := (2*base+94)*level/100 + level + 10
		return min, max
	}
	minNature, maxNature := 0.9, 1.1
	if random {
		minNature, maxNature = 1, 1
	}
	min := int(float64(2*base*level/100+5) * minNature)
	max := int(float64((2*base+94)*level/100+5) * maxNature)
	return min, max
}

// StatKeys is the canonical stat order for display.
var StatKeys = []string{"hp", "atk", "def", "spa", "spd", "spe"}

// StatLabels maps stat keys to human labels.
var StatLabels = map[string]string{
	"hp": "HP", "atk": "Attack", "def": "Defense",
	"spa": "Sp. Atk", "spd": "Sp. Def", "spe": "Speed",
}
