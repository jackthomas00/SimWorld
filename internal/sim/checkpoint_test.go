package sim

import (
	"errors"
	"strings"
	"testing"
)

func TestRestoreContinuesIdentically(t *testing.T) {
	reference := hashedWorld(t, 24, 42)
	var want []StateHash
	for tick := 1; tick <= 200; tick++ {
		if err := reference.Step(); err != nil {
			t.Fatalf("reference tick %d: %v", tick, err)
		}
		want = append(want, reference.StateHash())
	}

	original := hashedWorld(t, 24, 42)
	for tick := 1; tick <= 100; tick++ {
		if err := original.Step(); err != nil {
			t.Fatalf("original tick %d: %v", tick, err)
		}
	}
	cp, err := original.Checkpoint()
	if err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	restored, err := Restore(cp)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restored.StateHash() != want[99] {
		t.Fatalf("restored hash differs from the original at tick 100")
	}
	for tick := 101; tick <= 200; tick++ {
		if err := restored.Step(); err != nil {
			t.Fatalf("restored tick %d: %v", tick, err)
		}
		if got := restored.StateHash(); got != want[tick-1] {
			t.Fatalf("restored world diverged at tick %d", tick)
		}
	}
}

func TestCheckpointRefusesCustomGenerator(t *testing.T) {
	w := NewWorld("test", stepConfig(), WithIntentGenerator(NoIntents))
	if _, err := w.Checkpoint(); !errors.Is(err, ErrCustomGenerator) {
		t.Fatalf("Checkpoint error = %v, want ErrCustomGenerator", err)
	}

	// A later WithBehavior replaces the custom generator.
	w = NewWorld("test", stepConfig(), WithIntentGenerator(NoIntents), WithBehavior(DefaultBehaviorRules()))
	if _, err := w.Checkpoint(); err != nil {
		t.Fatalf("Checkpoint after WithBehavior: %v", err)
	}
}

func TestCheckpointWithoutBehavior(t *testing.T) {
	cp, err := NewWorld("test", stepConfig()).Checkpoint()
	if err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	if cp.Behavior != nil {
		t.Fatalf("world without behavior checkpointed behavior %+v", *cp.Behavior)
	}
}

func TestCheckpointAndRestoreAreIsolated(t *testing.T) {
	w := hashedWorld(t, 24, 3)
	before := w.StateHash()

	cp, err := w.Checkpoint()
	if err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	cp.State.Tick++
	cp.Behavior.HerbivoreBite++
	if w.StateHash() != before {
		t.Fatalf("changing the checkpoint changed the world")
	}

	cp, _ = w.Checkpoint()
	restored, err := Restore(cp)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	cp.State.Tick++
	if restored.StateHash() != before {
		t.Fatalf("changing the checkpoint changed the restored world")
	}
}

func TestRestoreRejectsInvalidState(t *testing.T) {
	cp, err := hashedWorld(t, 24, 3).Checkpoint()
	if err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	id := cp.State.CreatureIDs[0]
	c := cp.State.Creatures[id]
	c.Position.X = cp.Config.Width
	cp.State.Creatures[id] = c

	if _, err := Restore(cp); err == nil || !strings.Contains(err.Error(), "invalid checkpoint state") {
		t.Fatalf("Restore error = %v, want invalid state", err)
	}
}

func TestWithBehaviorInvalidRulesFailStep(t *testing.T) {
	rules := DefaultBehaviorRules()
	rules.SenseRadius = 0
	w := NewWorld("test", stepConfig(), WithBehavior(rules))

	if err := w.Step(); err == nil || !strings.Contains(err.Error(), "sense radius") {
		t.Fatalf("Step error = %v, want sense radius error", err)
	}
	if w.Tick() != 0 {
		t.Fatalf("failed step advanced the tick to %d", w.Tick())
	}
}
