package sim

import "testing"

func TestDecisionRandomMatchesTickSemanticsVector(t *testing.T) {
	rng := NewDecisionRandom(
		0x0123456789abcdef,
		Tick(42),
		EntityID(1001),
		DecisionKindMovementIntent,
		0,
	)

	if rng.State() != 0x94800feadc2e8b4b {
		t.Fatalf("state before first output = %#016x, want 0x94800feadc2e8b4b", rng.State())
	}
	if rng.Increment() != 0xa08f22dde802ee77 {
		t.Fatalf("increment = %#016x, want 0xa08f22dde802ee77", rng.Increment())
	}

	want := []uint32{
		0x5656e401,
		0x13b469ef,
		0x01104b72,
		0xacc421f5,
		0x3e603862,
	}
	for i, expected := range want {
		if got := rng.Uint32(); got != expected {
			t.Fatalf("output %d = %#08x, want %#08x", i, got, expected)
		}
	}
}

func TestDecisionRandomStreamsAreIndependent(t *testing.T) {
	first := NewDecisionRandom(7, Tick(3), EntityID(9), DecisionKindFeedingIntent, 0)
	second := NewDecisionRandom(7, Tick(3), EntityID(9), DecisionKindFeedingIntent, 1)

	if first.State() == second.State() && first.Increment() == second.Increment() {
		t.Fatal("different decision ordinals produced the same stream")
	}
}
