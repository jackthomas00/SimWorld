package sim

import (
	"errors"
	"fmt"
)

var ErrCustomGenerator = errors.New("world uses a custom intent generator and cannot be checkpointed")

// Checkpoint holds everything that decides a world's future ticks, so a world
// restored from it continues exactly as the original would have. Randomness is
// counter-based, so there is no generator state to save beyond the seed in
// Config.
type Checkpoint struct {
	ID     string
	Config WorldConfig
	Rules  Rules
	// Behavior is the rules passed to WithBehavior, or nil for a world that
	// runs without intents.
	Behavior *BehaviorRules
	State    WorldState
}

// Checkpoint captures the committed world. It fails for a world built with
// WithIntentGenerator, whose generator cannot be saved.
func (w *World) Checkpoint() (Checkpoint, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.custom {
		return Checkpoint{}, ErrCustomGenerator
	}
	cp := Checkpoint{ID: w.id, Config: w.config, Rules: w.rules, State: w.state.clone()}
	if w.behavior != nil {
		behavior := *w.behavior
		cp.Behavior = &behavior
	}
	return cp, nil
}

// Restore rebuilds a world from a checkpoint. The state must pass the same
// validation as a commit. The world keeps its own copy, so later changes to
// cp do not reach it.
func Restore(cp Checkpoint) (*World, error) {
	if err := validateState(cp.Config, cp.State); err != nil {
		return nil, fmt.Errorf("invalid checkpoint state: %w", err)
	}

	opts := []WorldOption{WithRules(cp.Rules)}
	if cp.Behavior != nil {
		opts = append(opts, WithBehavior(*cp.Behavior))
	}
	w := NewWorld(cp.ID, cp.Config, opts...)
	w.state = cp.State.clone()
	return w, nil
}
