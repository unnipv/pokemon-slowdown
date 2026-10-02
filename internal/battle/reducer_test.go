package battle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unnipv/pokemon-slowdown/internal/showdown"
)

// replay feeds a recorded battle stream through the parser and reducer.
func replay(t *testing.T, name string) *Reducer {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "battles", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	r := NewReducer("battle-test")
	for _, frame := range showdown.SplitFrames(string(raw)) {
		for _, ev := range showdown.Parse(frame) {
			r.Apply(ev)
		}
	}
	return r
}

func TestReplaySingles(t *testing.T) {
	r := replay(t, "gen9-singles.txt")
	s := r.State

	if s.Me != "p1" {
		t.Errorf("Me = %q, want p1", s.Me)
	}
	if s.Tier != "[Gen 9] Random Battle" {
		t.Errorf("Tier = %q", s.Tier)
	}
	if s.Gen != 9 || s.GameType != "singles" {
		t.Errorf("Gen/GameType = %d/%q", s.Gen, s.GameType)
	}
	if s.Turn != 6 {
		t.Errorf("Turn = %d, want 6", s.Turn)
	}
	if !s.Ended || s.Winner != "coffee_enjoyer" {
		t.Errorf("Ended=%v Winner=%q", s.Ended, s.Winner)
	}
	if s.AwaitingChoice {
		t.Error("should not await a choice after the battle ends")
	}
	if s.Weather != "Sandstorm" {
		t.Errorf("Weather = %q, want Sandstorm", s.Weather)
	}
	if s.P1.Conditions["Stealth Rock"] != 1 {
		t.Errorf("Stealth Rock on p1 = %d, want 1", s.P1.Conditions["Stealth Rock"])
	}
	if s.P1.Name != "coffee_enjoyer" || s.P2.Name != "sleepy_panda" {
		t.Errorf("side names = %q / %q", s.P1.Name, s.P2.Name)
	}
	if len(s.P1.Party) != 6 || len(s.P2.Party) != 6 {
		t.Errorf("party sizes = %d / %d, want 6/6", len(s.P1.Party), len(s.P2.Party))
	}

	// Landorus took Stealth Rock, Toxic damage and Sucker Punch, and
	// Terastallized into Ground.
	lati := findByName(s.P1, "Landorus-Therian")
	if lati == nil {
		t.Fatal("Landorus-Therian missing from p1")
	}
	if !lati.Active {
		t.Error("Landorus should be active")
	}
	if lati.Status != "tox" {
		t.Errorf("Landorus status = %q, want tox", lati.Status)
	}
	if !lati.Terastallized || lati.TeraType != "Ground" {
		t.Errorf("Tera = %v/%q, want true/Ground", lati.Terastallized, lati.TeraType)
	}
	if lati.HP != 80 || lati.MaxHP != 300 {
		t.Errorf("Landorus HP = %d/%d, want 80/300", lati.HP, lati.MaxHP)
	}

	// Gengar accumulated +2 Sp. Atk, then switched out, which must clear it.
	gengar := findByName(s.P1, "Gengar")
	if gengar == nil {
		t.Fatal("Gengar missing from p1")
	}
	if gengar.Active {
		t.Error("Gengar should be benched after switching out")
	}
	if gengar.Boosts["spa"] != 0 {
		t.Errorf("Gengar spa boost = %d, want 0 after switching out", gengar.Boosts["spa"])
	}

	// Our own moves came from the request, not from guessing.
	if len(lati.Moves) != 4 || lati.Moves[0].Name != "Earthquake" {
		t.Errorf("Landorus moves = %#v", lati.Moves)
	}
	if lati.Moves[0].PP != 23 {
		t.Errorf("Earthquake PP = %d, want 23", lati.Moves[0].PP)
	}

	for _, name := range []string{"Dragapult", "Garchomp", "Kingambit"} {
		p := findByName(s.P2, name)
		if p == nil {
			t.Fatalf("opponent %s missing", name)
		}
		if !p.Fainted {
			t.Errorf("opponent %s should be fainted", name)
		}
	}

	if len(s.Log) == 0 {
		t.Error("expected a battle log")
	}
	if !hasLogKind(s, "tera") {
		t.Error("expected a tera log entry")
	}
	if !hasLogKind(s, "faint") {
		t.Error("expected a faint log entry")
	}
}

func TestRequestKind(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want RequestKind
	}{
		{"wait", `{"wait":true,"side":{"name":"x","id":"p1","pokemon":[]}}`, RequestWait},
		{"team", `{"teamPreview":true,"side":{"name":"x","id":"p1","pokemon":[]}}`, RequestTeam},
		{"switch", `{"forceSwitch":[true],"side":{"name":"x","id":"p1","pokemon":[]}}`, RequestSwitch},
		{"move", `{"active":[{"moves":[]}],"side":{"name":"x","id":"p1","pokemon":[]}}`, RequestMove},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := ParseRequest(tc.raw)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if got := req.Kind(); got != tc.want {
				t.Errorf("Kind() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDisabledFieldAcceptsBoolOrString(t *testing.T) {
	req, err := ParseRequest(`{"active":[{"moves":[
		{"move":"A","id":"a","disabled":true},
		{"move":"B","id":"b","disabled":"Choice item"},
		{"move":"C","id":"c","disabled":false}
	]}],"side":{"name":"x","id":"p1","pokemon":[]}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	moves := req.Active[0].Moves
	if !moves[0].Disabled.Set || moves[0].Disabled.Reason != "" {
		t.Errorf("bool disabled wrong: %#v", moves[0].Disabled)
	}
	if !moves[1].Disabled.Set || moves[1].Disabled.Reason != "Choice item" {
		t.Errorf("string disabled wrong: %#v", moves[1].Disabled)
	}
	if moves[2].Disabled.Set {
		t.Errorf("false disabled should be unset: %#v", moves[2].Disabled)
	}
}

func TestChoiceRendering(t *testing.T) {
	singles := Choice{Slots: []ChoiceSlot{{Kind: "move", Move: 1}}}
	if got := singles.String(); got != "move 1" {
		t.Errorf("singles choice = %q", got)
	}
	doubles := Choice{Slots: []ChoiceSlot{
		{Kind: "move", Move: 1, Target: "+1", Mechanic: "terastalize"},
		{Kind: "move", Move: 3},
	}}
	if got := doubles.String(); got != "move 1 +1 terastalize, move 3" {
		t.Errorf("doubles choice = %q", got)
	}
	if got := (Choice{Slots: []ChoiceSlot{{Kind: "switch", Switch: 4}}}).String(); got != "switch 4" {
		t.Errorf("switch choice = %q", got)
	}
	if got := UndoChoice().String(); got != "undo" {
		t.Errorf("undo = %q", got)
	}
	if got := DefaultChoice().String(); got != "default" {
		t.Errorf("default = %q", got)
	}
	if got := TeamChoice([]int{2, 1, 3, 4, 5, 6}).String(); got != "team 213456" {
		t.Errorf("team = %q", got)
	}
}

func TestDoublesTargeting(t *testing.T) {
	req, err := ParseRequest(`{"active":[
		{"moves":[{"move":"Thunderbolt","id":"thunderbolt","target":"normal"},{"move":"Helping Hand","id":"helpinghand","target":"adjacentAlly"},{"move":"Protect","id":"protect","target":"self"}]},
		{"moves":[{"move":"Thunderbolt","id":"thunderbolt","target":"normal"}]}
	],"side":{"name":"x","id":"p1","pokemon":[]}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got := req.SlotCount(); got != 2 {
		t.Fatalf("SlotCount = %d, want 2", got)
	}
	ar := req.ActiveAt(0)

	normal := ar.Moves[0]
	if !NeedsTarget(normal, 2) {
		t.Error("normal move in doubles needs a target")
	}
	targets := LegalTargets(normal, 0, 2, 2)
	if len(targets) != 2 || targets[0].Spec != "+1" || targets[1].Spec != "+2" {
		t.Errorf("normal targets = %#v", targets)
	}

	ally := ar.Moves[1]
	at := LegalTargets(ally, 0, 2, 2)
	if len(at) != 1 || at[0].Spec != "-2" {
		t.Errorf("adjacentAlly from slot 0 should offer only the other ally: %#v", at)
	}

	protect := ar.Moves[2]
	if NeedsTarget(protect, 2) {
		t.Error("self-targeting move should not need a target")
	}
	if got := LegalTargets(protect, 0, 2, 2); got != nil {
		t.Errorf("self move targets = %#v, want none", got)
	}

	// Singles never requires a target.
	if NeedsTarget(normal, 1) {
		t.Error("singles move should not need a target")
	}
}

func TestMechanics(t *testing.T) {
	req, err := ParseRequest(`{"active":[{
		"moves":[{"move":"Shadow Ball","id":"shadowball"}],
		"canMegaEvo":true,
		"canZMove":[{"move":"Never-Ending Nightmare","target":"normal"}],
		"canDynamax":true,
		"maxMoves":{"maxMoves":[{"move":"Max Phantasm","target":"normal"}]},
		"canTerastallize":"Ghost"
	}],"side":{"name":"x","id":"p1","pokemon":[]}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	m := AvailableMechanics(req.ActiveAt(0))
	for _, kind := range []string{"mega", "zmove", "max", "terastalize"} {
		if !m.Has(kind) {
			t.Errorf("expected mechanic %q to be available", kind)
		}
	}
	primary, ok := m.Primary()
	if !ok || primary.Kind != "terastalize" {
		t.Errorf("primary mechanic = %#v, want terastalize", primary)
	}
	if primary.Detail != "Ghost" {
		t.Errorf("tera detail = %q, want Ghost", primary.Detail)
	}
	if got := ZMoveFor(req.ActiveAt(0), 1); got != "Never-Ending Nightmare" {
		t.Errorf("ZMoveFor = %q", got)
	}
	if got := MaxMoveFor(req.ActiveAt(0), 1); got != "Max Phantasm" {
		t.Errorf("MaxMoveFor = %q", got)
	}

	// A request with no mechanics must not advertise any.
	plain, _ := ParseRequest(`{"active":[{"moves":[]}],"side":{"name":"x","id":"p1","pokemon":[]}}`)
	if got := AvailableMechanics(plain.ActiveAt(0)); len(got.Available) != 0 {
		t.Errorf("expected no mechanics, got %#v", got.Available)
	}
}

func TestSwitchSlots(t *testing.T) {
	req, err := ParseRequest(`{"forceSwitch":[true],"side":{"name":"x","id":"p1","pokemon":[
		{"ident":"p1: A","details":"A","condition":"0 fnt","active":true},
		{"ident":"p1: B","details":"B","condition":"100/100","active":false},
		{"ident":"p1: C","details":"C","condition":"50/100","active":false}
	]}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	slots := req.SwitchSlots()
	if len(slots) != 3 {
		t.Fatalf("SwitchSlots = %d, want 3", len(slots))
	}
	if !slots[0].Fainted || slots[0].Legal {
		t.Errorf("fainted slot should be illegal: %#v", slots[0])
	}
	if !slots[1].Legal || !slots[2].Legal {
		t.Errorf("healthy bench slots should be legal: %#v", slots)
	}
	if got := req.BenchSlots(); len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Errorf("BenchSlots = %#v, want [2 3]", got)
	}
	if !req.MustSwitchAt(0) {
		t.Error("MustSwitchAt(0) should be true")
	}
}

func TestSubstituteLogsAreClear(t *testing.T) {
	r := NewReducer("battle-sub")
	feed := func(lines ...string) {
		t.Helper()
		for _, frame := range showdown.SplitFrames(strings.Join(lines, "\n")) {
			for _, ev := range showdown.Parse(frame) {
				r.Apply(ev)
			}
		}
	}

	feed(
		"|switch|p1a: Mewtwo|Mewtwo, L100, M|100/100",
		"|switch|p2a: Skarmory|Skarmory, L100, M|100/100",
		"|turn|1",
		"|move|p1a: Mewtwo|Substitute|p1a: Mewtwo",
		"|-start|p1a: Mewtwo|Substitute",
		"|-damage|p1a: Mewtwo|75/100",
	)
	p := r.State.Find("p1a: Mewtwo")
	if p == nil {
		t.Fatal("Mewtwo should be tracked")
	}
	if !p.HasSubstitute() {
		t.Fatal("Substitute should be active after -start")
	}
	if !hasLogText(r.State, "made a substitute") {
		t.Errorf("start log is unclear: %#v", r.State.Log)
	}

	// An unbroken substitute absorbs the hit without touching the Pokémon.
	feed(
		"|turn|2",
		"|move|p2a: Skarmory|Brave Bird|p1a: Mewtwo",
		"|-activate|p1a: Mewtwo|move: Substitute|[damage]",
	)
	if !hasLogText(r.State, "substitute took the hit") {
		t.Errorf("absorb log is unclear: %#v", r.State.Log)
	}
	if !p.HasSubstitute() {
		t.Error("an unbroken substitute must stay active")
	}

	// Breaking it clears the volatile and says so.
	feed(
		"|turn|3",
		"|move|p2a: Skarmory|Brave Bird|p1a: Mewtwo",
		"|-end|p1a: Mewtwo|Substitute",
	)
	if p.HasSubstitute() {
		t.Error("Substitute should be cleared by -end")
	}
	if !hasLogText(r.State, "substitute faded") {
		t.Errorf("fade log is missing: %#v", r.State.Log)
	}
}

func TestUnknownEventsDoNotBreakState(t *testing.T) {
	r := NewReducer("battle-x")
	r.Debug = true
	lines := []string{
		"|turn|3",
		"|totallynewmessage|p1a: X|value|another",
		"|-futureminor|p1a: X|1",
	}
	for _, frame := range showdown.SplitFrames(strings.Join(lines, "\n")) {
		for _, ev := range showdown.Parse(frame) {
			r.Apply(ev)
		}
	}
	if r.State.Turn != 3 {
		t.Errorf("Turn = %d, want 3", r.State.Turn)
	}
	if !hasLogKind(r.State, "unknown") {
		t.Error("expected unknown event to be logged in debug mode")
	}
}

func findByName(s *Side, name string) *Pokemon {
	for _, p := range s.Party {
		if p.Name == name {
			return p
		}
	}
	return nil
}

func hasLogKind(s *State, kind string) bool {
	for _, e := range s.Log {
		if e.Kind == kind {
			return true
		}
	}
	return false
}

func hasLogText(s *State, want string) bool {
	for _, e := range s.Log {
		if strings.Contains(e.Text, want) {
			return true
		}
	}
	return false
}
