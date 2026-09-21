package battle

import "testing"

// TestStatRangeMatchesShowdown checks the min/max range against values the
// Showdown client computes for the same base stat and level.
func TestStatRangeMatchesShowdown(t *testing.T) {
	// Gengar, base 110 Speed, level 82.
	if min, max := StatRange(110, 82, false, false); min != 166 || max != 288 {
		t.Errorf("speed range = %d..%d, want 166..288", min, max)
	}
	// Random-battle formats use neutral natures.
	if min, max := StatRange(110, 82, false, true); min != 185 || max != 262 {
		t.Errorf("random speed range = %d..%d, want 185..262", min, max)
	}
	// HP ignores nature. Base 60 at level 82.
	if min, max := StatRange(60, 82, true, false); min != 190 || max != 267 {
		t.Errorf("HP range = %d..%d, want 190..267", min, max)
	}
	// A missing base stat must not produce nonsense.
	if min, max := StatRange(0, 50, false, false); min != 0 || max != 0 {
		t.Errorf("zero base = %d..%d, want 0..0", min, max)
	}
	// An unknown level falls back to 100 rather than dividing by zero.
	if min, max := StatRange(100, 0, false, false); min <= 0 || max <= min {
		t.Errorf("level fallback range = %d..%d, want a sane range", min, max)
	}
}
