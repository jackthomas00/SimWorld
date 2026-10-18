package sim

import (
	"reflect"
	"testing"
)

// feedingState builds a state from explicit creatures and plants.
func feedingState(creatures []Creature, plants []Plant) WorldState {
	state := WorldState{
		Creatures:  make(map[EntityID]Creature),
		Plants:     make(map[EntityID]Plant),
		NextEntity: 1,
	}
	for _, c := range creatures {
		state.CreatureIDs = append(state.CreatureIDs, c.ID)
		state.Creatures[c.ID] = c
		state.NextEntity = max(state.NextEntity, c.ID+1)
	}
	for _, p := range plants {
		state.PlantIDs = append(state.PlantIDs, p.ID)
		state.Plants[p.ID] = p
		state.NextEntity = max(state.NextEntity, p.ID+1)
	}
	return state
}

func herbivore(id EntityID, at Position, energy int64) Creature {
	return Creature{ID: id, Kind: KindHerbivore, Position: at, Energy: energy, Health: 1}
}

func predator(id EntityID, at Position, energy int64) Creature {
	return Creature{ID: id, Kind: KindPredator, Position: at, Energy: energy, Health: 1}
}

func plant(id EntityID, at Position, energy int64) Plant {
	return Plant{ID: id, Position: at, Energy: energy, Health: 1}
}

func feedForTest(t *testing.T, state *WorldState, rules FeedingRules, intents ...FeedIntent) FeedingResult {
	t.Helper()
	result, err := resolveFeeding(state, intents, rules)
	if err != nil {
		t.Fatalf("resolveFeeding returned error: %v", err)
	}
	if err := validateState(movementConfig(), *state); err != nil {
		t.Fatalf("state invalid after feeding: %v", err)
	}
	return result
}

func feedOutcomeFor(t *testing.T, result FeedingResult, actor EntityID) FeedOutcome {
	t.Helper()
	for _, o := range result.Outcomes {
		if o.Intent.ActorID == actor {
			return o
		}
	}
	t.Fatalf("no outcome for actor %d", actor)
	return FeedOutcome{}
}

func TestFeedingPlantAppliesCostAndGain(t *testing.T) {
	state := feedingState(
		[]Creature{herbivore(1, pos(1, 1), 10)},
		[]Plant{plant(10, pos(1, 1), 8)},
	)

	result := feedForTest(t, &state, FeedingRules{EnergyCost: 2}, FeedIntent{ActorID: 1, TargetID: 10, Amount: 5})

	if o := feedOutcomeFor(t, result, 1); o.Result != FeedAccepted || o.Gained != 5 {
		t.Fatalf("outcome = %v gained %d, want accepted gained 5", o.Result, o.Gained)
	}
	if got := state.Creatures[1].Energy; got != 13 {
		t.Fatalf("actor energy = %d, want 13", got)
	}
	if got := state.Plants[10].Energy; got != 3 {
		t.Fatalf("plant energy = %d, want 3", got)
	}
}

func TestFeedingPlantIsAllOrNothing(t *testing.T) {
	state := feedingState(
		[]Creature{herbivore(1, pos(1, 1), 10), herbivore(2, pos(2, 1), 10)},
		[]Plant{plant(10, pos(1, 1), 6)},
	)
	before := state.clone()

	// Actor 1 takes 4 of 6; actor 2 asks for 3 of the remaining 2 and is
	// rejected without partial effects.
	result := feedForTest(t, &state, FeedingRules{},
		FeedIntent{ActorID: 2, TargetID: 10, Amount: 3},
		FeedIntent{ActorID: 1, TargetID: 10, Amount: 4},
	)

	if got := feedOutcomeFor(t, result, 1).Result; got != FeedAccepted {
		t.Fatalf("actor 1 = %v, want accepted", got)
	}
	if got := feedOutcomeFor(t, result, 2).Result; got != FeedRejectedInsufficientFood {
		t.Fatalf("actor 2 = %v, want insufficient food", got)
	}
	if !reflect.DeepEqual(state.Creatures[2], before.Creatures[2]) {
		t.Fatalf("rejected actor changed: %+v", state.Creatures[2])
	}
	if got := state.Plants[10].Energy; got != 2 {
		t.Fatalf("plant energy = %d, want 2", got)
	}
}

func TestFeedingDepletedPlantCannotBeEatenAgain(t *testing.T) {
	state := feedingState(
		[]Creature{herbivore(1, pos(1, 1), 10), herbivore(2, pos(2, 1), 10)},
		[]Plant{plant(10, pos(1, 1), 4)},
	)

	result := feedForTest(t, &state, FeedingRules{},
		FeedIntent{ActorID: 1, TargetID: 10, Amount: 4},
		FeedIntent{ActorID: 2, TargetID: 10, Amount: 1},
	)

	if got := feedOutcomeFor(t, result, 2).Result; got != FeedRejectedTargetDepleted {
		t.Fatalf("actor 2 = %v, want target depleted", got)
	}
	if state.Plants[10].IsAlive() {
		t.Fatalf("plant still alive after full consumption")
	}
}

func TestFeedingPreyConsumedOnlyOnce(t *testing.T) {
	state := feedingState(
		[]Creature{
			herbivore(5, pos(2, 2), 7),
			predator(1, pos(1, 2), 10),
			predator(2, pos(3, 2), 10),
		},
		nil,
	)

	result := feedForTest(t, &state, FeedingRules{EnergyCost: 1},
		FeedIntent{ActorID: 2, TargetID: 5},
		FeedIntent{ActorID: 1, TargetID: 5},
	)

	if o := feedOutcomeFor(t, result, 1); o.Result != FeedAccepted || o.Gained != 7 {
		t.Fatalf("actor 1 = %v gained %d, want accepted gained 7", o.Result, o.Gained)
	}
	if got := feedOutcomeFor(t, result, 2).Result; got != FeedRejectedTargetDepleted {
		t.Fatalf("actor 2 = %v, want target depleted", got)
	}
	if got := state.Creatures[1].Energy; got != 16 {
		t.Fatalf("winner energy = %d, want 16", got)
	}
	if got := state.Creatures[2].Energy; got != 10 {
		t.Fatalf("loser energy = %d, want unchanged 10", got)
	}
	if state.Creatures[5].IsAlive() {
		t.Fatalf("prey still alive")
	}
}

func TestFeedingConsumedPreyCannotActLater(t *testing.T) {
	state := feedingState(
		[]Creature{predator(1, pos(1, 1), 10), herbivore(2, pos(2, 1), 5)},
		[]Plant{plant(10, pos(2, 1), 5)},
	)

	result := feedForTest(t, &state, FeedingRules{},
		FeedIntent{ActorID: 1, TargetID: 2},
		FeedIntent{ActorID: 2, TargetID: 10, Amount: 3},
	)

	if got := feedOutcomeFor(t, result, 2).Result; got != FeedRejectedDeadActor {
		t.Fatalf("eaten herbivore = %v, want dead actor", got)
	}
	if got := state.Plants[10].Energy; got != 5 {
		t.Fatalf("plant energy = %d, want untouched 5", got)
	}
}

func TestFeedingPreyThatAteFirstYieldsItsMeal(t *testing.T) {
	// Lower ID acts first: the herbivore eats, then is eaten with its gain.
	state := feedingState(
		[]Creature{herbivore(1, pos(2, 1), 5), predator(2, pos(1, 1), 10)},
		[]Plant{plant(10, pos(2, 1), 5)},
	)

	result := feedForTest(t, &state, FeedingRules{},
		FeedIntent{ActorID: 1, TargetID: 10, Amount: 3},
		FeedIntent{ActorID: 2, TargetID: 1},
	)

	if o := feedOutcomeFor(t, result, 2); o.Result != FeedAccepted || o.Gained != 8 {
		t.Fatalf("predator = %v gained %d, want accepted gained 8", o.Result, o.Gained)
	}
}

func TestFeedingRejections(t *testing.T) {
	tests := []struct {
		name   string
		rules  FeedingRules
		intent FeedIntent
		want   FeedRejection
	}{
		{"unknown actor", FeedingRules{}, FeedIntent{ActorID: 99, TargetID: 10, Amount: 1}, FeedRejectedUnknownActor},
		{"dead actor", FeedingRules{}, FeedIntent{ActorID: 4, TargetID: 10, Amount: 1}, FeedRejectedDeadActor},
		{"cost equals energy", FeedingRules{EnergyCost: 10}, FeedIntent{ActorID: 1, TargetID: 10, Amount: 1}, FeedRejectedInsufficientEnergy},
		{"unknown target", FeedingRules{}, FeedIntent{ActorID: 1, TargetID: 99, Amount: 1}, FeedRejectedUnknownTarget},
		{"predator eats plant", FeedingRules{}, FeedIntent{ActorID: 2, TargetID: 10, Amount: 1}, FeedRejectedWrongDiet},
		{"herbivore eats herbivore", FeedingRules{}, FeedIntent{ActorID: 1, TargetID: 3, Amount: 1}, FeedRejectedWrongDiet},
		{"predator eats predator", FeedingRules{}, FeedIntent{ActorID: 2, TargetID: 6}, FeedRejectedWrongDiet},
		{"plant out of reach", FeedingRules{}, FeedIntent{ActorID: 1, TargetID: 11, Amount: 1}, FeedRejectedOutOfReach},
		{"prey out of reach", FeedingRules{}, FeedIntent{ActorID: 2, TargetID: 5}, FeedRejectedOutOfReach},
		{"zero amount", FeedingRules{}, FeedIntent{ActorID: 1, TargetID: 10, Amount: 0}, FeedRejectedInvalidAmount},
		{"already dead prey", FeedingRules{}, FeedIntent{ActorID: 2, TargetID: 4}, FeedRejectedTargetDepleted},
		{"too much", FeedingRules{}, FeedIntent{ActorID: 1, TargetID: 10, Amount: 9}, FeedRejectedInsufficientFood},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := feedingState(
				[]Creature{
					herbivore(1, pos(1, 1), 10),
					predator(2, pos(2, 2), 10),
					herbivore(3, pos(2, 1), 10),
					{ID: 4, Kind: KindHerbivore, Position: pos(3, 3)},
					herbivore(5, pos(4, 4), 10),
					predator(6, pos(1, 2), 10),
				},
				[]Plant{plant(10, pos(1, 1), 8), plant(11, pos(3, 1), 8)},
			)
			before := state.clone()

			result := feedForTest(t, &state, tt.rules, tt.intent)

			if got := result.Outcomes[0].Result; got != tt.want {
				t.Fatalf("result = %v, want %v", got, tt.want)
			}
			if !reflect.DeepEqual(state, before) {
				t.Fatalf("rejected intent mutated state")
			}
		})
	}
}

func TestFeedingDuplicateIntentsAllRejected(t *testing.T) {
	state := feedingState(
		[]Creature{herbivore(1, pos(1, 1), 10)},
		[]Plant{plant(10, pos(1, 1), 8), plant(11, pos(1, 2), 8)},
	)

	result := feedForTest(t, &state, FeedingRules{},
		FeedIntent{ActorID: 1, TargetID: 10, Amount: 1},
		FeedIntent{ActorID: 1, TargetID: 11, Amount: 1},
	)

	for _, o := range result.Outcomes {
		if o.Result != FeedRejectedDuplicate {
			t.Fatalf("outcome %+v, want duplicate", o)
		}
	}
}

func TestFeedingIndependentOfSubmissionOrder(t *testing.T) {
	build := func() WorldState {
		return feedingState(
			[]Creature{herbivore(1, pos(1, 1), 10), herbivore(2, pos(2, 2), 10), herbivore(3, pos(0, 0), 10)},
			[]Plant{plant(10, pos(1, 1), 5)},
		)
	}
	intents := []FeedIntent{
		{ActorID: 3, TargetID: 10, Amount: 2},
		{ActorID: 1, TargetID: 10, Amount: 2},
		{ActorID: 2, TargetID: 10, Amount: 2},
	}
	reversed := []FeedIntent{intents[2], intents[1], intents[0]}

	a, b := build(), build()
	ra := feedForTest(t, &a, FeedingRules{}, intents...)
	rb := feedForTest(t, &b, FeedingRules{}, reversed...)

	if !reflect.DeepEqual(a, b) || !reflect.DeepEqual(ra, rb) {
		t.Fatalf("submission order changed the outcome")
	}
	if got := feedOutcomeFor(t, ra, 3).Result; got != FeedRejectedInsufficientFood {
		t.Fatalf("highest actor = %v, want insufficient food", got)
	}
}

func TestFeedingRejectsNegativeCost(t *testing.T) {
	state := feedingState(nil, nil)
	if _, err := resolveFeeding(&state, nil, FeedingRules{EnergyCost: -1}); err == nil {
		t.Fatalf("expected error for negative cost")
	}
}
