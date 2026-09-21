package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/unnipv/pokemon-slowdown/internal/battle"
	"github.com/unnipv/pokemon-slowdown/internal/config"
	"github.com/unnipv/pokemon-slowdown/internal/dex"
	"github.com/unnipv/pokemon-slowdown/internal/showdown"
	"github.com/unnipv/pokemon-slowdown/internal/storage"
)

func testModel(t *testing.T, cfg config.Config) *Model {
	t.Helper()
	m := New(cfg, Deps{Now: func() time.Time { return time.Unix(0, 0) }})
	m.width, m.height = 100, 40
	m.layout = LayoutFor(m.width)
	return m
}

// feedFixture replays a recorded battle stream into the model.
func feedFixture(t *testing.T, m *Model, name string) {
	t.Helper()
	path := filepath.Join("..", "..", "testdata", "battles", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	for _, frame := range showdown.SplitFrames(string(raw)) {
		for _, ev := range showdown.Parse(frame) {
			m.handleShowdownEvent(ev)
		}
	}
}

func TestLayoutFor(t *testing.T) {
	cases := []struct {
		cols int
		want LayoutMode
	}{
		{200, LayoutCinematic},
		{100, LayoutCinematic},
		{99, LayoutStandard},
		{70, LayoutStandard},
		{69, LayoutSidecar},
		{46, LayoutSidecar},
		{45, LayoutCompact},
		{20, LayoutCompact},
	}
	for _, tc := range cases {
		if got := LayoutFor(tc.cols); got != tc.want {
			t.Errorf("LayoutFor(%d) = %s, want %s", tc.cols, got, tc.want)
		}
	}
}

func TestLayoutSpriteBoxesShrinkWithWidth(t *testing.T) {
	cinW, _ := LayoutCinematic.SpriteCells()
	stdW, _ := LayoutStandard.SpriteCells()
	sideW, _ := LayoutSidecar.SpriteCells()
	if !(cinW > stdW && stdW > sideW) {
		t.Errorf("sprite boxes should shrink as width drops: %d %d %d", cinW, stdW, sideW)
	}
	if LayoutCompact.Sprites() {
		t.Error("compact layout must not show sprites")
	}
	if c, _ := LayoutCompact.SpriteCells(); c != 0 {
		t.Errorf("compact sprite box should be zero, got %d", c)
	}
}

func TestTooSmall(t *testing.T) {
	if !TooSmall(20, 40) || !TooSmall(80, 5) {
		t.Error("narrow or short terminals should be rejected")
	}
	if TooSmall(80, 24) {
		t.Error("a normal terminal should be accepted")
	}
}

func TestSanitizeStripsHostileEscapes(t *testing.T) {
	cases := map[string]string{
		"hello":                         "hello",
		"\x1b[31mred\x1b[0m":            "red",
		"\x1b]0;hacked\x07safe":         "safe",
		"\x1b]52;c;cGF5bG9hZA==\x1b\\x": "x",
		"a\x07b":                        "ab",
		"tab\there":                     "tab here",
	}
	for in, want := range cases {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeLineCollapsesNewlines(t *testing.T) {
	if got := SanitizeLine("a\nb\r\nc"); got != "a b c" {
		t.Errorf("SanitizeLine = %q", got)
	}
}

func TestFormatNames(t *testing.T) {
	if got := FormatName("gen9randombattle"); got != "Gen 9 Random Battle" {
		t.Errorf("FormatName(gen9randombattle) = %q", got)
	}
	if got := FormatName("gen9ou"); got != "Gen 9 OU" {
		t.Errorf("FormatName(gen9ou) = %q", got)
	}
	// Unknown ids still get something readable.
	got := FormatName("gen12doublesou")
	if !strings.HasPrefix(got, "Gen 12") {
		t.Errorf("prettified name = %q, want a Gen 12 prefix", got)
	}
}

func TestRenderRecordedBattle(t *testing.T) {
	m := testModel(t, config.Default())
	feedFixture(t, m, "gen9-singles.txt")

	if m.active == "" {
		t.Fatal("no battle became active")
	}
	if m.screen != screenBattle {
		t.Fatalf("screen = %v, want battle", m.screen)
	}
	out := m.render()
	for _, want := range []string{"Turn 6", "Landorus-Therian", "Kingambit"} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q", want)
		}
	}
}

func TestRenderAtEveryLayoutWidth(t *testing.T) {
	for _, cols := range []int{40, 60, 80, 120, 200} {
		for _, rows := range []int{14, 30, 50} {
			m := testModel(t, config.Default())
			m.width, m.height = cols, rows
			m.layout = LayoutFor(cols)
			feedFixture(t, m, "gen9-singles.txt")

			// Every screen and overlay must render without panicking.
			_ = m.render()
			m.helpOpen = true
			_ = m.render()
			m.helpOpen = false
			m.openPalette()
			_ = m.render()
			m.palette.open = false
			m.prompt = newPrompt("test", "Test prompt",
				promptField{label: "One", value: "abc"},
				promptField{label: "Secret", secret: true, value: "hunter2"},
			)
			_ = m.render()
			m.prompt = promptState{}
			m.screen = screenTeams
			_ = m.render()
			m.screen = screenLobby
			_ = m.render()
		}
	}
}

func TestBattleOverlaysRender(t *testing.T) {
	m := testModel(t, config.Default())
	feedFixture(t, m, "gen9-singles.txt")
	bv := m.activeBattle()
	if bv == nil {
		t.Fatal("no active battle")
	}
	for _, ov := range []overlayKind{overlaySwitch, overlayInspect, overlayLog, overlayChat, overlayTarget} {
		bv.overlay = ov
		out := bv.render(m.width, m.height-2, m.layout)
		if out == "" {
			t.Errorf("overlay %v rendered nothing", ov)
		}
	}
	bv.overlay = overlayNone
}

func TestChatIsSanitizedBeforeRendering(t *testing.T) {
	m := testModel(t, config.Default())
	m.username = "me"
	bv := m.battleFor("battle-test")
	bv.addChat("Evil", "\x1b[31mred\x1b[0m \x1b]0;pwned\x07text", false, 0)
	bv.overlay = overlayChat

	out := bv.render(m.width, m.height-2, m.layout)
	if strings.Contains(out, "\x1b]0;pwned") {
		t.Fatal("an OSC sequence from chat survived into the render")
	}
	if strings.Contains(out, "pwned") {
		t.Error("sanitized chat should not contain the injected payload")
	}
	if !strings.Contains(out, "red") || !strings.Contains(out, "text") {
		t.Error("legitimate chat text was lost")
	}
}

func TestHostileRoomTitleIsSanitized(t *testing.T) {
	m := testModel(t, config.Default())
	bv := m.battleFor("battle-test")
	bv.apply(showdown.RoomTitle{
		Base:  showdown.Base{RoomID: "battle-test"},
		Title: "nice\x1b[2Jtitle",
	})
	out := bv.render(m.width, m.height-2, m.layout)
	if strings.Contains(out, "\x1b[2J") {
		t.Fatal("a clear-screen sequence survived into the render")
	}
	if !strings.Contains(out, "nicetitle") {
		t.Errorf("expected sanitized title, got %q", out)
	}
}

func TestTooSmallRendersGracefully(t *testing.T) {
	m := testModel(t, config.Default())
	m.width, m.height = 10, 3
	out := m.render()
	if !strings.Contains(out, "too small") {
		t.Errorf("expected a too-small message, got %q", out)
	}
}

func TestPaletteFiltersAndRuns(t *testing.T) {
	m := testModel(t, config.Default())
	m.openPalette()
	m.palette.filter = "theme"
	cmds := m.filteredCommands()
	if len(cmds) == 0 {
		t.Fatal("no palette command matched 'theme'")
	}
	if !strings.Contains(cmds[0].title, "theme") && !strings.Contains(cmds[0].title, "Theme") {
		t.Errorf("unexpected match: %q", cmds[0].title)
	}

	before := m.cfg.Theme
	_ = m.handlePaletteKey("enter")
	if m.cfg.Theme == before {
		t.Error("running the theme command should change the theme")
	}
	if m.palette.open {
		t.Error("palette should close after running a command")
	}
}

func TestQuitDoesNotForfeitImmediately(t *testing.T) {
	m := testModel(t, config.Default())
	feedFixture(t, m, "gen9-singles.txt")
	_, cmd := m.handleKey(keyMsg("q"))
	if cmd != nil {
		t.Fatal("pressing q during a battle must not quit immediately")
	}
	if m.confirm == "" {
		t.Fatal("pressing q during a battle must ask for confirmation")
	}
	// Cancelling clears the prompt and does not quit.
	_, cmd = m.handleKey(keyMsg("n"))
	if cmd != nil {
		t.Fatal("cancelling the prompt must not quit")
	}
	if m.confirm != "" {
		t.Error("n should cancel the quit prompt")
	}
}

// feedFixtureUntilTurn replays a battle stream and stops at a given turn.
func feedFixtureUntilTurn(t *testing.T, m *Model, name string, turn int) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "battles", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	for _, frame := range showdown.SplitFrames(string(raw)) {
		for _, ev := range showdown.Parse(frame) {
			m.handleShowdownEvent(ev)
			if bv := m.activeBattle(); bv != nil && bv.state().Turn >= turn {
				return
			}
		}
	}
}

func TestMoveLinesAlignToTheSameColumn(t *testing.T) {
	m := testModel(t, config.Default())
	feedFixture(t, m, "gen9-singles.txt")
	bv := m.activeBattle()
	if bv == nil {
		t.Fatal("no battle")
	}
	// A move request we can render.
	bv.apply(showdown.BattleRequest{
		Base: showdown.Base{RoomID: bv.room},
		Request: `{"active":[{"moves":[
			{"move":"Shadow Ball","id":"shadowball","pp":23,"maxpp":24,"target":"normal"},
			{"move":"Sludge Wave","id":"sludgewave","pp":16,"maxpp":16,"target":"allAdjacent"},
			{"move":"Focus Blast","id":"focusblast","pp":8,"maxpp":8,"target":"normal"},
			{"move":"Nasty Plot","id":"nastyplot","pp":31,"maxpp":32,"target":"self"}
		]}],"side":{"name":"x","id":"p1","pokemon":[{"ident":"p1: Gengar","details":"Gengar, L82, M","condition":"241/241","active":true}]}}`,
	})

	for _, width := range []int{40, 60, 80, 120} {
		layout := LayoutFor(width)
		bv.curLayout = layout
		lines := bv.renderMoves(width, layout)

		var moveLines []string
		for _, line := range lines {
			plain := stripANSI(line)
			if strings.TrimSpace(plain) == "" {
				continue
			}
			if len(moveLines) < 4 && (strings.HasPrefix(plain, "  1") || strings.HasPrefix(plain, "  2") ||
				strings.HasPrefix(plain, "  3") || strings.HasPrefix(plain, "  4")) {
				moveLines = append(moveLines, strings.TrimRight(plain, " "))
			}
		}
		if len(moveLines) != 4 {
			t.Fatalf("width %d: found %d move lines, want 4", width, len(moveLines))
		}
		// The PP column is right-aligned, so every move row must end on the
		// same column once trailing padding is removed.
		want := len([]rune(moveLines[0]))
		for i, line := range moveLines {
			if got := len([]rune(line)); got != want {
				t.Errorf("width %d: move row %d ends at column %d, want %d\n  %q\n  %q",
					width, i+1, got, want, moveLines[0], line)
			}
		}
	}
}

func TestTypeBadgesAreFixedWidth(t *testing.T) {
	m := testModel(t, config.Default())
	bv := m.battleFor("battle-x")

	allTypes := []string{
		"normal", "fire", "water", "electric", "grass", "ice", "fighting",
		"poison", "ground", "flying", "psychic", "bug", "rock", "ghost",
		"dragon", "dark", "steel", "fairy",
	}
	first := lipgloss.Width(bv.typeBadge("ghost"))
	for _, typ := range allTypes {
		if w := lipgloss.Width(bv.typeBadge(typ)); w != first {
			t.Errorf("typeBadge(%q) is %d cols, want %d (badges must align)", typ, w, first)
		}
	}
	// A move with no known type must still reserve the same column, otherwise
	// the PP column shifts.
	if w := lipgloss.Width(bv.typeBadge("")); w != first {
		t.Errorf("empty typeBadge is %d cols, want %d", w, first)
	}
}

func TestMoveLinesAlignWithRealTypes(t *testing.T) {
	// Uses the cached dex so move type badges actually render. This is the
	// case that catches whitespace normalisation drifting the PP column.
	if _, err := os.Stat(filepath.Join(storage.DexDir(), "moves.json")); err != nil {
		t.Skip("dex cache not present; run `slowdown doctor` once")
	}
	d := dex.New(storage.DexDir())
	if err := d.Ensure(context.Background()); err != nil {
		t.Skipf("dex unavailable: %v", err)
	}
	m := New(config.Default(), Deps{Dex: d, Now: time.Now})
	m.width, m.height = 80, 40
	bv := m.battleFor("battle-x")
	bv.apply(showdown.BattleRequest{
		Base: showdown.Base{RoomID: "battle-x"},
		Request: `{"active":[{"moves":[
			{"move":"Shadow Ball","id":"shadowball","pp":23,"maxpp":24,"target":"normal"},
			{"move":"Focus Blast","id":"focusblast","pp":8,"maxpp":8,"target":"normal"},
			{"move":"Nasty Plot","id":"nastyplot","pp":31,"maxpp":32,"target":"self"},
			{"move":"Sludge Wave","id":"sludgewave","pp":16,"maxpp":16,"target":"allAdjacent"}
		]}],"side":{"name":"x","id":"p1","pokemon":[{"ident":"p1: Gengar","details":"Gengar, L82, M","condition":"241/241","active":true}]}}`,
	})

	for _, width := range []int{60, 80, 120} {
		layout := LayoutFor(width)
		bv.curLayout = layout
		var rows []string
		for _, line := range bv.renderMoves(width, layout) {
			plain := strings.TrimRight(stripANSI(line), " ")
			if len(rows) < 4 && len(plain) > 2 && plain[2] >= '1' && plain[2] <= '4' {
				rows = append(rows, plain)
			}
		}
		if len(rows) != 4 {
			t.Fatalf("width %d: got %d move rows", width, len(rows))
		}
		want := len([]rune(rows[0]))
		for i, r := range rows {
			if got := len([]rune(r)); got != want {
				t.Errorf("width %d: row %d ends at %d, want %d\n  %q\n  %q",
					width, i+1, got, want, rows[0], r)
			}
		}
		// Sanity: the badges really are present, otherwise this proves nothing.
		if !strings.Contains(rows[0], "GHOST") {
			t.Errorf("width %d: expected a type badge in %q", width, rows[0])
		}
	}
}

func TestNarrowMonLineKeepsTypes(t *testing.T) {
	m := testModel(t, config.Default())
	// No dex in this test, so types cannot render; assert the builder does not
	// rely on the dex to keep the line within bounds.
	bv := m.battleFor("battle-x")
	p := &battle.Pokemon{Name: "Landorus-Therian", HP: 263, MaxHP: 300, HPPercent: 87, Species: "Landorus-Therian"}
	for _, width := range []int{36, 46} {
		layout := LayoutFor(width)
		line := bv.renderMonLine(p, false, width, layout)
		if got := lipgloss.Width(line); got > width-1 {
			t.Errorf("width %d: mon line is %d cols: %q", width, got, stripANSI(line))
		}
	}
}

func TestPartyTrackerShowsEveryPokemon(t *testing.T) {
	m := testModel(t, config.Default())
	feedFixture(t, m, "gen9-singles.txt")
	bv := m.activeBattle()
	if bv == nil {
		t.Fatal("no battle")
	}
	line := stripANSI(bv.renderPartyTracker(120, LayoutStandard))

	// Two parties of six: six dots each, filled or hollow.
	if got := strings.Count(line, "●") + strings.Count(line, "○"); got != 12 {
		t.Errorf("expected 12 dots, got %d: %q", got, line)
	}
	// The opponent should show both hidden and seen Pokémon.
	if !strings.Contains(line, "○") {
		t.Errorf("expected hidden Pokémon to render hollow: %q", line)
	}
	if strings.Count(line, "/6") != 2 {
		t.Errorf("expected a remaining count per side: %q", line)
	}
	// Colour must not be the only signal, so the counts are textual.
	if !strings.Contains(line, "foe") || !strings.Contains(line, "you") {
		t.Errorf("expected both sides labelled: %q", line)
	}
}

func TestPartyTrackerFitsNarrowLayouts(t *testing.T) {
	m := testModel(t, config.Default())
	feedFixture(t, m, "gen9-singles.txt")
	bv := m.activeBattle()
	for _, width := range []int{36, 46, 60, 120} {
		line := stripANSI(bv.renderPartyTracker(width, LayoutFor(width)))
		if lipgloss.Width(line) > width {
			t.Errorf("width %d: tracker is %d cols: %q", width, lipgloss.Width(line), line)
		}
	}
}

func TestLobbyRowsKeepTagsInTheirOwnColumn(t *testing.T) {
	m := testModel(t, config.Default())
	m.formats = []showdown.Format{
		{ID: "gen9ffarandombattle", Name: "[Gen 9] Free-For-All Random Battle", Random: true, Searchable: true},
		{ID: "gen9championsdoublescustomgame", Name: "[Gen 9] Champions] Doubles Custom Game", Challengeable: true},
		{ID: "gen9ou", Name: "[Gen 9] OU", Searchable: true, Challengeable: true},
	}
	for _, width := range []int{50, 60, 80, 120} {
		out := stripANSI(m.renderLobbyList(width, 30))
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "Battlerandom") || strings.Contains(line, "Gamerandom") ||
				strings.Contains(line, "Gamechallenge") {
				t.Errorf("width %d: tag ran into the format name: %q", width, line)
			}
		}
	}
}

func TestBattleIDFromInput(t *testing.T) {
	cases := map[string]string{
		"battle-gen9randombattle-123":                             "battle-gen9randombattle-123",
		"https://replay.pokemonshowdown.com/gen9randombattle-123": "battle-gen9randombattle-123",
		"replay.pokemonshowdown.com/battle-gen9ou-9":              "battle-gen9ou-9",
		"nonsense": "",
	}
	for in, want := range cases {
		if got := battleIDFromInput(in); got != want {
			t.Errorf("battleIDFromInput(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNextThemeCycles(t *testing.T) {
	seen := map[string]bool{}
	cur := ThemeNames[0]
	for i := 0; i < len(ThemeNames); i++ {
		cur = nextTheme(cur)
		seen[cur] = true
	}
	if len(seen) != len(ThemeNames) {
		t.Errorf("theme cycle visited %d themes, want %d", len(seen), len(ThemeNames))
	}
}

// keyMsg builds a key press message for a single character or named key.
func keyMsg(s string) tea.KeyPressMsg {
	var k tea.KeyPressMsg
	switch s {
	case "enter":
		k.Code = tea.KeyEnter
	case "esc":
		k.Code = tea.KeyEscape
	case "up":
		k.Code = tea.KeyUp
	case "down":
		k.Code = tea.KeyDown
	case "backspace":
		k.Code = tea.KeyBackspace
	default:
		r := []rune(s)
		if len(r) == 1 {
			k.Code = r[0]
			k.Text = s
		}
	}
	return k
}

func TestHTMLCommandOutputIsRenderedAsText(t *testing.T) {
	// /data and friends reply with HTML; it must not reach the screen as markup.
	const raw = `/raw <ul class="utilichart"><li class="result"><span class="col itemiconcol">` +
		`<psicon item="choiceband"></psicon></span> <span class="col namecol">` +
		`<a href="https://dex.pokemonshowdown.com/items/choiceband">Choice Band</a></span> ` +
		`<span class="col itemdesccol">Holder's Attack is 1.5&times;, but it can only select the first move it executes.</span></li></ul>`

	if !looksLikeHTML(raw) {
		t.Fatal("HTML command output should be detected")
	}
	text := htmlToText(raw)
	if strings.Contains(text, "<") || strings.Contains(text, ">") {
		t.Errorf("tags survived: %q", text)
	}
	for _, want := range []string{"Choice Band", "Holder's Attack is 1.5×", "first move"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
	if strings.Contains(text, "psicon") || strings.Contains(text, "utilichart") {
		t.Errorf("markup leaked into the text: %q", text)
	}

	// And it must survive into the rendered chat overlay.
	m := testModel(t, config.Default())
	bv := m.battleFor("battle-x")
	bv.addChat("cosmopense", raw, true, 0)
	bv.overlay = overlayChat
	out := stripANSI(bv.render(m.width, m.height-2, m.layout))
	if strings.Contains(out, "<span") || strings.Contains(out, "utilichart") {
		t.Errorf("markup reached the screen:\n%s", out)
	}
	if !strings.Contains(out, "Choice Band") {
		t.Error("the readable text did not reach the screen")
	}
}

func TestPlainChatIsLeftAlone(t *testing.T) {
	if looksLikeHTML("gl hf") {
		t.Error("plain chat must not be treated as HTML")
	}
	if looksLikeHTML("a < b") {
		t.Error("a stray angle bracket is not HTML")
	}
	if got := htmlToText("gl hf"); got != "gl hf" {
		t.Errorf("plain text was rewritten: %q", got)
	}
}

func TestInspectOverlayFitsOnScreen(t *testing.T) {
	// A battle in progress, which is when an overlay is actually opened.
	m := testModel(t, config.Default())
	feedFixtureUntilTurn(t, m, "gen9-singles.txt", 4)
	bv := m.activeBattle()
	if bv == nil {
		t.Fatal("no battle")
	}
	bv.overlay = overlayInspect

	for _, rows := range []int{18, 24, 30, 44} {
		out := bv.render(120, rows-2, LayoutForSize(120, rows))
		// The framed box must be closed; a missing bottom border means it ran
		// off the screen.
		if !strings.Contains(out, "╰") {
			t.Errorf("rows=%d: overlay bottom border missing", rows)
		}
		if got := len(strings.Split(out, "\n")); got > rows {
			t.Errorf("rows=%d: rendered %d lines, more than the screen", rows, got)
		}
	}
}

// TestInspectCoversTheWholeParty guards that every party member — including
// benched ones — can be inspected, and that inspecting from the switch overlay
// returns to it afterwards.
func TestInspectCoversTheWholeParty(t *testing.T) {
	m := testModel(t, config.Default())
	feedFixtureUntilTurn(t, m, "gen9-singles.txt", 4)
	bv := m.activeBattle()
	if bv == nil {
		t.Fatal("no battle")
	}
	side := bv.state().MySide()
	if side == nil {
		t.Fatal("no side")
	}

	// Every party member is reachable by cycling the inspect overlay.
	seen := map[*battle.Pokemon]bool{}
	for _, p := range bv.inspectables() {
		seen[p] = true
	}
	for _, p := range side.Party {
		if !seen[p] {
			t.Errorf("%s is missing from the inspect cycle", p.Name)
		}
	}

	// Inspecting from the switch overlay opens the highlighted member and
	// returns there on esc.
	bv.overlay = overlaySwitch
	bv.overlayCursor = 2
	want := bv.switchOptions()[2].Pokemon
	bv.handleOverlayKey("i", m)
	if bv.overlay != overlayInspect {
		t.Fatalf("i in the switch overlay should open inspect, got %v", bv.overlay)
	}
	if got := bv.inspectables()[bv.inspectIndex]; got != want {
		t.Errorf("inspect opened on %s, want %s", got.Name, want.Name)
	}
	if out := stripANSI(bv.render(m.width, m.height-2, m.layout)); !strings.Contains(out, want.Name) {
		t.Errorf("inspect overlay did not render %s", want.Name)
	}
	bv.handleOverlayKey("esc", m)
	if bv.overlay != overlaySwitch {
		t.Errorf("esc should return to the switch overlay, got %v", bv.overlay)
	}

	// A benched Pokémon still shows its moveset, taken from the request.
	var bench *battle.Pokemon
	for _, p := range side.Party {
		if !p.Active && len(p.MoveIDs) > 0 {
			bench = p
			break
		}
	}
	if bench == nil {
		t.Fatal("no benched Pokémon with a known moveset")
	}
	bv.inspectPokemon(bench, overlayNone)
	out := stripANSI(bv.render(m.width, m.height-2, m.layout))
	if !strings.Contains(out, bv.moveName(bench.MoveIDs[0])) {
		t.Errorf("benched moveset not shown for %s", bench.Name)
	}
}

// TestInspectShowsFoePotential guards the opponent panel: since the foe's
// exact stats, item and ability are hidden, the overlay shows what is possible
// instead of blank cells.
func TestInspectShowsFoePotential(t *testing.T) {
	if _, err := os.Stat(filepath.Join(storage.DexDir(), "pokedex.json")); err != nil {
		t.Skip("dex cache not present; run `slowdown doctor` once")
	}
	d := dex.New(storage.DexDir())
	if err := d.Ensure(context.Background()); err != nil {
		t.Skipf("dex unavailable: %v", err)
	}
	m := New(config.Default(), Deps{Dex: d, Now: time.Now})
	m.width, m.height = 100, 44
	feedFixtureUntilTurn(t, m, "gen9-singles.txt", 4)
	bv := m.activeBattle()
	if bv == nil {
		t.Fatal("no battle")
	}
	foe := activeOf(bv.state().Opponent())
	if len(foe) == 0 {
		t.Fatal("no opponent active")
	}
	bv.inspectPokemon(foe[0], overlayNone)
	out := stripANSI(bv.render(m.width, m.height-2, m.layout))
	for _, want := range []string{"Ability", "Item", "Min", "Max"} {
		if !strings.Contains(out, want) {
			t.Errorf("foe inspect missing %q:\n%s", want, out)
		}
	}
	if bv.possibleAbilities(foe[0].Species) == "" {
		t.Errorf("no dex abilities found for %s", foe[0].Species)
	}
}

// TestChoiceShowsWaitingFeedback guards that a target-less choice is
// acknowledged on screen, and that further choice keys are ignored until the
// server moves the turn on.
func TestChoiceShowsWaitingFeedback(t *testing.T) {
	m := testModel(t, config.Default())
	feedFixtureUntilTurn(t, m, "gen9-singles.txt", 4)
	bv := m.activeBattle()
	if bv == nil {
		t.Fatal("no battle")
	}

	// A sent choice is described in the move area.
	bv.waiting = true
	bv.waitNote = "Shadow Ball"
	out := stripANSI(bv.render(m.width, m.height-2, m.layout))
	if !strings.Contains(out, "waiting for the opponent") {
		t.Errorf("no waiting feedback:\n%s", out)
	}
	if !strings.Contains(out, "Shadow Ball") {
		t.Errorf("chosen move is not named:\n%s", out)
	}

	// Choice keys are consumed and do nothing while waiting.
	for _, k := range []string{"1", "s", "t"} {
		if _, handled := bv.handleKey(keyMsg(k), m); !handled {
			t.Errorf("key %q should be consumed while waiting", k)
		}
	}
	if bv.overlay != overlayNone {
		t.Errorf("a choice key opened an overlay while waiting: %v", bv.overlay)
	}

	// Quitting stays reachable: q passes through to the app's quit prompt.
	if _, handled := bv.handleKey(keyMsg("q"), m); handled {
		t.Error("q should pass through to the quit prompt while waiting")
	}

	// The next turn clears the indicator.
	bv.apply(showdown.BattleTurn{Base: showdown.Base{RoomID: bv.room}, Turn: 5})
	if bv.waiting {
		t.Error("waiting should clear on the next turn")
	}
}

// TestChoiceNoteNamesTheMove checks the human-readable note built for the
// waiting indicator.
func TestChoiceNoteNamesTheMove(t *testing.T) {
	m := testModel(t, config.Default())
	feedFixtureUntilTurn(t, m, "gen9-singles.txt", 4)
	bv := m.activeBattle()
	req := bv.state().Request
	if req == nil {
		t.Fatal("no request")
	}
	note := bv.choiceNote(req, []battle.ChoiceSlot{{Kind: "move", Move: 1}})
	if !strings.Contains(note, "Shadow Ball") {
		t.Errorf("choice note = %q, want the move name", note)
	}
}

// TestMoveClassShowsCategoryAndPower checks the extra moveset detail.
func TestMoveClassShowsCategoryAndPower(t *testing.T) {
	if _, err := os.Stat(filepath.Join(storage.DexDir(), "moves.json")); err != nil {
		t.Skip("dex cache not present; run `slowdown doctor` once")
	}
	d := dex.New(storage.DexDir())
	if err := d.Ensure(context.Background()); err != nil {
		t.Skipf("dex unavailable: %v", err)
	}
	m := New(config.Default(), Deps{Dex: d, Now: time.Now})
	bv := m.battleFor("battle-x")

	if got := stripANSI(bv.moveClass("shadowball")); !strings.Contains(got, "Spec") || !strings.Contains(got, "80") {
		t.Errorf("shadowball class = %q, want Spec 80", got)
	}
	if got := stripANSI(bv.moveClass("nastyplot")); !strings.Contains(got, "Status") {
		t.Errorf("nastyplot class = %q, want Status", got)
	}
}

// TestEverySizeFitsAndKeepsTheStatusLine guards the bottom line. The status bar
// is rendered last, so it is the first thing lost when the view overflows: an
// over-tall view gets clipped by Bubble Tea and the keyboard hints disappear.
func TestEverySizeFitsAndKeepsTheStatusLine(t *testing.T) {
	m := testModel(t, config.Default())
	feedFixtureUntilTurn(t, m, "gen9-singles.txt", 4)

	for _, rows := range []int{14, 18, 24, 30, 45, 60} {
		for _, cols := range []int{40, 80, 120, 180} {
			m.width, m.height = cols, rows
			m.layout = LayoutForSize(cols, rows)

			out := m.render()
			if got := len(strings.Split(out, "\n")); got > rows {
				t.Errorf("cols=%d rows=%d (%s): rendered %d lines, taller than the screen",
					cols, rows, m.layout, got)
			}
			plain := stripANSI(out)
			if !strings.Contains(plain, "1-4 move") && !strings.Contains(plain, "esc lobby") {
				t.Errorf("cols=%d rows=%d (%s): status line missing", cols, rows, m.layout)
			}
			// The moves are essential and must survive the squeeze.
			if !strings.Contains(plain, "switch") {
				t.Errorf("cols=%d rows=%d (%s): move hints missing", cols, rows, m.layout)
			}
		}
	}
}

// finishBattle marks a battle room as over.
func finishBattle(t *testing.T, m *Model, room string) *battleView {
	t.Helper()
	bv := m.battleFor(room)
	bv.apply(showdown.BattleWin{
		Base: showdown.Base{RoomID: room},
		User: "someone",
	})
	if !bv.state().Ended {
		t.Fatalf("%s should be finished", room)
	}
	return bv
}

func TestTabSkipsFinishedBattles(t *testing.T) {
	m := testModel(t, config.Default())
	finishBattle(t, m, "battle-a")
	live := m.battleFor("battle-b")

	m.active = "battle-a"
	m.screen = screenBattle

	m.switchBattle()
	if m.active != "battle-b" {
		t.Errorf("tab landed on %q, want battle-b", m.active)
	}
	// With only one live battle, tab must stay there rather than cycling back
	// into the finished one.
	m.switchBattle()
	if m.active != "battle-b" {
		t.Errorf("tab cycled into a finished battle: %q", m.active)
	}
	if live.state().Ended {
		t.Fatal("test setup wrong: battle-b should be live")
	}
}

func TestTabWithOnlyFinishedBattlesReturnsToTheLobby(t *testing.T) {
	m := testModel(t, config.Default())
	finishBattle(t, m, "battle-a")
	m.active = "battle-a"
	m.screen = screenBattle

	m.switchBattle()
	if m.screen != screenLobby {
		t.Errorf("screen = %v, want the lobby", m.screen)
	}
}

func TestDismissRemovesABattleFromTheRotation(t *testing.T) {
	m := testModel(t, config.Default())
	finishBattle(t, m, "battle-a")
	m.active = "battle-a"
	m.screen = screenBattle

	m.dismissBattle("battle-a")

	if _, ok := m.battles["battle-a"]; ok {
		t.Error("battle was not removed")
	}
	if len(m.order) != 0 {
		t.Errorf("rotation still lists %v", m.order)
	}
	if m.screen != screenLobby {
		t.Errorf("screen = %v, want the lobby with nothing left", m.screen)
	}
}

func TestDismissFinishedClosesOnlyFinishedOnes(t *testing.T) {
	m := testModel(t, config.Default())
	finishBattle(t, m, "battle-a")
	finishBattle(t, m, "battle-b")
	m.battleFor("battle-c")

	m.dismissFinishedBattles()

	if _, ok := m.battles["battle-a"]; ok {
		t.Error("battle-a should be closed")
	}
	if _, ok := m.battles["battle-b"]; ok {
		t.Error("battle-b should be closed")
	}
	if _, ok := m.battles["battle-c"]; !ok {
		t.Error("a live battle must not be closed")
	}
}

func TestXClosesAFinishedBattle(t *testing.T) {
	m := testModel(t, config.Default())
	feedFixture(t, m, "gen9-singles.txt")
	bv := m.activeBattle()
	if bv == nil || !bv.state().Ended {
		t.Fatal("fixture should end with a finished battle")
	}
	room := bv.room

	if _, handled := bv.handleKey(keyMsg("x"), m); !handled {
		t.Fatal("x should be handled on the result screen")
	}
	if _, ok := m.battles[room]; ok {
		t.Error("x did not close the battle")
	}
}

func TestPaletteOffersToCloseFinishedBattles(t *testing.T) {
	m := testModel(t, config.Default())
	finishBattle(t, m, "battle-a")
	m.active = "battle-a"

	var titles []string
	for _, c := range m.commands() {
		titles = append(titles, c.title)
	}
	joined := strings.Join(titles, "\n")
	if !strings.Contains(joined, "Close this finished battle") {
		t.Errorf("missing a close command:\n%s", joined)
	}
	if !strings.Contains(joined, "Close all finished battles") {
		t.Errorf("missing a close-all command:\n%s", joined)
	}
}
