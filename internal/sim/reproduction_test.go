package sim

import (
	"math"
	"reflect"
	"testing"
)

var defaultReproductionRules = sameBirthRules(BirthRules{EnergyCost: 4, OffspringEnergy: 3, OffspringHealth: 1})

func sameBirthRules(b BirthRules) ReproductionRules {
	return ReproductionRules{Herbivore: b, Predator: b}
}

func reproduceForTest(t *testing.T, state *WorldState, rules ReproductionRules, intents ...ReproductionIntent) ReproductionResult {
	t.Helper()
	result, err := resolveReproduction(movementConfig(), state, intents, rules)
	if err != nil {
		t.Fatalf("resolveReproduction returned error: %v", err)
	}
	if err := validateState(movementConfig(), *state); err != nil {
		t.Fatalf("state invalid after reproduction: %v", err)
	}
	return result
}

func reproductionOutcomeFor(t *testing.T, result ReproductionResult, parent EntityID) ReproductionOutcome {
	t.Helper()
	for _, o := range result.Outcomes {
		if o.Intent.ParentID == parent {
			return o
		}
	}
	t.Fatalf("no outcome for parent %d", parent)
	return ReproductionOutcome{}
}

func TestReproductionCreatesOffspring(t *testing.T) {
	state := feedingState([]Creature{predator(1, pos(1, 1), 10)}, nil)

	result := reproduceForTest(t, &state, defaultReproductionRules, ReproductionIntent{ParentID: 1, To: pos(2, 2)})

	o := reproductionOutcomeFor(t, result, 1)
	if o.Result != ReproductionAccepted || o.OffspringID != 2 {
		t.Fatalf("outcome = %+v, want accepted with ID 2", o)
	}
	want := Creature{ID: 2, Kind: KindPredator, Position: pos(2, 2), Energy: 3, Health: 1}
	if got := state.Creatures[2]; got != want {
		t.Fatalf("offspring = %+v, want %+v", got, want)
	}
	if got := state.Creatures[1].Energy; got != 6 {
		t.Fatalf("parent energy = %d, want 6", got)
	}
	if state.NextEntity != 3 {
		t.Fatalf("NextEntity = %d, want 3", state.NextEntity)
	}
}

func TestReproductionIDsFollowParentOrder(t *testing.T) {
	state := feedingState(
		[]Creature{herbivore(3, pos(0, 0), 10), herbivore(1, pos(4, 4), 10), herbivore(2, pos(2, 2), 10)},
		nil,
	)

	result := reproduceForTest(t, &state, defaultReproductionRules,
		ReproductionIntent{ParentID: 3, To: pos(1, 0)},
		ReproductionIntent{ParentID: 2, To: pos(2, 3)},
		ReproductionIntent{ParentID: 1, To: pos(3, 4)},
	)

	for parent, want := range map[EntityID]EntityID{1: 4, 2: 5, 3: 6} {
		if got := reproductionOutcomeFor(t, result, parent).OffspringID; got != want {
			t.Fatalf("parent %d offspring ID = %d, want %d", parent, got, want)
		}
	}
}

func TestReproductionConflictLowestParentWinsAndLoserConsumesNothing(t *testing.T) {
	state := feedingState(
		[]Creature{herbivore(1, pos(1, 1), 10), herbivore(2, pos(3, 1), 10), herbivore(3, pos(4, 4), 10)},
		nil,
	)

	result := reproduceForTest(t, &state, defaultReproductionRules,
		ReproductionIntent{ParentID: 2, To: pos(2, 1)},
		ReproductionIntent{ParentID: 1, To: pos(2, 1)},
		ReproductionIntent{ParentID: 3, To: pos(4, 3)},
	)

	if got := reproductionOutcomeFor(t, result, 2).Result; got != ReproductionRejectedLostConflict {
		t.Fatalf("parent 2 = %v, want lost conflict", got)
	}
	if got := state.Creatures[2].Energy; got != 10 {
		t.Fatalf("losing parent energy = %d, want unchanged 10", got)
	}
	// The rejected birth must not consume an ID: parent 3 gets the next one.
	if got := reproductionOutcomeFor(t, result, 1).OffspringID; got != 4 {
		t.Fatalf("parent 1 offspring = %d, want 4", got)
	}
	if got := reproductionOutcomeFor(t, result, 3).OffspringID; got != 5 {
		t.Fatalf("parent 3 offspring = %d, want 5", got)
	}
}

func TestReproductionCellOfDeadCreatureIsFree(t *testing.T) {
	state := feedingState(
		[]Creature{herbivore(1, pos(1, 1), 10), {ID: 2, Kind: KindHerbivore, Position: pos(2, 1)}},
		nil,
	)

	result := reproduceForTest(t, &state, defaultReproductionRules, ReproductionIntent{ParentID: 1, To: pos(2, 1)})

	if got := reproductionOutcomeFor(t, result, 1).Result; got != ReproductionAccepted {
		t.Fatalf("result = %v, want accepted", got)
	}
}

func TestReproductionOntoPlantAllowed(t *testing.T) {
	state := feedingState([]Creature{herbivore(1, pos(1, 1), 10)}, []Plant{plant(2, pos(2, 1), 5)})

	result := reproduceForTest(t, &state, defaultReproductionRules, ReproductionIntent{ParentID: 1, To: pos(2, 1)})

	if o := reproductionOutcomeFor(t, result, 1); o.Result != ReproductionAccepted || o.OffspringID != 3 {
		t.Fatalf("outcome = %+v, want accepted with ID 3", o)
	}
}

func TestReproductionRejections(t *testing.T) {
	tests := []struct {
		name   string
		intent ReproductionIntent
		want   ReproductionRejection
	}{
		{"unknown parent", ReproductionIntent{ParentID: 99, To: pos(1, 0)}, ReproductionRejectedUnknownParent},
		{"dead parent", ReproductionIntent{ParentID: 2, To: pos(3, 2)}, ReproductionRejectedDeadParent},
		{"cost equals energy", ReproductionIntent{ParentID: 3, To: pos(4, 3)}, ReproductionRejectedInsufficientEnergy},
		{"out of bounds", ReproductionIntent{ParentID: 1, To: pos(-1, 0)}, ReproductionRejectedOutOfBounds},
		{"not adjacent", ReproductionIntent{ParentID: 1, To: pos(2, 0)}, ReproductionRejectedOutOfReach},
		{"parent's own cell", ReproductionIntent{ParentID: 1, To: pos(0, 0)}, ReproductionRejectedOccupied},
		{"occupied by living creature", ReproductionIntent{ParentID: 1, To: pos(1, 1)}, ReproductionRejectedOccupied},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := feedingState(
				[]Creature{
					herbivore(1, pos(0, 0), 10),
					{ID: 2, Kind: KindHerbivore, Position: pos(2, 2)},
					herbivore(3, pos(4, 4), 4),
					herbivore(4, pos(1, 1), 10),
				},
				nil,
			)
			before := state.clone()

			result := reproduceForTest(t, &state, defaultReproductionRules, tt.intent)

			if got := result.Outcomes[0].Result; got != tt.want {
				t.Fatalf("result = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(state, before) {
				t.Fatalf("rejected intent mutated state")
			}
		})
	}
}

func TestReproductionDuplicateIntentsAllRejected(t *testing.T) {
	state := feedingState([]Creature{herbivore(1, pos(1, 1), 10)}, nil)

	result := reproduceForTest(t, &state, defaultReproductionRules,
		ReproductionIntent{ParentID: 1, To: pos(1, 2)},
		ReproductionIntent{ParentID: 1, To: pos(2, 1)},
	)

	for _, o := range result.Outcomes {
		if o.Result != ReproductionRejectedDuplicate {
			t.Fatalf("outcome %+v, want duplicate", o)
		}
	}
}

func TestReproductionAllocationFailureLeavesStateUntouched(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*WorldState)
	}{
		{"overflow", func(s *WorldState) { s.NextEntity = math.MaxUint64 - 2 }},
		{"NextEntity at maximum", func(s *WorldState) { s.NextEntity = math.MaxUint64 }},
		{"zero NextEntity", func(s *WorldState) { s.NextEntity = 0 }},
		{"NextEntity in use", func(s *WorldState) { s.NextEntity = 2 }},
		{"later ID in use", func(s *WorldState) {
			s.Plants[11] = plant(11, pos(4, 4), 5)
			s.PlantIDs = append(s.PlantIDs, 11)
			s.NextEntity = 10
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := feedingState(
				[]Creature{herbivore(1, pos(0, 0), 10), herbivore(2, pos(4, 0), 10)},
				nil,
			)
			tt.mutate(&state)
			before := state.clone()

			_, err := resolveReproduction(movementConfig(), &state, []ReproductionIntent{
				{ParentID: 1, To: pos(0, 1)},
				{ParentID: 2, To: pos(4, 1)},
			}, defaultReproductionRules)

			if err == nil {
				t.Fatalf("expected allocation error")
			}
			if !reflect.DeepEqual(state, before) {
				t.Fatalf("failed allocation mutated state")
			}
		})
	}
}

func TestReproductionAllocatesUpToLastSafeID(t *testing.T) {
	state := feedingState([]Creature{herbivore(1, pos(0, 0), 10)}, nil)
	// MaxUint64 is never a valid NextEntity, so the last allocatable ID is
	// MaxUint64-2, leaving NextEntity at MaxUint64-1.
	state.NextEntity = math.MaxUint64 - 2

	result := reproduceForTest(t, &state, defaultReproductionRules, ReproductionIntent{ParentID: 1, To: pos(0, 1)})

	if got := reproductionOutcomeFor(t, result, 1).OffspringID; got != math.MaxUint64-2 {
		t.Fatalf("offspring ID = %d, want MaxUint64-2", got)
	}
	if state.NextEntity != math.MaxUint64-1 {
		t.Fatalf("NextEntity = %d, want MaxUint64-1", state.NextEntity)
	}
}

func TestReproductionNoAcceptedBirthsSkipsAllocationCheck(t *testing.T) {
	state := feedingState([]Creature{herbivore(1, pos(0, 0), 2)}, nil)
	state.NextEntity = math.MaxUint64

	_, err := resolveReproduction(movementConfig(), &state, []ReproductionIntent{{ParentID: 1, To: pos(0, 1)}}, defaultReproductionRules)
	if err != nil {
		t.Fatalf("unexpected error with no accepted births: %v", err)
	}
}

func TestReproductionRejectsInvalidRules(t *testing.T) {
	valid := BirthRules{EnergyCost: 1, OffspringEnergy: 1, OffspringHealth: 1}
	for _, bad := range []BirthRules{
		{EnergyCost: -1, OffspringEnergy: 1, OffspringHealth: 1},
		{EnergyCost: 1, OffspringEnergy: 0, OffspringHealth: 1},
		{EnergyCost: 1, OffspringEnergy: 1, OffspringHealth: 0},
	} {
		// Each kind's rules are validated independently.
		for _, rules := range []ReproductionRules{{Herbivore: bad, Predator: valid}, {Herbivore: valid, Predator: bad}} {
			state := feedingState(nil, nil)
			if _, err := resolveReproduction(movementConfig(), &state, nil, rules); err == nil {
				t.Fatalf("expected error for rules %+v", rules)
			}
		}
	}
}

func TestReproductionUsesEachKindsRules(t *testing.T) {
	state := feedingState([]Creature{herbivore(1, pos(0, 0), 30), predator(2, pos(4, 4), 30)}, nil)
	rules := ReproductionRules{
		Herbivore: BirthRules{EnergyCost: 5, OffspringEnergy: 3, OffspringHealth: 1},
		Predator:  BirthRules{EnergyCost: 20, OffspringEnergy: 12, OffspringHealth: 2},
	}

	reproduceForTest(t, &state, rules,
		ReproductionIntent{ParentID: 1, To: pos(1, 0)},
		ReproductionIntent{ParentID: 2, To: pos(3, 4)},
	)

	if got := state.Creatures[1].Energy; got != 25 {
		t.Fatalf("herbivore parent energy = %d, want 25", got)
	}
	if got := state.Creatures[2].Energy; got != 10 {
		t.Fatalf("predator parent energy = %d, want 10", got)
	}
	if got := state.Creatures[3]; got.Kind != KindHerbivore || got.Energy != 3 || got.Health != 1 {
		t.Fatalf("herbivore offspring = %+v", got)
	}
	if got := state.Creatures[4]; got.Kind != KindPredator || got.Energy != 12 || got.Health != 2 {
		t.Fatalf("predator offspring = %+v", got)
	}
}

func TestReproductionRejectsKindWithoutBirthRules(t *testing.T) {
	state := feedingState([]Creature{{ID: 1, Kind: KindPlant, Position: pos(0, 0), Energy: 30, Health: 1}}, nil)

	result := reproduceForTest(t, &state, defaultReproductionRules, ReproductionIntent{ParentID: 1, To: pos(1, 0)})

	if got := result.Outcomes[0].Result; got != ReproductionRejectedUnsupportedKind {
		t.Fatalf("result = %v, want unsupported kind", got)
	}
}
