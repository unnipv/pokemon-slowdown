package battle

// Mechanic is a generation-specific battle mechanic exposed by a request.
type Mechanic struct {
	// Kind is the protocol token appended to a move choice: "mega",
	// "ultra", "zmove", "max" or "terastallize".
	Kind string
	// Label is a short human label, e.g. "Mega", "Z-Move", "Dynamax", "Tera".
	Label string
	// Detail carries extra information such as the Tera type.
	Detail string
}

// Mechanics describes every mechanic available to one active Pokémon. Nothing
// here is hardcoded per generation: it is derived entirely from the request, so
// a control is only shown when the server says it is legal.
type Mechanics struct {
	Available []Mechanic
}

// Has reports whether a mechanic kind is available.
func (m Mechanics) Has(kind string) bool {
	for _, a := range m.Available {
		if a.Kind == kind {
			return true
		}
	}
	return false
}

// AvailableMechanics derives the available mechanics for an active Pokémon.
func AvailableMechanics(ar *PokemonMoveRequest) Mechanics {
	if ar == nil {
		return Mechanics{}
	}
	var out []Mechanic
	switch {
	case ar.CanMegaEvo:
		out = append(out, Mechanic{Kind: "mega", Label: "Mega"})
	case ar.CanMegaEvoX:
		out = append(out, Mechanic{Kind: "mega", Label: "Mega X"})
	case ar.CanMegaEvoY:
		out = append(out, Mechanic{Kind: "mega", Label: "Mega Y"})
	}
	if ar.CanUltraBurst {
		out = append(out, Mechanic{Kind: "ultra", Label: "Ultra Burst"})
	}
	if len(ar.CanZMove) > 0 {
		out = append(out, Mechanic{Kind: "zmove", Label: "Z-Move"})
	}
	if ar.CanDynamax {
		out = append(out, Mechanic{Kind: "max", Label: "Dynamax"})
	}
	if ar.CanTerastallize != "" {
		out = append(out, Mechanic{Kind: "terastallize", Label: "Tera", Detail: ar.CanTerastallize})
	}
	return Mechanics{Available: out}
}

// Primary returns the mechanic a single "use the mechanic" keystroke should
// apply, preferring the most modern mechanic available.
func (m Mechanics) Primary() (Mechanic, bool) {
	for _, kind := range []string{"terastallize", "max", "zmove", "ultra", "mega"} {
		if a := m.byKind(kind); a != nil {
			return *a, true
		}
	}
	return Mechanic{}, false
}

func (m Mechanics) byKind(kind string) *Mechanic {
	for i := range m.Available {
		if m.Available[i].Kind == kind {
			return &m.Available[i]
		}
	}
	return nil
}

// ZMoveFor returns the Z-Move name for a move slot, or "".
func ZMoveFor(ar *PokemonMoveRequest, moveSlot int) string {
	if ar == nil || moveSlot < 1 || moveSlot > len(ar.CanZMove) {
		return ""
	}
	if z := ar.CanZMove[moveSlot-1]; z != nil {
		return z.Move
	}
	return ""
}

// MaxMoveFor returns the Dynamax move name for a move slot, or "".
func MaxMoveFor(ar *PokemonMoveRequest, moveSlot int) string {
	if ar == nil || ar.MaxMoves == nil || moveSlot < 1 || moveSlot > len(ar.MaxMoves.MaxMoves) {
		return ""
	}
	return ar.MaxMoves.MaxMoves[moveSlot-1].Move
}

// GmaxForm returns the Gigantamax forme name, or "".
func GmaxForm(ar *PokemonMoveRequest) string {
	if ar == nil || ar.MaxMoves == nil {
		return ""
	}
	return ar.MaxMoves.Gigantamax
}
