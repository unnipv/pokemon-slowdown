package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/unnipv/pokemon-slowdown/internal/battle"
	"strconv"
)

// renderOverlay draws the active overlay over the battle body.
func (bv *battleView) renderOverlay(base string, width, height int, layout LayoutMode) string {
	var box string
	switch bv.overlay {
	case overlaySwitch:
		box = bv.renderSwitchOverlay(width)
	case overlayInspect:
		box = bv.renderInspectOverlay(width)
	case overlayLog:
		box = bv.renderLogOverlay(width, height)
	case overlayChat:
		box = bv.renderChatOverlay(width, height)
	case overlayTarget:
		box = bv.renderTargetOverlay(width)
	}
	if box == "" {
		return base
	}
	return overlayAt(base, box, height)
}

// overlayAt replaces lines of the base with the overlay, positioned near the
// top third but always clamped so the whole overlay is visible.
func overlayAt(base, box string, height int) string {
	boxLines := strings.Split(box, "\n")
	baseLines := strings.Split(base, "\n")
	out := make([]string, len(baseLines))
	copy(out, baseLines)

	maxStart := len(out) - len(boxLines)
	if maxStart < 0 {
		maxStart = 0
	}
	start := (height - len(boxLines)) / 3
	if start < 0 {
		start = 0
	}
	if start > maxStart {
		start = maxStart
	}
	for i, ln := range boxLines {
		idx := start + i
		if idx >= 0 && idx < len(out) {
			out[idx] = ln
		}
	}
	return strings.Join(out, "\n")
}

func (bv *battleView) overlayFrame(title string, width int, body []string) string {
	if width < 20 {
		width = 20
	}
	// An overlay does not need the whole screen; a box much wider than its
	// content is just a lot of empty space.
	if width > 78 {
		width = 78
	}

	// The box is len(body) + 6 lines tall: a title, a blank, the body, a blank,
	// the hint, and two borders. Trim the body so the bottom border is never
	// pushed off the screen.
	maxBody := bv.curHeight - 6
	if bv.curHeight == 0 {
		maxBody = len(body)
	}
	if maxBody < 4 {
		maxBody = 4
	}
	if len(body) > maxBody {
		trimmed := append([]string{}, body[:maxBody-1]...)
		trimmed = append(trimmed, bv.theme.Dim.Render(
			fmt.Sprintf("… %d more lines", len(body)-(maxBody-1))))
		body = trimmed
	}
	inner := width - 6
	t := bv.theme
	head := t.Title.Render(title)
	lines := []string{head, ""}
	lines = append(lines, body...)
	lines = append(lines, "", t.Dim.Render("esc close"))
	content := strings.Join(lines, "\n")
	return t.BoxFocus.Width(inner).Padding(0, 2).Render(content)
}

// switchOption is one party member in the switch overlay, built from battle
// state rather than from the request so it always carries a name, types, HP
// and status.
type switchOption struct {
	Index   int
	Pokemon *battle.Pokemon
	Legal   bool
}

func (bv *battleView) switchOptions() []switchOption {
	side := bv.state().MySide()
	if side == nil {
		return nil
	}
	out := make([]switchOption, 0, len(side.Party))
	for i, p := range side.Party {
		out = append(out, switchOption{
			Index:   i + 1,
			Pokemon: p,
			Legal:   !p.Fainted && !p.Active,
		})
	}
	return out
}

func (bv *battleView) renderSwitchOverlay(width int) string {
	t := bv.theme
	opts := bv.switchOptions()
	if len(opts) == 0 {
		return bv.overlayFrame("Switch", width, []string{t.Muted.Render("No party information yet.")})
	}

	nameW := 16
	if width < 60 {
		nameW = 12
	}

	var body []string
	for i, o := range opts {
		p := o.Pokemon
		cursor := "  "
		if i == bv.overlayCursor {
			cursor = t.Accent.Render("> ")
		}

		name := SanitizeLine(p.Name)
		if g := genderGlyph(p.Gender); g != "" {
			name += " " + g
		}
		padded := padRight(name, nameW)

		if !o.Legal {
			reason := "unavailable"
			switch {
			case p.Fainted:
				reason = "fainted"
			case p.Active:
				reason = "in battle"
			}
			body = append(body, t.Disabled.Render(
				fmt.Sprintf("  [%d] %s %s", o.Index, padded, reason)))
			continue
		}

		line := fmt.Sprintf("%s[%d] %s", cursor, o.Index, t.Fg.Bold(true).Render(padded))
		if types := bv.typeList(p.Species, LayoutStandard); types != "" {
			line += " " + types
		}
		line += " " + bv.hpBar(p.HPPercent, 8) + " " + bv.hpText(p, false)
		if st := bv.statusBadge(p.Status); st != "" {
			line += " " + st
		}
		body = append(body, line)
	}
	body = append(body, "", t.Dim.Render("enter choose  ·  i inspect"))
	return bv.overlayFrame("Switch", width, body)
}

// inspectables returns the Pokémon the inspect overlay cycles through: the
// opponent's active Pokémon first, then ours — the active ones, followed by
// the rest of the party so every team member can be examined.
func (bv *battleView) inspectables() []*battle.Pokemon {
	s := bv.state()
	var out []*battle.Pokemon
	out = append(out, activeOf(s.Opponent())...)
	out = append(out, activeOf(s.MySide())...)
	out = append(out, benchOf(s.MySide())...)
	return out
}

// benchOf returns a side's non-active party members in party order, including
// fainted ones: the inspect overlay shows the whole team, not only what can
// still fight.
func benchOf(side *battle.Side) []*battle.Pokemon {
	if side == nil {
		return nil
	}
	var out []*battle.Pokemon
	for _, p := range side.Party {
		if !p.Active {
			out = append(out, p)
		}
	}
	return out
}

// inspectPokemon opens the inspect overlay. A nil Pokémon keeps the current
// selection; otherwise the overlay jumps to that Pokémon. back is the overlay
// to restore when inspect closes (overlayNone for the global inspect key).
func (bv *battleView) inspectPokemon(p *battle.Pokemon, back overlayKind) {
	bv.inspectBack = back
	bv.overlay = overlayInspect
	if p == nil {
		return
	}
	for i, q := range bv.inspectables() {
		if q == p {
			bv.inspectIndex = i
			return
		}
	}
	bv.inspectIndex = 0
}

func (bv *battleView) renderInspectOverlay(width int) string {
	t := bv.theme
	list := bv.inspectables()
	if len(list) == 0 {
		return bv.overlayFrame("Inspect", width, []string{t.Muted.Render("Nothing to inspect.")})
	}
	if bv.inspectIndex >= len(list) {
		bv.inspectIndex = 0
	}
	p := list[bv.inspectIndex]
	mine := bv.isMine(p)

	title := SanitizeLine(p.Name)
	if g := genderGlyph(p.Gender); g != "" {
		title += " " + g
	}
	if types := bv.typeList(p.Species, LayoutStandard); types != "" {
		title += "  " + types
	}

	body := []string{t.Fg.Bold(true).Render(title)}
	if len(list) > 1 {
		body = append(body, t.Dim.Render(fmt.Sprintf("  %d of %d  ·  ↑/↓ to cycle",
			bv.inspectIndex+1, len(list))))
	}
	body = append(body, "")

	body = append(body, fmt.Sprintf("HP       %s %s", bv.hpBar(p.HPPercent, 12), bv.hpText(p, !mine)))
	body = append(body, fmt.Sprintf("Status   %s", orDash(bv.statusBadge(p.Status))))
	if p.Level > 0 {
		body = append(body, fmt.Sprintf("Level    %d", p.Level))
	}
	if p.Terastallized {
		body = append(body, "Tera     "+t.Primary.Render(p.TeraType))
	}
	if b := bv.boostText(p); b != "" {
		body = append(body, "Boosts   "+b)
	}
	if p.Item != "" {
		body = append(body, "Item     "+t.Muted.Render(p.Item))
	}
	if p.Ability != "" {
		body = append(body, "Ability  "+t.Muted.Render(p.Ability))
	}

	body = append(body, "")
	body = append(body, bv.renderStatTable(p, mine)...)
	body = append(body, "")

	if mine {
		body = append(body, t.Muted.Render("Moves"))
		switch {
		case len(p.Moves) > 0:
			for _, mv := range p.Moves {
				line := fmt.Sprintf("  %-18s %d/%d", mv.Name, mv.PP, mv.MaxPP)
				if mv.Disabled {
					line = t.Disabled.Render(line + "  disabled")
				}
				body = append(body, line)
			}
		case len(p.MoveIDs) > 0:
			// A benched Pokémon has a known moveset but no PP yet, because the
			// server only reports PP for the active slot.
			for _, id := range p.MoveIDs {
				body = append(body, "  "+bv.moveName(id))
			}
			body = append(body, t.Dim.Render("  PP shown while active"))
		default:
			body = append(body, t.Dim.Render("  (not yet known)"))
		}
	} else {
		body = append(body, t.Muted.Render(fmt.Sprintf("Revealed moves (%d)", len(p.SeenMoves))))
		if len(p.SeenMoves) == 0 {
			body = append(body, t.Dim.Render("  none seen yet"))
		}
		for _, m := range p.SeenMoves {
			body = append(body, "  "+t.Fg.Render(m))
		}
		body = append(body, "", t.Dim.Render("Only what the battle has revealed is shown."))
	}
	return bv.overlayFrame("Inspect", width, body)
}

// renderStatTable shows base stats from the dex, the absolute current stats the
// server reports for our own Pokémon, and the value after in-battle stat
// changes.
func (bv *battleView) renderStatTable(p *battle.Pokemon, mine bool) []string {
	t := bv.theme
	var base map[string]int
	if bv.deps.Dex != nil {
		if sp, ok := bv.deps.Dex.Species(p.Species); ok {
			base = sp.BaseStats
		}
	}

	head := "  " + padRight("Stat", 8) + " " + padLeft("Base", 5) + " " + padLeft("Actual", 7) + " " + padLeft("In battle", 11)
	out := []string{t.Muted.Render(head)}

	for _, k := range battle.StatKeys {
		label := battle.StatLabels[k]
		if label == "" {
			label = k
		}
		baseVal := "-"
		if v, ok := base[k]; ok {
			baseVal = strconv.Itoa(v)
		}

		actual := "-"
		if v, ok := p.Stats[k]; ok {
			actual = strconv.Itoa(v)
		} else if mine && k == "hp" && p.MaxHP > 0 {
			actual = strconv.Itoa(p.MaxHP)
		} else if !mine {
			actual = "?"
		}

		effective := "-"
		switch {
		case k == "hp":
			switch {
			case mine && p.MaxHP > 0:
				effective = fmt.Sprintf("%d/%d", p.HP, p.MaxHP)
			case p.HPPercent > 0:
				effective = fmt.Sprintf("%d%%", p.HPPercent)
			}
		case p.Stats[k] > 0:
			v := p.Stats[k]
			if n := p.Boosts[k]; n != 0 {
				eff := int(float64(v)*battle.BoostMultiplier(n) + 0.5)
				text := fmt.Sprintf("%d %+d", eff, n)
				if n > 0 {
					effective = t.Success.Render(text)
				} else {
					effective = t.Danger.Render(text)
				}
			} else {
				effective = t.Muted.Render(strconv.Itoa(v))
			}
		case !mine:
			// The opponent's stats are not revealed; only public base stats are.
			effective = t.Dim.Render("?")
		}

		// Pad by display width, not byte length: styled values carry escape
		// sequences that would otherwise blow out the columns.
		out = append(out, "  "+padRight(label, 8)+" "+
			padLeft(baseVal, 5)+" "+padLeft(actual, 7)+" "+padLeft(effective, 11))
	}
	if !mine {
		out = append(out, "", t.Dim.Render("  Opponent stats are not revealed."))
	}
	return out
}

func (bv *battleView) isMine(p *battle.Pokemon) bool {
	s := bv.state()
	return s.Me != "" && p.SideID == s.Me
}

// moveName resolves a move ID to its display name, falling back to the raw ID
// when the dex is not available.
func (bv *battleView) moveName(id string) string {
	if bv.deps.Dex != nil {
		if m, ok := bv.deps.Dex.Move(id); ok && m.Name != "" {
			return m.Name
		}
	}
	return id
}

func (bv *battleView) renderLogOverlay(width, height int) string {
	s := bv.state()
	t := bv.theme
	rows := height - 8
	if rows < 4 {
		rows = 4
	}
	end := clamp(bv.logScroll, 0, len(s.Log))
	start := end - rows
	if start < 0 {
		start = 0
	}
	var body []string
	for _, e := range s.Log[start:end] {
		body = append(body, bv.styleLogLine(e))
	}
	if len(body) == 0 {
		body = append(body, t.Muted.Render("(nothing yet)"))
	}
	body = append(body, "", t.Dim.Render("↑/↓ scroll"))
	return bv.overlayFrame("Battle log", width, body)
}

func (bv *battleView) styleLogLine(e battle.LogEntry) string {
	t := bv.theme
	text := SanitizeLine(e.Text)
	if text == "" {
		return ""
	}
	switch e.Kind {
	case "move":
		return t.Fg.Render(text)
	case "damage", "faint":
		return t.Danger.Render(text)
	case "heal":
		return t.Success.Render(text)
	case "status", "boost":
		return t.Warning.Render(text)
	case "crit", "effect", "tera", "zmove", "mega":
		return t.Accent.Render(text)
	case "weather", "field", "side":
		return t.Primary.Render(text)
	case "turn":
		return t.Title.Render(text)
	case "error":
		return t.Danger.Render(text)
	case "hint", "spacer":
		return t.Dim.Render(text)
	default:
		return t.Muted.Render(text)
	}
}

func (bv *battleView) renderChatOverlay(width, height int) string {
	t := bv.theme
	rows := height - 10
	if rows < 3 {
		rows = 3
	}
	start := len(bv.chat) - rows
	if start < 0 {
		start = 0
	}
	var body []string
	for _, c := range bv.chat[start:] {
		name := t.Muted.Render(SanitizeLine(c.user))
		if c.me {
			name = t.Primary.Render(SanitizeLine(c.user))
		}
		// Converted command output can span several lines.
		parts := strings.Split(c.text, "\n")
		body = append(body, name+": "+t.Fg.Render(parts[0]))
		indent := strings.Repeat(" ", lipgloss.Width(name)+2)
		for _, cont := range parts[1:] {
			body = append(body, indent+t.Fg.Render(cont))
		}
	}
	if len(body) == 0 {
		body = append(body, t.Muted.Render("(no messages)"))
	}
	body = append(body, "", t.Accent.Render("› ")+t.Fg.Render(bv.chatInput)+t.Primary.Render("▌"))
	return bv.overlayFrame("Chat", width, body)
}

func (bv *battleView) renderTargetOverlay(width int) string {
	s := bv.state()
	t := bv.theme
	var body []string
	for i, opt := range bv.targets {
		cursor := "  "
		if i == bv.overlayCursor {
			cursor = t.Accent.Render("> ")
		}
		label := bv.targetLabel(opt)
		side := "ally"
		if opt.Foe {
			side = "foe"
		}
		line := fmt.Sprintf("%s[%d] %s  %s", cursor, i+1, t.Fg.Render(label), t.Muted.Render(side))
		body = append(body, line)
	}
	_ = s
	return bv.overlayFrame("Choose a target", width, body)
}

func (bv *battleView) targetLabel(opt battle.TargetOption) string {
	s := bv.state()
	var side *battle.Side
	if opt.Foe {
		side = s.Opponent()
	} else {
		side = s.MySide()
	}
	if side != nil {
		if p := side.ActiveAt(opt.Slot); p != nil {
			return SanitizeLine(p.Name)
		}
	}
	return opt.Spec
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
