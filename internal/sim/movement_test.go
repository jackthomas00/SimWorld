package sim

import (
	"slices"
	"testing"
)

func movementConfig() WorldConfig {
	return WorldConfig{Width: 5, Height: 5, RegionWidth: 5, RegionHeight: 5}
}

// movementState builds a state with one living creature per ID at the given
// position, each with the given energy.
func movementState(energy int64, creatures map[EntityID]Position) WorldState {
	state := WorldState{
		Creatures:  make(map[EntityID]Creature, len(creatures)),
		Plants:     make(map[EntityID]Plant),
		NextEntity: 1,
	}
	for id, pos := range creatures {
		state.CreatureIDs = append(state.CreatureIDs, id)
		state.Creatures[id] = Creature{ID: id, Kind: KindHerbivore, Position: pos, Energy: energy, Health: 1}
		if id >= state.NextEntity {
			state.NextEntity = id + 1
		}
	}
	slices.Sort(state.CreatureIDs)
	return state
}

func pos(x, y int32) Position { return Position{X: x, Y: y} }

func resolveForTest(t *testing.T, state *WorldState, rules MovementRules, intents ...MoveIntent) MovementResult {
	t.Helper()
	result, err := resolveMovement(movementConfig(), state, intents, rules)
	if err != nil {
		t.Fatalf("resolveMovement returned error: %v", err)
	}
	if err := validateState(movementConfig(), *state); err != nil {
		t.Fatalf("state invalid after movement: %v", err)
	}
	return result
}

func outcomeFor(t *testing.T, result MovementResult, actor EntityID) MoveRejection {
	t.Helper()
	for _, o := range result.Outcomes {
		if o.Intent.ActorID == actor {
			return o.Result
		}
	}
	t.Fatalf("no outcome for actor %d", actor)
	return 0
}

func assertPosition(t *testing.T, state WorldState, id EntityID, want Position) {
	t.Helper()
	if got := state.Creatures[id].Position; got != want {
		t.Fatalf("creature %d at %+v, want %+v", id, got, want)
	}
}

func TestMovementChainIntoVacatedCellSucceeds(t *testing.T) {
	state := movementState(10, map[EntityID]Position{10: pos(0, 0), 20: pos(1, 0)})

	result := resolveForTest(t, &state, MovementRules{},
		MoveIntent{ActorID: 10, To: pos(1, 0)},
		MoveIntent{ActorID: 20, To: pos(2, 0)},
	)

	if outcomeFor(t, result, 10) != MoveAccepted || outcomeFor(t, result, 20) != MoveAccepted {
		t.Fatalf("expected both moves accepted, got %+v", result.Outcomes)
	}
	assertPosition(t, state, 10, pos(1, 0))
	assertPosition(t, state, 20, pos(2, 0))
}

func TestMovementChainResultIndependentOfIntentOrder(t *testing.T) {
	// Submitting the dependent move first must not matter.
	state := movementState(10, map[EntityID]Position{1: pos(0, 0), 2: pos(1, 0), 3: pos(2, 0)})

	resolveForTest(t, &state, MovementRules{},
		MoveIntent{ActorID: 3, To: pos(3, 0)},
		MoveIntent{ActorID: 1, To: pos(1, 0)},
		MoveIntent{ActorID: 2, To: pos(2, 0)},
	)

	assertPosition(t, state, 1, pos(1, 0))
	assertPosition(t, state, 2, pos(2, 0))
	assertPosition(t, state, 3, pos(3, 0))
}

func TestMovementSwapIsRejected(t *testing.T) {
	state := movementState(10, map[EntityID]Position{10: pos(0, 0), 20: pos(1, 0)})

	result := resolveForTest(t, &state, MovementRules{EnergyCost: 1},
		MoveIntent{ActorID: 10, To: pos(1, 0)},
		MoveIntent{ActorID: 20, To: pos(0, 0)},
	)

	if outcomeFor(t, result, 10) != MoveRejectedCycle || outcomeFor(t, result, 20) != MoveRejectedCycle {
		t.Fatalf("expected both moves rejected as cycle, got %+v", result.Outcomes)
	}
	assertPosition(t, state, 10, pos(0, 0))
	assertPosition(t, state, 20, pos(1, 0))
	if state.Creatures[10].Energy != 10 || state.Creatures[20].Energy != 10 {
		t.Fatalf("rejected moves must not charge energy")
	}
}

func TestMovementLongerCycleAndFeederAreRejected(t *testing.T) {
	// 1 -> 2 -> 3 -> 1 is a cycle; 4 moves into 1's cell and is blocked.
	state := movementState(10, map[EntityID]Position{
		1: pos(0, 0), 2: pos(1, 0), 3: pos(1, 1), 4: pos(0, 1),
	})

	result := resolveForTest(t, &state, MovementRules{},
		MoveIntent{ActorID: 1, To: pos(1, 0)},
		MoveIntent{ActorID: 2, To: pos(1, 1)},
		MoveIntent{ActorID: 3, To: pos(0, 0)},
		MoveIntent{ActorID: 4, To: pos(1, 1)},
	)

	// 2 wins (1,1) over 4, so 4 lost the conflict; 1, 2, 3 form a cycle.
	for _, id := range []EntityID{1, 2, 3} {
		if got := outcomeFor(t, result, id); got != MoveRejectedCycle {
			t.Fatalf("creature %d: got %v, want cycle", id, got)
		}
	}
	if got := outcomeFor(t, result, 4); got != MoveRejectedLostConflict {
		t.Fatalf("creature 4: got %v, want lost conflict", got)
	}
}

func TestMovementFeederIntoSwapLosesConflict(t *testing.T) {
	// 1 and 2 swap; 3 wants 2's cell. Every cell in a cycle is claimed by a
	// cycle member, so an outside move into a cycle always loses a conflict
	// or breaks the cycle.
	state := movementState(10, map[EntityID]Position{1: pos(0, 0), 2: pos(1, 0), 3: pos(0, 1)})

	result := resolveForTest(t, &state, MovementRules{},
		MoveIntent{ActorID: 3, To: pos(1, 0)}, // loses to 1
		MoveIntent{ActorID: 1, To: pos(1, 0)},
		MoveIntent{ActorID: 2, To: pos(0, 0)},
	)
	if got := outcomeFor(t, result, 3); got != MoveRejectedLostConflict {
		t.Fatalf("creature 3: got %v", got)
	}
	if outcomeFor(t, result, 1) != MoveRejectedCycle || outcomeFor(t, result, 2) != MoveRejectedCycle {
		t.Fatalf("expected swap rejected, got %+v", result.Outcomes)
	}
}

func TestMovementChainBehindNonVacatingOccupantIsBlocked(t *testing.T) {
	state := movementState(10, map[EntityID]Position{5: pos(0, 0), 6: pos(1, 0), 1: pos(0, 1)})
	result := resolveForTest(t, &state, MovementRules{},
		MoveIntent{ActorID: 1, To: pos(0, 0)}, // depends on 5 vacating
		MoveIntent{ActorID: 5, To: pos(1, 0)},
		MoveIntent{ActorID: 6, To: pos(0, 0)}, // loses (0,0) to 1
	)
	// 1 wins (0,0); 5 wins (1,0); 6 lost, so 5 is blocked by non-vacating 6,
	// and 1 is blocked by non-vacating 5.
	if outcomeFor(t, result, 1) != MoveRejectedBlocked || outcomeFor(t, result, 5) != MoveRejectedBlocked {
		t.Fatalf("expected blocked chain, got %+v", result.Outcomes)
	}
	assertPosition(t, state, 1, pos(0, 1))
	assertPosition(t, state, 5, pos(0, 0))
	assertPosition(t, state, 6, pos(1, 0))
}

func TestMovementLowestIDWinsDestination(t *testing.T) {
	state := movementState(10, map[EntityID]Position{7: pos(0, 0), 3: pos(2, 0)})

	result := resolveForTest(t, &state, MovementRules{},
		MoveIntent{ActorID: 7, To: pos(1, 0)},
		MoveIntent{ActorID: 3, To: pos(1, 0)},
	)

	if outcomeFor(t, result, 3) != MoveAccepted || outcomeFor(t, result, 7) != MoveRejectedLostConflict {
		t.Fatalf("expected 3 to win, got %+v", result.Outcomes)
	}
	assertPosition(t, state, 3, pos(1, 0))
	assertPosition(t, state, 7, pos(0, 0))
}

func TestMovementDestinationLoserBlocksDependents(t *testing.T) {
	// 20 loses its destination to 5, so it stays and 10 cannot enter its cell.
	state := movementState(10, map[EntityID]Position{5: pos(3, 1), 10: pos(0, 0), 20: pos(1, 0)})

	result := resolveForTest(t, &state, MovementRules{},
		MoveIntent{ActorID: 10, To: pos(1, 0)},
		MoveIntent{ActorID: 20, To: pos(2, 0)},
		MoveIntent{ActorID: 5, To: pos(2, 0)},
	)

	if outcomeFor(t, result, 20) != MoveRejectedLostConflict || outcomeFor(t, result, 10) != MoveRejectedBlocked {
		t.Fatalf("unexpected outcomes %+v", result.Outcomes)
	}
	assertPosition(t, state, 10, pos(0, 0))
	assertPosition(t, state, 20, pos(1, 0))
	assertPosition(t, state, 5, pos(2, 0))
}

func TestMovementRejectsInvalidIntents(t *testing.T) {
	state := movementState(10, map[EntityID]Position{1: pos(0, 0), 2: pos(4, 4), 3: pos(2, 2), 4: pos(3, 3)})
	dead := state.Creatures[4]
	dead.Energy, dead.Health = 0, 0
	state.Creatures[4] = dead

	result := resolveForTest(t, &state, MovementRules{},
		MoveIntent{ActorID: 1, To: pos(1, 0)},
		MoveIntent{ActorID: 1, To: pos(0, 1)},
		MoveIntent{ActorID: 2, To: pos(5, 4)},
		MoveIntent{ActorID: 3, To: pos(2, 2)},
		MoveIntent{ActorID: 4, To: pos(3, 2)},
		MoveIntent{ActorID: 99, To: pos(1, 1)},
	)

	want := map[EntityID]MoveRejection{
		1:  MoveRejectedDuplicate,
		2:  MoveRejectedOutOfBounds,
		3:  MoveRejectedNoop,
		4:  MoveRejectedDeadActor,
		99: MoveRejectedUnknownActor,
	}
	for _, o := range result.Outcomes {
		if o.Result != want[o.Intent.ActorID] {
			t.Fatalf("actor %d: got %v, want %v", o.Intent.ActorID, o.Result, want[o.Intent.ActorID])
		}
	}
	if len(result.Outcomes) != 6 {
		t.Fatalf("expected one outcome per intent, got %d", len(result.Outcomes))
	}
	assertPosition(t, state, 1, pos(0, 0))
}

func TestMovementIntoDeadCreatureCellSucceeds(t *testing.T) {
	state := movementState(10, map[EntityID]Position{1: pos(0, 0), 2: pos(1, 0)})
	dead := state.Creatures[2]
	dead.Energy, dead.Health = 0, 0
	state.Creatures[2] = dead

	result := resolveForTest(t, &state, MovementRules{}, MoveIntent{ActorID: 1, To: pos(1, 0)})

	if outcomeFor(t, result, 1) != MoveAccepted {
		t.Fatalf("dead creatures must not block movement, got %+v", result.Outcomes)
	}
}

func TestMovementChargesEnergyAndReleasesDeadCells(t *testing.T) {
	state := movementState(3, map[EntityID]Position{1: pos(0, 0), 2: pos(1, 0)})
	survivor := state.Creatures[2]
	survivor.Energy = 10
	state.Creatures[2] = survivor

	result := resolveForTest(t, &state, MovementRules{EnergyCost: 3},
		MoveIntent{ActorID: 1, To: pos(1, 0)},
		MoveIntent{ActorID: 2, To: pos(2, 0)},
	)

	// 1 dies from the move cost but its move still resolves.
	if outcomeFor(t, result, 1) != MoveAccepted {
		t.Fatalf("expected move to resolve, got %+v", result.Outcomes)
	}
	c1, c2 := state.Creatures[1], state.Creatures[2]
	if c1.IsAlive() || c1.Position != pos(1, 0) || c1.Energy != 0 {
		t.Fatalf("creature 1 should be dead at (1,0), got %+v", c1)
	}
	if !c2.IsAlive() || c2.Energy != 7 {
		t.Fatalf("creature 2 should have 7 energy, got %+v", c2)
	}
	if _, occupied := result.Occupancy[pos(1, 0)]; occupied {
		t.Fatalf("cell of creature that died during movement must be released")
	}
	if result.Occupancy[pos(2, 0)] != 2 {
		t.Fatalf("expected creature 2 to occupy (2,0), got %+v", result.Occupancy)
	}
}

func TestMovementRejectsNegativeCost(t *testing.T) {
	state := movementState(10, map[EntityID]Position{1: pos(0, 0)})
	before := state.clone()

	_, err := resolveMovement(movementConfig(), &state, []MoveIntent{{ActorID: 1, To: pos(1, 0)}}, MovementRules{EnergyCost: -1})
	if err == nil {
		t.Fatalf("expected error for negative energy cost")
	}
	assertValidationStateEqual(t, before, state)
}

func TestMovementIsDeterministicAcrossIntentPermutations(t *testing.T) {
	creatures := map[EntityID]Position{
		1: pos(0, 0), 2: pos(1, 0), 3: pos(2, 0), 4: pos(0, 1), 5: pos(1, 1), 6: pos(3, 3),
	}
	intents := []MoveIntent{
		{ActorID: 1, To: pos(1, 0)},
		{ActorID: 2, To: pos(2, 0)},
		{ActorID: 3, To: pos(3, 0)},
		{ActorID: 4, To: pos(1, 1)},
		{ActorID: 5, To: pos(0, 1)},
		{ActorID: 6, To: pos(3, 0)},
	}

	reference := movementState(10, creatures)
	resolveForTest(t, &reference, MovementRules{EnergyCost: 1}, intents...)

	for i := range intents {
		rotated := append(slices.Clone(intents[i:]), intents[:i]...)
		slices.Reverse(rotated)
		state := movementState(10, creatures)
		resolveForTest(t, &state, MovementRules{EnergyCost: 1}, rotated...)
		assertValidationStateEqual(t, reference, state)
	}
}
