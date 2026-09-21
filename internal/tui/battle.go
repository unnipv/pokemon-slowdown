package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/unnipv/pokemon-slowdown/internal/battle"
	"github.com/unnipv/pokemon-slowdown/internal/config"
	"github.com/unnipv/pokemon-slowdown/internal/dex"
	"github.com/unnipv/pokemon-slowdown/internal/showdown"
	"github.com/unnipv/pokemon-slowdown/internal/sprites"
	"time"
)

// overlayKind selects the battle overlay currently open.
type overlayKind int

const (
	overlayNone overlayKind = iota
	overlaySwitch
	overlayInspect
	overlayLog
	overlayChat
	overlayTarget
)

type chatLine struct {
	user string
	text string
	me   bool
	at   int64
}

// spriteRequest is a sprite the battle needs rendered.
type spriteRequest struct {
	Key    string
	Sprite sprites.Ref
}

// battleView owns one battle: its deterministic state plus all UI state.
type battleView struct {
	room    string
	owner   *Model
	theme   Theme
	deps    Deps
	reducer *battle.Reducer

	curLayout LayoutMode
	curWidth  int
	curHeight int

	overlay       overlayKind
	overlayCursor int

	draft           map[int]battle.ChoiceSlot
	slot            int
	pendingMove     int
	pendingMechanic string
	pendingSlot     int
	targets         []battle.TargetOption

	logScroll int
	chat      []chatLine
	chatInput string

	spriteRendered map[string]string
	spriteFrames   map[string]int
	spritePayload  map[string]string
	loading        map[string]bool

	previewOrder []int

	animFrame int
	titleText string
	lastError string

	// timerSeenAt is when the last timer message arrived, so the countdown can
	// tick locally between server messages.
	timerSeenAt time.Time
	// inspectIndex selects which Pokémon the inspect overlay shows.
	inspectIndex int
	// inspectBack is the overlay to return to when inspect closes. It is
	// overlaySwitch when inspect was opened from the switch overlay, so the
	// user lands back on the party list with their cursor intact.
	inspectBack overlayKind
}

func newBattleView(room string, owner *Model) *battleView {
	return &battleView{
		room:           room,
		owner:          owner,
		theme:          owner.theme,
		deps:           owner.deps,
		reducer:        battle.NewReducer(room),
		draft:          map[int]battle.ChoiceSlot{},
		spriteRendered: map[string]string{},
		spriteFrames:   map[string]int{},
		spritePayload:  map[string]string{},
		loading:        map[string]bool{},
	}
}

// tagline returns a line for a slot using the owner's configuration.
func (bv *battleView) tagline(slot config.TaglineSlot) string {
	if bv.owner == nil {
		return ""
	}
	return bv.owner.cfg.Tagline(slot, bv.owner.cfg.TaglinesRandom)
}

// wonByMe reports whether the battle winner is us.
func (bv *battleView) wonByMe(s *battle.State) bool {
	if s.Winner == "" || bv.owner == nil || bv.owner.username == "" {
		return false
	}
	return showdown.ToID(s.Winner) == showdown.ToID(bv.owner.username)
}

func (bv *battleView) state() *battle.State { return bv.reducer.State }

func (bv *battleView) apply(ev showdown.Event) {
	bv.reducer.Debug = bv.deps.Debug
	if title, ok := ev.(showdown.RoomTitle); ok {
		bv.titleText = SanitizeLine(title.Title)
	}
	bv.reducer.Apply(ev)

	// Reset per-request UI state when a new request arrives.
	if _, ok := ev.(showdown.BattleRequest); ok {
		bv.draft = map[int]battle.ChoiceSlot{}
		bv.slot = 0
		bv.overlay = overlayNone
		bv.overlayCursor = 0
	}
	if e, ok := ev.(showdown.BattleError); ok {
		bv.lastError = SanitizeLine(e.Message)
	}
	if _, ok := ev.(showdown.BattleInactive); ok {
		bv.timerSeenAt = time.Now()
	}
	if _, ok := ev.(showdown.BattleStart); ok {
		bv.previewOrder = nil
	}
}

func (bv *battleView) title() string {
	s := bv.state()
	if bv.titleText != "" {
		return bv.titleText
	}
	if s.Tier != "" {
		return s.Tier
	}
	return bv.room
}

func (bv *battleView) addChat(user, text string, me bool, at int64) {
	// Commands like /data reply with HTML. Render it as text so the output is
	// readable instead of a wall of markup.
	if looksLikeHTML(text) {
		text = htmlToText(text)
	}
	bv.chat = append(bv.chat, chatLine{
		user: SanitizeLine(user),
		text: SanitizeLine(text),
		me:   me,
		at:   at,
	})
	if len(bv.chat) > 500 {
		bv.chat = bv.chat[len(bv.chat)-500:]
	}
}

func (bv *battleView) markLoading(key string) { bv.loading[key] = true }
func (bv *battleView) isLoading(key string) bool {
	return bv.loading[key]
}

func (bv *battleView) receiveSprite(msg spriteReadyMsg) {
	delete(bv.loading, msg.key)
	if msg.err != nil || msg.sprite == nil {
		return
	}
	bv.spriteFrames[msg.key] = 0
	bv.renderSprite(msg.key, msg.sprite)
}

// wantedSprites lists the sprites the current view needs.
func (bv *battleView) wantedSprites(animate bool) []spriteRequest {
	s := bv.state()
	var out []spriteRequest
	add := func(p *battle.Pokemon, back bool) {
		if p == nil || p.Species == "" {
			return
		}
		ref := bv.spriteRef(p, back, animate)
		out = append(out, spriteRequest{Key: ref.Key(), Sprite: ref})
	}
	for _, p := range s.P1.ActiveParty() {
		add(p, s.Me == p.SideID)
	}
	for _, p := range s.P2.ActiveParty() {
		add(p, s.Me == p.SideID)
	}
	if s.TeamPreview {
		for _, p := range s.P1.Party {
			add(p, false)
		}
		for _, p := range s.P2.Party {
			add(p, false)
		}
	}
	return out
}

func (bv *battleView) spriteRef(p *battle.Pokemon, back, animate bool) sprites.Ref {
	id := dex.ToID(p.Species)
	if bv.deps.Dex != nil {
		if sp, ok := bv.deps.Dex.Species(p.Species); ok {
			id = sp.SpriteID()
		}
	}
	return sprites.Ref{ID: id, Shiny: p.Shiny, Back: back, Animated: animate}
}

// renderSprite renders and caches one sprite block.
func (bv *battleView) renderSprite(key string, sprite *sprites.Sprite) {
	if bv.deps.Renderer == nil {
		return
	}
	frame := 0
	if sprite.Animated {
		frame = bv.spriteFrames[key] % sprite.FrameCount()
	}
	cols, rows := bv.spriteCells()
	if cols == 0 {
		return
	}
	out, err := bv.deps.Renderer.Render(sprite.Frame(frame), cols, rows)
	if err != nil {
		return
	}
	bv.spriteRendered[key] = out
}

// advanceAnimation moves animated sprites to their next frame.
func (bv *battleView) advanceAnimation(global int) {
	if bv.deps.Sprites == nil || bv.deps.Renderer == nil {
		return
	}
	// Out-of-band graphics are drawn as a static frame; re-sending them on
	// every tick would flicker.
	if sprites.SupportsPayload(bv.deps.Renderer) {
		return
	}
	bv.animFrame++
	if bv.animFrame%2 != 0 {
		return
	}
	for key, frame := range bv.spriteFrames {
		ref := bv.refForKey(key)
		sp, ok := bv.deps.Sprites.Cached(ref)
		if !ok || !sp.Animated {
			continue
		}
		bv.spriteFrames[key] = frame + 1
		bv.renderSprite(key, sp)
	}
}

// refForKey reconstructs a sprite ref from its cache key.
func (bv *battleView) refForKey(key string) sprites.Ref {
	parts := strings.Split(key, "|")
	if len(parts) < 2 {
		return sprites.Ref{ID: key}
	}
	kind := parts[1]
	return sprites.Ref{
		ID:       parts[0],
		Back:     strings.Contains(kind, "back"),
		Shiny:    strings.Contains(kind, "shiny"),
		Animated: strings.Contains(kind, "ani"),
	}
}

// ---------------------------------------------------------------------------
// Input
// ---------------------------------------------------------------------------

// handleKey processes a key press. It reports whether the key was consumed.
func (bv *battleView) handleKey(msg tea.KeyPressMsg, m *Model) (tea.Cmd, bool) {
	key := msg.String()
	s := bv.state()

	if bv.overlay != overlayNone {
		return bv.handleOverlayKey(key, m), true
	}

	// Team preview takes priority: it is a different interaction entirely.
	if s.TeamPreview {
		return bv.handlePreviewKey(key, m), true
	}

	// After a battle, enter queues the same format again. That is the "next
	// battle" action; tab is only for switching between battles already open.
	if s.Ended {
		switch key {
		case "enter", "r":
			id := showdown.ToID(s.Tier)
			if bv.owner == nil {
				return nil, true
			}
			if id == "" {
				id = bv.owner.cfg.DefaultFormat
			}
			return bv.owner.requeue(id), true
		case "x", "d":
			// Close the battle so it stops appearing when switching battles.
			if bv.owner != nil {
				bv.owner.dismissBattle(bv.room)
			}
			return nil, true
		}
	}

	switch key {
	case "esc":
		m.screen = screenLobby
		return nil, true
	case "?":
		m.helpOpen = true
		return nil, true
	case "l":
		bv.overlay = overlayLog
		bv.logScroll = len(s.Log)
		return nil, true
	case "c":
		bv.overlay = overlayChat
		return nil, true
	case "i":
		bv.inspectPokemon(nil, overlayNone)
		return nil, true
	case "s":
		if bv.canSwitch() {
			bv.overlay = overlaySwitch
			bv.overlayCursor = 0
		}
		return nil, true
	case "tab":
		m.switchBattle()
		return nil, true
	}

	// Mechanic toggle applies to the pending move.
	if key == "t" {
		if bv.toggleMechanic() {
			return nil, true
		}
	}

	if n := digit(key); n > 0 {
		return bv.chooseMove(n, m), true
	}
	return nil, false
}

func (bv *battleView) handlePreviewKey(key string, m *Model) tea.Cmd {
	s := bv.state()
	if s.MySide() == nil {
		return nil
	}
	n := len(s.MySide().Party)
	switch key {
	case "enter", " ":
		return bv.submitPreview()
	case "esc":
		// Accept the default order rather than stranding the user.
		bv.previewOrder = nil
		return bv.submitPreview()
	}
	if d := digit(key); d > 0 && d <= n {
		bv.togglePreviewPick(d)
	}
	return nil
}

func (bv *battleView) togglePreviewPick(slot int) {
	found := -1
	for i, v := range bv.previewOrder {
		if v == slot {
			found = i
			break
		}
	}
	if found >= 0 {
		bv.previewOrder = append(bv.previewOrder[:found], bv.previewOrder[found+1:]...)
		return
	}
	bv.previewOrder = append(bv.previewOrder, slot)
}

func (bv *battleView) submitPreview() tea.Cmd {
	s := bv.state()
	if s.MySide() == nil || bv.deps.Client == nil {
		return nil
	}
	n := len(s.MySide().Party)
	order := make([]int, 0, n)
	seen := map[int]bool{}
	for _, v := range bv.previewOrder {
		if !seen[v] {
			order = append(order, v)
			seen[v] = true
		}
	}
	for i := 1; i <= n; i++ {
		if !seen[i] {
			order = append(order, i)
		}
	}
	rqid := 0
	if s.Request != nil {
		rqid = s.Request.RqID
	}
	return func() tea.Msg {
		_ = bv.deps.Client.Choose(bv.room, battle.TeamChoice(order).String(), rqid)
		return nil
	}
}

// canSwitch reports whether a switch is legal right now.
func (bv *battleView) canSwitch() bool {
	s := bv.state()
	req := s.Request
	if req == nil {
		return false
	}
	switch req.Kind() {
	case battle.RequestSwitch, battle.RequestMove:
		return true
	default:
		return false
	}
}

// toggleMechanic enables the primary mechanic for the current slot.
func (bv *battleView) toggleMechanic() bool {
	req := bv.state().Request
	if req == nil || req.Kind() != battle.RequestMove {
		return false
	}
	ar := req.ActiveAt(bv.slot)
	mech, ok := battle.AvailableMechanics(ar).Primary()
	if !ok {
		return false
	}
	if bv.pendingMechanic == mech.Kind {
		bv.pendingMechanic = ""
		return true
	}
	bv.pendingMechanic = mech.Kind
	return true
}

// chooseMove records a move choice for the current slot.
func (bv *battleView) chooseMove(move int, m *Model) tea.Cmd {
	s := bv.state()
	req := s.Request
	if req == nil || req.Kind() != battle.RequestMove {
		return nil
	}
	ar := req.ActiveAt(bv.slot)
	if ar == nil || move < 1 || move > len(ar.Moves) {
		return nil
	}
	mv := ar.Moves[move-1]
	if mv.Disabled.Set {
		return nil
	}

	targets := battle.LegalTargets(mv, bv.slot, bv.activeCount(s.MySide()), bv.activeCount(s.Opponent()))
	if len(targets) > 0 {
		bv.overlay = overlayTarget
		bv.targets = targets
		bv.overlayCursor = 0
		bv.pendingMove = move
		bv.pendingSlot = bv.slot
		return nil
	}
	return bv.commitMove(bv.slot, move, "")
}

func (bv *battleView) activeCount(side *battle.Side) int {
	if side == nil {
		return 0
	}
	return len(side.ActiveParty())
}

// commitMove stores a completed move decision and submits when ready.
func (bv *battleView) commitMove(slot, move int, target string) tea.Cmd {
	bv.draft[slot] = battle.ChoiceSlot{
		Kind:     "move",
		Move:     move,
		Target:   target,
		Mechanic: bv.pendingMechanic,
	}
	bv.pendingMove = 0
	bv.pendingMechanic = ""
	return bv.maybeSubmit()
}

// commitSwitch stores a switch decision and submits when ready.
func (bv *battleView) commitSwitch(partySlot int) tea.Cmd {
	s := bv.state()
	req := s.Request
	if req == nil {
		return nil
	}
	if req.Kind() == battle.RequestSwitch {
		// Forced switch: one decision, submit immediately.
		return bv.sendChoice(battle.Choice{Slots: []battle.ChoiceSlot{{Kind: "switch", Switch: partySlot}}})
	}
	bv.draft[bv.slot] = battle.ChoiceSlot{Kind: "switch", Switch: partySlot}
	return bv.maybeSubmit()
}

// maybeSubmit sends the choice once every required slot has a decision.
func (bv *battleView) maybeSubmit() tea.Cmd {
	s := bv.state()
	req := s.Request
	if req == nil {
		return nil
	}
	need := req.SlotCount()
	slots := make([]battle.ChoiceSlot, 0, need)
	for i := 0; i < need; i++ {
		slot, ok := bv.draft[i]
		if !ok {
			if req.Kind() == battle.RequestMove {
				// A fainted or empty slot may be passed.
				slots = append(slots, battle.ChoiceSlot{Kind: "pass"})
				continue
			}
			return nil
		}
		slots = append(slots, slot)
	}
	return bv.sendChoice(battle.Choice{Slots: slots})
}

func (bv *battleView) sendChoice(choice battle.Choice) tea.Cmd {
	s := bv.state()
	if bv.deps.Client == nil {
		return nil
	}
	rqid := 0
	if s.Request != nil {
		rqid = s.Request.RqID
	}
	text := choice.String()
	bv.draft = map[int]battle.ChoiceSlot{}
	bv.slot = 0
	return func() tea.Msg {
		_ = bv.deps.Client.Choose(bv.room, text, rqid)
		return nil
	}
}

// handleOverlayKey routes keys while an overlay is open.
func (bv *battleView) handleOverlayKey(key string, m *Model) tea.Cmd {
	s := bv.state()
	switch bv.overlay {
	case overlaySwitch:
		slots := bv.switchOptions()
		switch key {
		case "esc", "s":
			bv.overlay = overlayNone
		case "up", "k":
			bv.overlayCursor = clamp(bv.overlayCursor-1, 0, len(slots)-1)
		case "down", "j":
			bv.overlayCursor = clamp(bv.overlayCursor+1, 0, len(slots)-1)
		case "enter":
			if bv.overlayCursor < len(slots) && slots[bv.overlayCursor].Legal {
				cmd := bv.commitSwitch(slots[bv.overlayCursor].Index)
				bv.overlay = overlayNone
				return cmd
			}
		case "i":
			// Inspect the highlighted party member without leaving the
			// switch list; esc in inspect comes back here.
			if bv.overlayCursor < len(slots) {
				bv.inspectPokemon(slots[bv.overlayCursor].Pokemon, overlaySwitch)
			}
		default:
			if d := digit(key); d > 0 {
				for _, sl := range slots {
					if sl.Index == d && sl.Legal {
						cmd := bv.commitSwitch(d)
						bv.overlay = overlayNone
						return cmd
					}
				}
			}
		}
	case overlayTarget:
		switch key {
		case "esc":
			bv.overlay = overlayNone
			bv.pendingMove = 0
		case "up", "k":
			bv.overlayCursor = clamp(bv.overlayCursor-1, 0, len(bv.targets)-1)
		case "down", "j":
			bv.overlayCursor = clamp(bv.overlayCursor+1, 0, len(bv.targets)-1)
		case "enter":
			if bv.overlayCursor < len(bv.targets) {
				t := bv.targets[bv.overlayCursor]
				bv.overlay = overlayNone
				return bv.commitMove(bv.pendingSlot, bv.pendingMove, t.Spec)
			}
		default:
			if d := digit(key); d > 0 && d <= len(bv.targets) {
				t := bv.targets[d-1]
				bv.overlay = overlayNone
				return bv.commitMove(bv.pendingSlot, bv.pendingMove, t.Spec)
			}
		}
	case overlayLog:
		switch key {
		case "esc", "l":
			bv.overlay = overlayNone
		case "up", "k":
			bv.logScroll = clamp(bv.logScroll-1, 0, len(s.Log))
		case "down", "j":
			bv.logScroll = clamp(bv.logScroll+1, 0, len(s.Log))
		case "pgup":
			bv.logScroll = clamp(bv.logScroll-8, 0, len(s.Log))
		case "pgdown":
			bv.logScroll = clamp(bv.logScroll+8, 0, len(s.Log))
		}
	case overlayInspect:
		switch key {
		case "esc", "i":
			back := bv.inspectBack
			bv.inspectBack = overlayNone
			bv.overlay = back
		case "up", "k", "left":
			bv.inspectIndex--
			if bv.inspectIndex < 0 {
				bv.inspectIndex = max(0, len(bv.inspectables())-1)
			}
		case "down", "j", "right", "tab":
			bv.inspectIndex++
			if n := len(bv.inspectables()); n > 0 && bv.inspectIndex >= n {
				bv.inspectIndex = 0
			}
		}
	case overlayChat:
		switch key {
		case "esc":
			bv.overlay = overlayNone
			bv.chatInput = ""
		case "enter":
			text := strings.TrimSpace(bv.chatInput)
			bv.chatInput = ""
			if text != "" && bv.deps.Client != nil {
				room := bv.room
				client := bv.deps.Client
				return func() tea.Msg {
					_ = client.Chat(room, text)
					return nil
				}
			}
		case "backspace":
			if len(bv.chatInput) > 0 {
				bv.chatInput = bv.chatInput[:len(bv.chatInput)-1]
			}
		case "space":
			bv.chatInput += " "
		default:
			if r := msg2rune(key); r != 0 {
				bv.chatInput += string(r)
			}
		}
	}
	return nil
}

func digit(s string) int {
	if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		return int(s[0] - '0')
	}
	return 0
}

// printable returns the rune for a single-character key, or 0.
func printable(s string) rune {
	r := []rune(s)
	if len(r) != 1 {
		return 0
	}
	if r[0] < 0x20 || r[0] == 0x7f {
		return 0
	}
	return r[0]
}

func msg2rune(s string) rune { return printable(s) }
