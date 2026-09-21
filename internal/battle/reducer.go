package battle

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/unnipv/pokemon-slowdown/internal/showdown"
)

// maxLogEntries bounds the in-memory battle history.
const maxLogEntries = 2000

// Reducer applies typed showdown events to a battle State. It is the only
// place that knows how protocol messages change battle state.
type Reducer struct {
	State *State
	// Debug records otherwise-silent events (unknown protocol types) in the
	// battle log so protocol drift can be diagnosed.
	Debug bool
}

// NewReducer returns a reducer for a battle room.
func NewReducer(roomID string) *Reducer {
	return &Reducer{State: NewState(roomID)}
}

// Apply folds one event into the state.
func (r *Reducer) Apply(ev showdown.Event) {
	switch e := ev.(type) {

	// ---- battle initialisation ----
	case showdown.BattlePlayer:
		if s := r.State.Side(e.Player); s != nil {
			s.Name, s.Avatar, s.Rating = e.Name, e.Avatar, e.Rating
		}
	case showdown.BattleTeamSize:
		if s := r.State.Side(e.Player); s != nil {
			s.TeamSize = e.Size
		}
	case showdown.BattleGameType:
		r.State.GameType = e.GameType
	case showdown.BattleGen:
		r.State.Gen = e.Gen
	case showdown.BattleTier:
		r.State.Tier = e.Tier
		r.State.Format = e.Tier
	case showdown.BattleRated:
		r.State.Rated = true
		r.State.RatedMsg = e.Message
	case showdown.BattleRule:
		r.State.Rules = append(r.State.Rules, e.Rule)
	case showdown.BattleClearpoke:
		r.State.TeamPreview = true
		r.State.P1.Party = nil
		r.State.P2.Party = nil
	case showdown.BattlePoke:
		side := r.State.Side(e.Player)
		if side == nil {
			break
		}
		species, _, _, _, _ := parseDetails(e.Details)
		side.Party = append(side.Party, &Pokemon{
			SideID:    e.Player,
			Name:      species,
			Species:   species,
			Level:     100,
			Position:  -1,
			Item:      itemFromFlag(e.Item),
			Boosts:    map[string]int{},
			Volatiles: map[string]bool{},
		})
	case showdown.BattleTeamPreview:
		r.State.TeamPreview = true
	case showdown.BattleStart:
		r.State.TeamPreview = false

	// ---- progress ----
	case showdown.BattleTurn:
		r.State.Turn = e.Turn
		r.log("turn", "— Turn %d —", e.Turn)
	case showdown.BattleRequest:
		r.applyRequest(e.Request)
	case showdown.BattleWin:
		r.State.Ended = true
		r.State.Winner = e.User
		r.State.AwaitingChoice = false
		r.State.Request = nil
		r.log("win", "%s won the battle!", e.User)
	case showdown.BattleTie:
		r.State.Ended = true
		r.State.Tie = true
		r.State.AwaitingChoice = false
		r.State.Request = nil
		r.log("tie", "The battle ended in a tie.")
	case showdown.BattleInactive:
		r.State.TimerOn = e.On
		r.State.TimerMsg = e.Message
		r.State.TimerSeconds = firstInt(e.Message)
		if e.On {
			r.log("timer", "Battle timer: %s", e.Message)
		} else {
			r.log("timer", "Battle timer off.")
		}
	case showdown.BattleUpkeep:
		// Field condition durations are decremented in the UI from their
		// start counts; the protocol does not resend them here.
	case showdown.BattleTimestamp:
		// Server clock; not needed for state.
	case showdown.BattleError:
		r.log("error", "%s", e.Message)
	case showdown.BattleClear:
		r.log("spacer", "")
	case showdown.BattleMessage:
		r.log("message", "%s", e.Message)

	// ---- actions ----
	case showdown.BattleMove:
		p := r.State.Find(e.User)
		name := identName(e.User)
		if p != nil {
			name = r.display(p)
			p.RememberMove(e.Move)
		}
		if e.Tags.Has("still") {
			break
		}
		r.log("move", "%s used %s!", name, e.Move)
	case showdown.BattleSwitch:
		r.switchIn(e.User, e.Details, e.HP, e.Status, e.Drag)
	case showdown.BattleDetailsChange:
		r.applyDetails(e.User, e.Details, e.HP, e.Status)
	case showdown.BattleFormeChange:
		if p := r.State.Find(e.User); p != nil {
			species, _, _, _, _ := parseDetails(e.Species)
			p.Species = species
			p.setCondition(e.HP)
			if e.Status != "" {
				p.Status = e.Status
			}
			r.log("forme", "%s changed forme to %s!", r.display(p), species)
		}
	case showdown.BattleReplace:
		r.applyDetails(e.User, e.Details, e.HP, e.Status)
		if p := r.State.Find(e.User); p != nil {
			r.log("replace", "%s was revealed to be %s!", r.display(p), p.Species)
		}
	case showdown.BattleSwap:
		if p := r.State.Find(e.User); p != nil {
			p.Position = e.Position
			p.Slot = string(rune('a' + e.Position))
		}
	case showdown.BattleCant:
		name := identName(e.User)
		if p := r.State.Find(e.User); p != nil {
			name = r.display(p)
		}
		if e.Move != "" {
			r.log("cant", "%s can't use %s! (%s)", name, e.Move, e.Reason)
		} else {
			r.log("cant", "%s can't move! (%s)", name, e.Reason)
		}
	case showdown.BattleFaint:
		if p := r.State.Find(e.User); p != nil {
			p.Fainted = true
			p.HP = 0
			p.HPPercent = 0
			p.Status = "fnt"
			r.log("faint", "%s fainted!", r.display(p))
		}

	// ---- damage / status ----
	case showdown.BattleDamage:
		if p := r.State.Find(e.Target); p != nil {
			beforeHP, beforePct := p.HP, p.HPPercent
			hadMax := p.MaxHP > 0
			p.setCondition(e.HP)
			if e.Status != "" {
				p.Status = e.Status
			}
			r.log("damage", "%s lost %s%s", r.display(p), damageDelta(beforeHP, beforePct, p, hadMax), fromSuffix(e.Tags))
		}
	case showdown.BattleHeal:
		if p := r.State.Find(e.Target); p != nil {
			beforeHP, beforePct := p.HP, p.HPPercent
			hadMax := p.MaxHP > 0
			p.setCondition(e.HP)
			if e.Status != "" {
				p.Status = e.Status
			}
			r.log("heal", "%s restored %s%s", r.display(p), healDelta(beforeHP, beforePct, p, hadMax), fromSuffix(e.Tags))
		}
	case showdown.BattleSetHP:
		if p := r.State.Find(e.Target); p != nil {
			p.setCondition(e.HP)
		}
	case showdown.BattleStatus:
		if p := r.State.Find(e.Target); p != nil {
			p.Status = e.Status
			r.log("status", "%s was %s!", r.display(p), statusName(e.Status))
		}
	case showdown.BattleCureStatus:
		if p := r.State.Find(e.Target); p != nil {
			p.Status = ""
			r.log("status", "%s recovered from %s.", r.display(p), statusName(e.Status))
		}
	case showdown.BattleCureTeam:
		side := r.State.Side(sideOf(e.User))
		if side != nil {
			for _, p := range side.Party {
				p.Status = ""
			}
		}
		r.log("status", "A bell chimed and cured the team's status conditions.")

	// ---- boosts ----
	case showdown.BattleBoost:
		if p := r.State.Find(e.Target); p != nil {
			if e.Set {
				p.Boosts[e.Stat] = clampBoost(e.Amount)
			} else {
				p.Boosts[e.Stat] = clampBoost(p.Boosts[e.Stat] + e.Amount)
			}
			r.log("boost", "%s's %s %s!", r.display(p), statName(e.Stat), riseFall(e.Amount))
		}
	case showdown.BattleSwapBoost:
		if a, b := r.State.Find(e.Source), r.State.Find(e.Target); a != nil && b != nil {
			for _, stat := range e.Stats {
				a.Boosts[stat], b.Boosts[stat] = b.Boosts[stat], a.Boosts[stat]
			}
			r.log("boost", "%s swapped stat changes with %s.", r.display(a), r.display(b))
		}
	case showdown.BattleInvertBoost:
		if p := r.State.Find(e.Target); p != nil {
			for k, v := range p.Boosts {
				p.Boosts[k] = -v
			}
			r.log("boost", "%s's stat changes were inverted!", r.display(p))
		}
	case showdown.BattleClearBoost:
		if p := r.State.Find(e.Target); p != nil {
			p.Boosts = map[string]int{}
			r.log("boost", "%s's stat changes were cleared!", r.display(p))
		}
	case showdown.BattleClearAllBoost:
		for _, side := range []*Side{r.State.P1, r.State.P2} {
			for _, p := range side.Party {
				p.Boosts = map[string]int{}
			}
		}
		r.log("boost", "All stat changes were eliminated!")
	case showdown.BattleCopyBoost:
		if a, b := r.State.Find(e.Source), r.State.Find(e.Target); a != nil && b != nil {
			cp := map[string]int{}
			for k, v := range a.Boosts {
				cp[k] = v
			}
			b.Boosts = cp
			r.log("boost", "%s copied %s's stat changes!", r.display(b), r.display(a))
		}

	// ---- field ----
	case showdown.BattleWeather:
		if e.Upkeep {
			break
		}
		if e.Weather == "" || e.Weather == "none" {
			r.State.Weather = ""
			r.log("weather", "The weather returned to normal.")
		} else {
			r.State.Weather = e.Weather
			r.log("weather", "%s", weatherStart(e.Weather))
		}
	case showdown.BattleFieldStart:
		if isTerrain(e.Condition) {
			r.State.Terrain = e.Condition
		}
		r.State.Field[e.Condition] = true
		r.log("field", "%s", conditionStart(e.Condition))
	case showdown.BattleFieldEnd:
		delete(r.State.Field, e.Condition)
		if r.State.Terrain == e.Condition {
			r.State.Terrain = ""
		}
		r.log("field", "%s", conditionEnd(e.Condition))
	case showdown.BattleFieldActivate:
		r.log("field", "%s activated.", cleanCondition(e.Condition))
	case showdown.BattleSideStart:
		side := r.State.Side(e.Side)
		if side != nil {
			side.Conditions[e.Condition]++
			r.log("side", "%s started on %s's side.", cleanCondition(e.Condition), sideLabel(r, side))
		}
	case showdown.BattleSideEnd:
		if side := r.State.Side(e.Side); side != nil {
			delete(side.Conditions, e.Condition)
			r.log("side", "%s ended on %s's side.", cleanCondition(e.Condition), sideLabel(r, side))
		}
	case showdown.BattleSwapSideConditions:
		r.State.P1.Conditions, r.State.P2.Conditions = r.State.P2.Conditions, r.State.P1.Conditions
		r.log("side", "Side conditions were swapped!")
	case showdown.BattleVolatileStart:
		if p := r.State.Find(e.Target); p != nil {
			p.Volatiles[e.Effect] = true
			r.log("volatile", "%s: %s", r.display(p), cleanCondition(e.Effect))
		}
	case showdown.BattleVolatileEnd:
		if p := r.State.Find(e.Target); p != nil {
			delete(p.Volatiles, e.Effect)
		}

	// ---- items / abilities / formes ----
	case showdown.BattleItem:
		if p := r.State.Find(e.Target); p != nil {
			p.Item = e.Item
			r.log("item", "%s revealed its %s!", r.display(p), e.Item)
		}
	case showdown.BattleEndItem:
		if p := r.State.Find(e.Target); p != nil {
			if e.Eat {
				r.log("item", "%s ate its %s!", r.display(p), e.Item)
			} else {
				r.log("item", "%s lost its %s!", r.display(p), e.Item)
			}
			p.Item = ""
		}
	case showdown.BattleAbility:
		if p := r.State.Find(e.Target); p != nil {
			p.Ability = e.Ability
			if e.From == "" {
				r.log("ability", "%s's %s!", r.display(p), e.Ability)
			} else {
				r.log("ability", "%s's ability became %s!", r.display(p), e.Ability)
			}
		}
	case showdown.BattleEndAbility:
		if p := r.State.Find(e.Target); p != nil {
			p.Ability = ""
			r.log("ability", "%s's ability was suppressed!", r.display(p))
		}
	case showdown.BattleTransform:
		if p := r.State.Find(e.Target); p != nil {
			r.log("transform", "%s transformed into %s!", r.display(p), e.Species)
		}
	case showdown.BattleMega:
		if p := r.State.Find(e.User); p != nil {
			r.log("mega", "%s Mega Evolved!", r.display(p))
		}
	case showdown.BattlePrimal:
		if p := r.State.Find(e.User); p != nil {
			r.log("mega", "%s underwent Primal Reversion!", r.display(p))
		}
	case showdown.BattleBurst:
		if p := r.State.Find(e.User); p != nil {
			r.log("mega", "%s Ultra Burst!", r.display(p))
		}
	case showdown.BattleZPower:
		if p := r.State.Find(e.User); p != nil {
			r.log("zmove", "%s unleashed its Z-Power!", r.display(p))
		}
	case showdown.BattleZBroken:
		if p := r.State.Find(e.Target); p != nil {
			r.log("zmove", "The Z-Move broke through %s's protection!", r.display(p))
		}
	case showdown.BattleHint:
		r.log("hint", "(%s)", e.Message)

	// ---- effects we model generically ----
	case showdown.BattleEffect:
		r.applyEffect(e)

	case showdown.BattleUnknown:
		if r.Debug {
			r.log("unknown", "unhandled protocol event %q (%d args)", e.Type, len(e.Args))
		}
	}
}

// applyEffect renders the long tail of minor actions.
func (r *Reducer) applyEffect(e showdown.BattleEffect) {
	arg := func(i int) string {
		if i < len(e.Args) {
			return e.Args[i]
		}
		return ""
	}
	name := func(i int) string {
		if p := r.State.Find(arg(i)); p != nil {
			return r.display(p)
		}
		return identName(arg(i))
	}
	switch e.Kind {
	case "crit":
		r.log("crit", "A critical hit!")
	case "supereffective":
		r.log("effect", "It's super effective!")
	case "resisted":
		r.log("effect", "It's not very effective...")
	case "immune":
		r.log("effect", "It doesn't affect %s...", name(0))
	case "fail":
		if a := arg(1); a != "" {
			r.log("effect", "But %s's %s failed!", name(0), cleanCondition(a))
		} else {
			r.log("effect", "But it failed!")
		}
	case "miss":
		r.log("effect", "%s's attack missed!", name(0))
	case "activate":
		r.log("effect", "%s activated.", cleanCondition(arg(0)))
	case "terastallize":
		if p := r.State.Find(arg(0)); p != nil {
			p.Terastallized = true
			p.TeraType = arg(1)
			r.log("tera", "%s Terastallized into the %s type!", r.display(p), arg(1))
		}
	case "hitcount":
		r.log("effect", "Hit %s time(s)!", arg(1))
	case "prepare":
		r.log("effect", "%s is preparing %s!", name(0), arg(1))
	case "mustrecharge":
		r.log("effect", "%s must recharge!", name(0))
	case "singlemove", "singleturn":
		r.log("effect", "%s: %s", name(0), cleanCondition(arg(1)))
	case "nothing":
		r.log("effect", "But nothing happened!")
	case "waiting":
		r.log("effect", "%s is waiting for %s...", name(0), name(1))
	case "center":
		r.log("effect", "The Pokémon were centered.")
	case "block":
		r.log("effect", "%s's %s blocked it!", name(0), cleanCondition(arg(1)))
	case "notarget":
		r.log("effect", "There was no target...")
	case "combine":
		r.log("effect", "The moves were combined!")
	case "end":
		// handled by BattleVolatileEnd
	default:
		if r.Debug {
			r.log("effect", "%s %v", e.Kind, e.Args)
		}
	}
}

// ---------------------------------------------------------------------------
// Request handling
// ---------------------------------------------------------------------------

func (r *Reducer) applyRequest(raw string) {
	req, err := ParseRequest(raw)
	if err != nil {
		r.log("error", "Could not parse choice request: %v", err)
		return
	}
	r.State.Request = req
	r.State.AwaitingChoice = req.NeedsChoice()
	if req.Side.ID != "" {
		r.State.Me = req.Side.ID
	}
	r.syncSide(req.Side)
	if req.Ally != nil {
		r.syncSide(*req.Ally)
	}
	if req.Kind() == RequestMove {
		for i, ar := range req.Active {
			p := r.State.MySide().ActiveAt(i)
			if p == nil {
				continue
			}
			p.Moves = make([]Move, 0, len(ar.Moves))
			for _, mr := range ar.Moves {
				p.Moves = append(p.Moves, Move{
					ID:             mr.ID,
					Name:           mr.Move,
					PP:             mr.PP,
					MaxPP:          mr.MaxPP,
					Target:         mr.Target,
					Disabled:       mr.Disabled.Set,
					DisabledReason: mr.Disabled.Reason,
				})
			}
		}
	}
}

// syncSide reconciles our own side from a request, which is authoritative.
// Party order is adopted from the request while existing Pokémon pointers are
// reused so transient state (boosts, volatiles) survives.
func (r *Reducer) syncSide(sr SideRequest) {
	side := r.State.Side(sr.ID)
	if side == nil {
		return
	}
	if sr.Name != "" {
		side.Name = sr.Name
	}
	ordered := make([]*Pokemon, 0, len(sr.Pokemon))
	used := map[*Pokemon]bool{}
	for _, sp := range sr.Pokemon {
		p := matchPokemon(side, sp, used)
		if p == nil {
			p = &Pokemon{
				SideID:    side.ID,
				Position:  -1,
				Boosts:    map[string]int{},
				Volatiles: map[string]bool{},
			}
		}
		applySwitchRequest(p, sp)
		used[p] = true
		ordered = append(ordered, p)
	}
	for _, p := range side.Party {
		if !used[p] {
			ordered = append(ordered, p)
		}
	}
	side.Party = ordered
}

func matchPokemon(side *Side, sp PokemonSwitchRequest, used map[*Pokemon]bool) *Pokemon {
	_, _, name := parseIdent(sp.Ident)
	species, _, _, _, _ := parseDetails(sp.Details)
	for _, p := range side.Party {
		if used[p] {
			continue
		}
		if p.Name == name || p.Species == species {
			return p
		}
	}
	return nil
}

func applySwitchRequest(p *Pokemon, sp PokemonSwitchRequest) {
	species, level, gender, shiny, tera := parseDetails(sp.Details)
	if sp.Ident != "" {
		p.Ident = sp.Ident
	}
	if _, _, name := parseIdent(sp.Ident); name != "" {
		p.Name = name
	}
	p.Species = species
	p.Level = level
	p.Gender = gender
	p.Shiny = shiny
	if tera != "" {
		p.TeraType = tera
		p.Terastallized = true
	}
	p.setCondition(sp.Condition)
	p.Item = sp.Item
	p.Ability = sp.Ability
	p.BaseAbility = sp.BaseAbility
	if len(sp.Moves) > 0 {
		p.MoveIDs = sp.Moves
	}
	if len(sp.Stats) > 0 {
		p.Stats = sp.Stats
	}
	p.Revealed = true
}

// ---------------------------------------------------------------------------
// Switching
// ---------------------------------------------------------------------------

func (r *Reducer) switchIn(ident, details, hp, status string, drag bool) {
	sideID, slot, _ := parseIdent(ident)
	side := r.State.Side(sideID)
	if side == nil {
		return
	}
	for _, p := range side.Party {
		if p.Active && p.Slot == slot {
			p.Active = false
			p.Slot = ""
			p.Position = -1
			p.Boosts = map[string]int{}
			p.Volatiles = map[string]bool{}
			p.Terastallized = false
			p.TeraType = ""
			p.Moves = nil
		}
	}

	species, _, _, _, _ := parseDetails(details)
	p := r.findForSwitch(side, ident, species)
	if p == nil {
		p = &Pokemon{
			SideID:    sideID,
			Boosts:    map[string]int{},
			Volatiles: map[string]bool{},
		}
		side.Party = append(side.Party, p)
	}
	p.SetIdent(ident)
	if species != "" {
		p.Species = species
	}
	applyDetailsString(p, details)
	p.setCondition(hp)
	if status != "" {
		p.Status = status
	}
	p.Revealed = true

	switch {
	case drag:
		r.log("switch", "%s was dragged out!", r.display(p))
	case r.State.Turn == 0 || r.State.TeamPreview:
		r.log("switch", "%s was sent out!", r.display(p))
	case r.isMine(p):
		r.log("switch", "Go! %s!", p.Name)
	default:
		name := "The opponent"
		if side := r.State.Side(p.SideID); side != nil && side.Name != "" {
			name = side.Name
		}
		r.log("switch", "%s sent out %s!", name, p.Name)
	}
}

// isMine reports whether a Pokémon belongs to the player this client controls.
func (r *Reducer) isMine(p *Pokemon) bool {
	return p != nil && r.State.Me != "" && p.SideID == r.State.Me
}

func (r *Reducer) findForSwitch(side *Side, ident, species string) *Pokemon {
	_, _, name := parseIdent(ident)
	for _, p := range side.Party {
		if p.Active {
			continue
		}
		if name != "" && p.Name == name {
			return p
		}
	}
	for _, p := range side.Party {
		if p.Active {
			continue
		}
		if species != "" && p.Species == species {
			return p
		}
	}
	return nil
}

func (r *Reducer) applyDetails(ident, details, hp, status string) {
	p := r.State.ensure(ident)
	if p == nil {
		return
	}
	applyDetailsString(p, details)
	p.setCondition(hp)
	if status != "" {
		p.Status = status
	}
}

func applyDetailsString(p *Pokemon, details string) {
	species, level, gender, shiny, tera := parseDetails(details)
	if species != "" {
		p.Species = species
	}
	if level > 0 {
		p.Level = level
	}
	p.Gender = gender
	p.Shiny = shiny
	if tera != "" {
		p.TeraType = tera
		p.Terastallized = true
	}
}

// ---------------------------------------------------------------------------
// Display helpers
// ---------------------------------------------------------------------------

// display returns a Pokémon's name, prefixed for the opponent's side.
func (r *Reducer) display(p *Pokemon) string {
	if p == nil {
		return "?"
	}
	if r.State.Me != "" && p.SideID != r.State.Me {
		return "The opposing " + p.Name
	}
	return p.Name
}

func (r *Reducer) log(kind, format string, args ...any) {
	text := format
	if len(args) > 0 {
		text = fmt.Sprintf(format, args...)
	}
	r.State.Log = append(r.State.Log, LogEntry{Turn: r.State.Turn, Kind: kind, Text: text})
	if len(r.State.Log) > maxLogEntries {
		r.State.Log = r.State.Log[len(r.State.Log)-maxLogEntries:]
	}
}

// firstInt returns the first integer in a string, used to pull a countdown out
// of the timer's prose messages.
func firstInt(s string) int {
	start := -1
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			n, err := strconv.Atoi(s[start:i])
			if err == nil {
				return n
			}
			start = -1
		}
	}
	if start >= 0 {
		if n, err := strconv.Atoi(s[start:]); err == nil {
			return n
		}
	}
	return 0
}

func identName(ident string) string {
	_, _, name := parseIdent(ident)
	if name == "" {
		return ident
	}
	return name
}

func sideOf(ident string) string {
	side, _, _ := parseIdent(ident)
	return side
}

func sideLabel(r *Reducer, s *Side) string {
	if r.State.Me != "" && s.ID != r.State.Me {
		return "the opposing side"
	}
	if s.Name != "" {
		return s.Name
	}
	return "your side"
}

func itemFromFlag(s string) string {
	if s == "" || s == "item" {
		return ""
	}
	return s
}

func clampBoost(n int) int {
	if n > 6 {
		return 6
	}
	if n < -6 {
		return -6
	}
	return n
}

var statNames = map[string]string{
	"atk": "Attack", "def": "Defense", "spa": "Sp. Atk", "spd": "Sp. Def",
	"spe": "Speed", "accuracy": "accuracy", "evasion": "evasiveness",
}

func statName(s string) string {
	if n, ok := statNames[s]; ok {
		return n
	}
	return s
}

func riseFall(n int) string {
	if n > 0 {
		return "rose" + degree(n)
	}
	return "fell" + degree(n)
}

// degree matches Pokémon's own wording for how large a stat change was.
func degree(n int) string {
	switch {
	case n >= 3 || n <= -3:
		return " drastically"
	case n == 2 || n == -2:
		return " sharply"
	}
	return ""
}

// damageDelta renders how much HP was lost, preferring a raw number for our own
// Pokémon (where we know the maximum) and a percentage for the opponent's.
func damageDelta(beforeHP, beforePct int, p *Pokemon, hadMax bool) string {
	if hadMax && beforeHP > p.HP {
		return fmt.Sprintf("%d HP (%d/%d)", beforeHP-p.HP, p.HP, p.MaxHP)
	}
	if beforePct > p.HPPercent {
		return fmt.Sprintf("%d%% (%d%%)", beforePct-p.HPPercent, p.HPPercent)
	}
	return fmt.Sprintf("HP (%d%%)", p.HPPercent)
}

func healDelta(beforeHP, beforePct int, p *Pokemon, hadMax bool) string {
	if hadMax && p.HP > beforeHP {
		return fmt.Sprintf("%d HP (%d/%d)", p.HP-beforeHP, p.HP, p.MaxHP)
	}
	if p.HPPercent > beforePct {
		return fmt.Sprintf("%d%% (%d%%)", p.HPPercent-beforePct, p.HPPercent)
	}
	return fmt.Sprintf("HP (%d%%)", p.HPPercent)
}

// fromSuffix renders the "[from] ..." tag, which explains what caused an
// effect. It is the difference between "lost 45%" and "lost 45% to Stealth
// Rock".
func fromSuffix(tags showdown.Tags) string {
	from := tags.Get("from")
	if from == "" {
		return ""
	}
	return " [" + cleanCondition(from) + "]"
}

var statusNames = map[string]string{
	"brn": "burned", "par": "paralyzed", "slp": "asleep", "frz": "frozen",
	"psn": "poisoned", "tox": "badly poisoned", "fnt": "fainted",
}

func statusName(s string) string {
	if n, ok := statusNames[s]; ok {
		return n
	}
	return s
}

func isTerrain(c string) bool {
	return strings.Contains(c, "Terrain")
}

// cleanCondition strips protocol prefixes such as "move: " and "ability: ".
func cleanCondition(c string) string {
	for _, prefix := range []string{"move: ", "ability: ", "item: ", "weather: ", "species: "} {
		c = strings.TrimPrefix(c, prefix)
	}
	return c
}

func weatherStart(w string) string {
	switch cleanCondition(w) {
	case "RainDance":
		return "It started to rain!"
	case "Sandstorm":
		return "A sandstorm kicked up!"
	case "SunnyDay":
		return "The sunlight got bright!"
	case "Hail":
		return "It started to hail!"
	case "Snow":
		return "It started to snow!"
	case "DesolateLand":
		return "The sunlight turned extremely harsh!"
	case "PrimordialSea":
		return "A heavy rain began to fall!"
	case "DeltaStream":
		return "Mysterious strong winds are protecting Flying-type Pokémon!"
	default:
		return cleanCondition(w) + " started."
	}
}

func conditionStart(c string) string {
	switch cleanCondition(c) {
	case "Trick Room":
		return "The dimensions were twisted!"
	case "Magic Room":
		return "It created a bizarre area in which held items lose their effects!"
	case "Wonder Room":
		return "It created a bizarre area in which Defense and Sp. Def stats are swapped!"
	case "Gravity":
		return "Gravity intensified!"
	case "Electric Terrain":
		return "An electric current ran across the battlefield!"
	case "Grassy Terrain":
		return "Grass grew to cover the battlefield!"
	case "Misty Terrain":
		return "Mist swirled around the battlefield!"
	case "Psychic Terrain":
		return "The battlefield got weird!"
	default:
		return cleanCondition(c) + " started."
	}
}

func conditionEnd(c string) string {
	switch cleanCondition(c) {
	case "Trick Room":
		return "The twisted dimensions returned to normal!"
	case "Gravity":
		return "Gravity returned to normal!"
	default:
		return cleanCondition(c) + " ended."
	}
}
