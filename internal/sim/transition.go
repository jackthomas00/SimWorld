package sim

import (
	"errors"
	"fmt"
	"math"
	"slices"
)

var (
	ErrTickOverflow = errors.New("tick overflow")
)

// Intents holds every intent generated for one tick.
type Intents struct {
	Move      []MoveIntent
	Feed      []FeedIntent
	Reproduce []ReproductionIntent
}

// IntentGenerator chooses intents from the committed snapshot S_t. The
// snapshot is a private copy, so a generator cannot affect committed state.
type IntentGenerator func(config WorldConfig, snapshot WorldState) (Intents, error)

// NoIntents is the default generator: every tick runs with no actions.
func NoIntents(WorldConfig, WorldState) (Intents, error) {
	return Intents{}, nil
}

// Step executes one tick. On any error the committed state is unchanged.
func (w *World) Step() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.state.Tick == math.MaxUint64 {
		return ErrTickOverflow
	}

	// Both copies are private. The generator gets its own so that nothing it
	// does can reach the committed maps, even on a failed tick.
	snapshot := w.state.clone()
	working := w.state.clone()

	if err := applyAging(&working, w.rules.Aging); err != nil {
		return fmt.Errorf("environment: %w", err)
	}
	if err := applyEnvironment(w.config, &working, w.rules.Environment); err != nil {
		return fmt.Errorf("environment: %w", err)
	}
	if err := applyMetabolism(&working, w.rules.Metabolism); err != nil {
		return fmt.Errorf("environment: %w", err)
	}

	generate := w.generate
	if generate == nil {
		generate = NoIntents
	}
	intents, err := generate(w.config, snapshot)
	if err != nil {
		return fmt.Errorf("intent generation: %w", err)
	}

	if _, err := resolveMovement(w.config, &working, intents.Move, w.rules.Movement); err != nil {
		return fmt.Errorf("movement: %w", err)
	}
	if _, err := resolveFeeding(&working, intents.Feed, w.rules.Feeding); err != nil {
		return fmt.Errorf("feeding: %w", err)
	}
	if _, err := resolveReproduction(w.config, &working, intents.Reproduce, w.rules.Reproduction); err != nil {
		return fmt.Errorf("reproduction: %w", err)
	}

	removeDead(&working)
	working.Tick = w.state.Tick + 1
	if err := validateState(w.config, working); err != nil {
		return fmt.Errorf("commit rejected: %w", err)
	}

	w.state = working
	return nil
}

// removeDead physically removes dead creatures and depleted plants from both
// the entity maps and the ID slices, preserving the order of survivors.
func removeDead(state *WorldState) {
	state.CreatureIDs = slices.DeleteFunc(state.CreatureIDs, func(id EntityID) bool {
		creature, exists := state.Creatures[id]
		if exists && creature.IsAlive() {
			return false
		}
		delete(state.Creatures, id)
		return true
	})
	state.PlantIDs = slices.DeleteFunc(state.PlantIDs, func(id EntityID) bool {
		plant, exists := state.Plants[id]
		if exists && plant.IsAlive() {
			return false
		}
		delete(state.Plants, id)
		return true
	})
}
