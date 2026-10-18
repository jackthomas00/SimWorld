package sim

import (
	"strings"
	"testing"
)

func TestValidateStateAcceptsValidState(t *testing.T) {
	err := validateState(validValidationConfig(), validValidationState())
	if err != nil {
		t.Fatalf("validateState returned an error for valid state: %v", err)
	}
}

func TestValidateStateRejectsInvalidDimensions(t *testing.T) {
	config := validValidationConfig()
	config.RegionWidth = 0

	err := validateState(config, validValidationState())
	if err == nil || !strings.Contains(err.Error(), "region width") {
		t.Fatalf("expected region width error, got %v", err)
	}
}

func TestValidateStateRejectsDuplicateEntityIDsAcrossStores(t *testing.T) {
	state := validValidationState()
	state.PlantIDs[0] = state.CreatureIDs[0]
	state.Plants[state.CreatureIDs[0]] = Plant{
		ID:       state.CreatureIDs[0],
		Position: Position{X: 2, Y: 2},
		Energy:   4,
		Health:   1,
	}
	delete(state.Plants, EntityID(2))

	err := validateState(validValidationConfig(), state)
	if err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("expected duplicate entity ID error, got %v", err)
	}
}

func TestValidateStateRejectsDuplicateSliceEntries(t *testing.T) {
	state := validValidationState()
	state.CreatureIDs = append(state.CreatureIDs, state.CreatureIDs[0])

	err := validateState(validValidationConfig(), state)
	if err == nil || !strings.Contains(err.Error(), "duplicate ID") {
		t.Fatalf("expected duplicate slice ID error, got %v", err)
	}
}

func TestValidateStateRejectsSliceMapMismatch(t *testing.T) {
	state := validValidationState()
	state.Creatures[EntityID(4)] = Creature{
		ID:       4,
		Kind:     KindPredator,
		Position: Position{X: 3, Y: 3},
		Energy:   7,
		Health:   1,
	}

	err := validateState(validValidationConfig(), state)
	if err == nil || !strings.Contains(err.Error(), "missing from creature ID slice") {
		t.Fatalf("expected map/slice mismatch error, got %v", err)
	}
}

func TestValidateStateRejectsOutOfBoundsPositions(t *testing.T) {
	state := validValidationState()
	creature := state.Creatures[EntityID(1)]
	creature.Position = Position{X: 4, Y: 0}
	state.Creatures[EntityID(1)] = creature

	err := validateState(validValidationConfig(), state)
	if err == nil || !strings.Contains(err.Error(), "outside world bounds") {
		t.Fatalf("expected out-of-bounds error, got %v", err)
	}
}

func TestValidateStateRejectsLivingCreatureCollision(t *testing.T) {
	state := validValidationState()
	state.CreatureIDs = append(state.CreatureIDs, 4)
	state.Creatures[EntityID(4)] = Creature{
		ID:       4,
		Kind:     KindPredator,
		Position: Position{X: 0, Y: 0},
		Energy:   6,
		Health:   1,
	}

	err := validateState(validValidationConfig(), state)
	if err == nil || !strings.Contains(err.Error(), "occupy position") {
		t.Fatalf("expected living creature collision error, got %v", err)
	}
}

func TestValidateStateRejectsInvalidNextEntity(t *testing.T) {
	state := validValidationState()
	state.NextEntity = state.CreatureIDs[0]

	err := validateState(validValidationConfig(), state)
	if err == nil || !strings.Contains(err.Error(), "collides") {
		t.Fatalf("expected NextEntity collision error, got %v", err)
	}
}

func TestValidateStateRejectsInvalidEnergyHealth(t *testing.T) {
	state := validValidationState()
	plant := state.Plants[EntityID(2)]
	plant.Health = 0
	state.Plants[EntityID(2)] = plant

	err := validateState(validValidationConfig(), state)
	if err == nil || !strings.Contains(err.Error(), "zero health with nonzero energy") {
		t.Fatalf("expected energy/health invariant error, got %v", err)
	}
}

func TestValidateStateDoesNotMutateState(t *testing.T) {
	state := validValidationState()
	before := state.clone()

	err := validateState(validValidationConfig(), state)
	if err != nil {
		t.Fatalf("validateState returned an error for valid state: %v", err)
	}

	assertValidationStateEqual(t, before, state)
}

func validValidationConfig() WorldConfig {
	return WorldConfig{
		Width:        4,
		Height:       4,
		RegionWidth:  2,
		RegionHeight: 2,
		Seed:         7,
	}
}

func validValidationState() WorldState {
	return WorldState{
		Tick:        3,
		CreatureIDs: []EntityID{1, 3},
		Creatures: map[EntityID]Creature{
			1: {
				ID:       1,
				Kind:     KindHerbivore,
				Position: Position{X: 0, Y: 0},
				Energy:   10,
				Health:   1,
			},
			3: {
				ID:       3,
				Kind:     KindPredator,
				Position: Position{X: 1, Y: 0},
				Energy:   12,
				Health:   1,
			},
		},
		PlantIDs: []EntityID{2},
		Plants: map[EntityID]Plant{
			2: {
				ID:       2,
				Position: Position{X: 2, Y: 2},
				Energy:   4,
				Health:   1,
			},
		},
		NextEntity: 4,
	}
}

func assertValidationStateEqual(t *testing.T, want, got WorldState) {
	t.Helper()

	if want.Tick != got.Tick || want.NextEntity != got.NextEntity {
		t.Fatalf("state metadata changed: want %+v got %+v", want, got)
	}
	if len(want.CreatureIDs) != len(got.CreatureIDs) || len(want.PlantIDs) != len(got.PlantIDs) {
		t.Fatalf("ID slice lengths changed: want %+v got %+v", want, got)
	}
	for i := range want.CreatureIDs {
		if want.CreatureIDs[i] != got.CreatureIDs[i] {
			t.Fatalf("creature ID slice changed: want %v got %v", want.CreatureIDs, got.CreatureIDs)
		}
	}
	for i := range want.PlantIDs {
		if want.PlantIDs[i] != got.PlantIDs[i] {
			t.Fatalf("plant ID slice changed: want %v got %v", want.PlantIDs, got.PlantIDs)
		}
	}
	if len(want.Creatures) != len(got.Creatures) || len(want.Plants) != len(got.Plants) {
		t.Fatalf("entity map lengths changed: want %+v got %+v", want, got)
	}
	for id, creature := range want.Creatures {
		if got.Creatures[id] != creature {
			t.Fatalf("creature map changed for %d: want %+v got %+v", id, creature, got.Creatures[id])
		}
	}
	for id, plant := range want.Plants {
		if got.Plants[id] != plant {
			t.Fatalf("plant map changed for %d: want %+v got %+v", id, plant, got.Plants[id])
		}
	}
}
