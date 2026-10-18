package sim

import (
	"math"
	"reflect"
	"testing"
)

var seedingRules = EnvironmentRules{SeedThreshold: 20, SeedCost: 8, SeedlingEnergy: 5}

func environmentForTest(t *testing.T, config WorldConfig, state *WorldState, rules EnvironmentRules) {
	t.Helper()
	if err := applyEnvironment(config, state, rules); err != nil {
		t.Fatalf("applyEnvironment: %v", err)
	}
	if err := validateState(config, *state); err != nil {
		t.Fatalf("state invalid after environment: %v", err)
	}
}

func TestSeedingCreatesSeedlingAndChargesParent(t *testing.T) {
	state := feedingState(nil, []Plant{plant(1, pos(5, 5), 20)})

	environmentForTest(t, stepConfig(), &state, seedingRules)

	parent := state.Plants[1]
	if parent.Energy != 12 || parent.Version != 1 {
		t.Fatalf("parent = %+v, want energy 12 version 1", parent)
	}
	seedling, ok := state.Plants[2]
	if !ok {
		t.Fatalf("no seedling with ID 2; plants %v", state.PlantIDs)
	}
	want := Plant{ID: 2, Position: seedling.Position, Energy: 5, Health: 1}
	if seedling != want || chebyshevDistance(seedling.Position, pos(5, 5)) != 1 {
		t.Fatalf("seedling = %+v, want %+v on a neighbor of (5,5)", seedling, want)
	}
	if state.NextEntity != 3 {
		t.Fatalf("NextEntity = %d, want 3", state.NextEntity)
	}
}

func TestSeedingBelowThresholdDoesNothing(t *testing.T) {
	state := feedingState(nil, []Plant{plant(1, pos(5, 5), 19)})
	before := state.clone()

	environmentForTest(t, stepConfig(), &state, seedingRules)

	if !reflect.DeepEqual(state, before) {
		t.Fatalf("plant below threshold seeded")
	}
}

func TestSeedingUsesPostGrowthEnergy(t *testing.T) {
	state := feedingState(nil, []Plant{plant(1, pos(5, 5), 19)})
	rules := seedingRules
	rules.PlantGrowth = 1

	environmentForTest(t, stepConfig(), &state, rules)

	// 19 + 1 growth reaches the threshold; 20 - 8 = 12.
	if len(state.PlantIDs) != 2 || state.Plants[1].Energy != 12 {
		t.Fatalf("plants %v, parent %+v; want a seedling and parent energy 12", state.PlantIDs, state.Plants[1])
	}
	// Seedlings do not grow in the tick they appear.
	if got := state.Plants[2].Energy; got != 5 {
		t.Fatalf("seedling energy = %d, want 5", got)
	}
}

func TestSeedingSurroundedPlantDoesNotSeed(t *testing.T) {
	plants := []Plant{plant(1, pos(5, 5), 30)}
	next := EntityID(2)
	for _, o := range neighborOffsets {
		plants = append(plants, plant(next, pos(5+o[0], 5+o[1]), 3))
		next++
	}
	state := feedingState(nil, plants)
	before := state.clone()

	environmentForTest(t, stepConfig(), &state, seedingRules)

	if !reflect.DeepEqual(state, before) {
		t.Fatalf("surrounded plant seeded or was charged")
	}
}

func TestSeedingIgnoresCreaturesAndDepletedPlants(t *testing.T) {
	// A 3x1 strip: the only neighbor of (0,0) is (1,0), which holds a creature
	// and a depleted plant. Neither blocks a seedling.
	config := WorldConfig{Width: 3, Height: 1, RegionWidth: 3, RegionHeight: 1}
	state := feedingState(
		[]Creature{herbivore(3, pos(1, 0), 5)},
		[]Plant{plant(1, pos(0, 0), 20), {ID: 2, Position: pos(1, 0)}, plant(4, pos(2, 0), 1)},
	)

	environmentForTest(t, config, &state, seedingRules)

	if got := state.Plants[5]; got.Position != pos(1, 0) || !got.IsAlive() {
		t.Fatalf("seedling = %+v, want a living plant at (1,0)", got)
	}
}

func TestSeedingConflictLowestParentWinsLoserUncharged(t *testing.T) {
	// Both parents' only free neighbor is (1,0).
	config := WorldConfig{Width: 3, Height: 1, RegionWidth: 3, RegionHeight: 1}
	state := feedingState(nil, []Plant{plant(5, pos(0, 0), 20), plant(2, pos(2, 0), 20)})

	environmentForTest(t, config, &state, seedingRules)

	if got := state.Plants[2].Energy; got != 12 {
		t.Fatalf("winning parent 2 energy = %d, want 12", got)
	}
	if got := state.Plants[5]; got.Energy != 20 || got.Version != 0 {
		t.Fatalf("losing parent 5 = %+v, want unchanged", got)
	}
	if got := state.Plants[6]; got.Position != pos(1, 0) {
		t.Fatalf("seedling = %+v, want ID 6 at (1,0)", got)
	}
}

func TestSeedingIDsFollowParentOrder(t *testing.T) {
	state := feedingState(nil, []Plant{plant(9, pos(8, 8), 20), plant(3, pos(1, 1), 20)})
	state.NextEntity = 20

	environmentForTest(t, stepConfig(), &state, seedingRules)

	if p := state.Plants[20].Position; chebyshevDistance(p, pos(1, 1)) != 1 {
		t.Fatalf("ID 20 at %+v, want next to parent 3", p)
	}
	if p := state.Plants[21].Position; chebyshevDistance(p, pos(8, 8)) != 1 {
		t.Fatalf("ID 21 at %+v, want next to parent 9", p)
	}
}

func TestSeedingAllocationFailureLeavesSeedingUnapplied(t *testing.T) {
	state := feedingState(nil, []Plant{plant(1, pos(5, 5), 20)})
	state.NextEntity = math.MaxUint64 - 1
	before := state.clone()

	if err := applyEnvironment(stepConfig(), &state, seedingRules); err == nil {
		t.Fatalf("expected allocation error")
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatalf("failed allocation changed state")
	}
}

func TestSeedingIsDeterministic(t *testing.T) {
	build := func() WorldState {
		var plants []Plant
		for i := int32(0); i < 10; i++ {
			plants = append(plants, plant(EntityID(i+1), pos(i, (i*3)%10), 25))
		}
		return feedingState(nil, plants)
	}

	a, b := build(), build()
	environmentForTest(t, stepConfig(), &a, seedingRules)
	environmentForTest(t, stepConfig(), &b, seedingRules)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("same inputs produced different seedlings")
	}

	other := stepConfig()
	other.Seed++
	c := build()
	environmentForTest(t, other, &c, seedingRules)
	if reflect.DeepEqual(a, c) {
		t.Fatalf("different seeds produced identical placements; randomness unused")
	}
}

func TestEnvironmentRejectsInvalidSeedingRules(t *testing.T) {
	for _, rules := range []EnvironmentRules{
		{SeedThreshold: -1},
		{SeedThreshold: 10, SeedCost: -1, SeedlingEnergy: 1},
		{SeedThreshold: 10, SeedCost: 10, SeedlingEnergy: 1},
		{SeedThreshold: 10, SeedCost: 5, SeedlingEnergy: 0},
	} {
		state := feedingState(nil, nil)
		if err := applyEnvironment(stepConfig(), &state, rules); err == nil {
			t.Fatalf("expected error for %+v", rules)
		}
	}
}

func TestStepSeedlingIDsPrecedeBirths(t *testing.T) {
	w := NewWorld("test", stepConfig(),
		WithRules(Rules{
			Environment:  seedingRules,
			Reproduction: defaultReproductionRules,
		}),
		WithIntentGenerator(fixedIntents(Intents{
			Reproduce: []ReproductionIntent{{ParentID: 1, To: pos(1, 2)}},
		})),
	)
	w.AddCreature(herbivore(1, pos(1, 1), 10))
	w.AddPlant(plant(2, pos(7, 7), 20))

	if err := w.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}

	if _, ok := w.Plant(3); !ok {
		t.Fatalf("seedling did not receive ID 3")
	}
	if c := mustCreature(t, w, 4); c.Position != pos(1, 2) {
		t.Fatalf("offspring = %+v, want ID 4 at (1,2)", c)
	}
}

var sproutingRules = EnvironmentRules{SproutAttempts: 10, SproutEnergy: 4}

func TestSproutingPlacesPlantsOnEmptyCells(t *testing.T) {
	state := feedingState([]Creature{herbivore(1, pos(0, 0), 5)}, nil)

	environmentForTest(t, stepConfig(), &state, sproutingRules)

	sprouts := len(state.PlantIDs)
	if sprouts == 0 || sprouts > int(sproutingRules.SproutAttempts) {
		t.Fatalf("sprouted %d plants from %d attempts", sprouts, sproutingRules.SproutAttempts)
	}
	for i, id := range state.PlantIDs {
		if want := EntityID(2 + i); id != want {
			t.Fatalf("sprout %d has ID %d, want %d", i, id, want)
		}
		if got := state.Plants[id]; got.Energy != 4 || got.Health != 1 {
			t.Fatalf("sprout = %+v, want energy 4 health 1", got)
		}
	}
}

func TestSproutingSkipsCellsWithLivingPlants(t *testing.T) {
	config := WorldConfig{Width: 3, Height: 1, RegionWidth: 3, RegionHeight: 1}
	state := feedingState(nil, []Plant{plant(1, pos(0, 0), 5), plant(2, pos(1, 0), 5), plant(3, pos(2, 0), 5)})
	before := state.clone()

	environmentForTest(t, config, &state, EnvironmentRules{SproutAttempts: 50, SproutEnergy: 4})

	if !reflect.DeepEqual(state, before) {
		t.Fatalf("sprouted onto a cell that already holds a living plant")
	}
}

func TestSproutingNeverDuplicatesACell(t *testing.T) {
	config := WorldConfig{Width: 2, Height: 2, RegionWidth: 2, RegionHeight: 2}
	state := feedingState(nil, nil)

	// Far more attempts than cells: every cell fills exactly once.
	environmentForTest(t, config, &state, EnvironmentRules{SproutAttempts: 200, SproutEnergy: 4})

	if len(state.PlantIDs) != 4 {
		t.Fatalf("sprouted %d plants on a 2x2 world, want 4", len(state.PlantIDs))
	}
}

func TestSeedlingsClaimCellsBeforeSprouts(t *testing.T) {
	// (1,0) is the parent's only free neighbor; sprouting can then fill only
	// (2,0). Seedlings are allocated first.
	config := WorldConfig{Width: 3, Height: 1, RegionWidth: 3, RegionHeight: 1}
	state := feedingState(nil, []Plant{plant(1, pos(0, 0), 20)})
	rules := seedingRules
	rules.SproutAttempts, rules.SproutEnergy = 50, 4

	environmentForTest(t, config, &state, rules)

	if got := state.Plants[2]; got.Position != pos(1, 0) || got.Energy != rules.SeedlingEnergy {
		t.Fatalf("ID 2 = %+v, want seedling at (1,0)", got)
	}
	if got := state.Plants[3]; got.Position != pos(2, 0) || got.Energy != 4 {
		t.Fatalf("ID 3 = %+v, want sprout at (2,0)", got)
	}
	if len(state.PlantIDs) != 3 {
		t.Fatalf("plants %v, want exactly 3", state.PlantIDs)
	}
}

func TestSproutingIsDeterministic(t *testing.T) {
	a, b := feedingState(nil, nil), feedingState(nil, nil)
	environmentForTest(t, stepConfig(), &a, sproutingRules)
	environmentForTest(t, stepConfig(), &b, sproutingRules)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("same inputs produced different sprouts")
	}

	other := stepConfig()
	other.Seed++
	c := feedingState(nil, nil)
	environmentForTest(t, other, &c, sproutingRules)
	if reflect.DeepEqual(a, c) {
		t.Fatalf("different seeds produced identical sprouts; randomness unused")
	}
}

func TestSproutingAllocationFailureLeavesStateUnchanged(t *testing.T) {
	state := feedingState(nil, nil)
	state.NextEntity = math.MaxUint64 - 1
	before := state.clone()

	if err := applyEnvironment(stepConfig(), &state, sproutingRules); err == nil {
		t.Fatalf("expected allocation error")
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatalf("failed allocation changed state")
	}
}

func TestEnvironmentRejectsInvalidSproutingRules(t *testing.T) {
	for _, rules := range []EnvironmentRules{
		{SproutAttempts: -1, SproutEnergy: 1},
		{SproutAttempts: MaxSproutAttempts + 1, SproutEnergy: 1},
		{SproutAttempts: 1, SproutEnergy: 0},
	} {
		state := feedingState(nil, nil)
		if err := applyEnvironment(stepConfig(), &state, rules); err == nil {
			t.Fatalf("expected error for %+v", rules)
		}
	}
}

func TestStepPlantsRecoverFromExtinction(t *testing.T) {
	w := NewWorld("test", stepConfig(), WithRules(Rules{
		Environment:  sproutingRules,
		Reproduction: defaultReproductionRules,
	}))

	if err := w.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}
	if len(w.PlantList()) == 0 {
		t.Fatalf("no plants sprouted in an empty world")
	}
}

func TestMetabolismChargesEachKind(t *testing.T) {
	state := feedingState([]Creature{herbivore(1, pos(0, 0), 10), predator(2, pos(1, 0), 10)}, nil)

	if err := applyMetabolism(&state, MetabolismRules{Herbivore: 1, Predator: 3}); err != nil {
		t.Fatalf("applyMetabolism: %v", err)
	}

	if got := state.Creatures[1]; got.Energy != 9 || got.Version != 1 {
		t.Fatalf("herbivore = %+v, want energy 9 version 1", got)
	}
	if got := state.Creatures[2]; got.Energy != 7 {
		t.Fatalf("predator energy = %d, want 7", got.Energy)
	}
}

func TestMetabolismKillsAndDeadCannotAct(t *testing.T) {
	w := NewWorld("test", stepConfig(),
		WithRules(Rules{
			Metabolism:   MetabolismRules{Predator: 2},
			Reproduction: defaultReproductionRules,
		}),
		WithIntentGenerator(fixedIntents(Intents{
			Move: []MoveIntent{{ActorID: 1, To: pos(2, 1)}},
			Feed: []FeedIntent{{ActorID: 1, TargetID: 2}},
		})),
	)
	w.AddCreature(predator(1, pos(1, 1), 2))
	w.AddCreature(herbivore(2, pos(2, 2), 5))

	if err := w.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}

	if _, alive := w.Creature(1); alive {
		t.Fatalf("predator survived metabolism that drained its energy")
	}
	if got := mustCreature(t, w, 2); got.Energy != 5 {
		t.Fatalf("starved predator still fed: herbivore %+v", got)
	}
}

func TestMetabolismZeroIsNoopAndNegativeRejected(t *testing.T) {
	state := feedingState([]Creature{herbivore(1, pos(0, 0), 10)}, nil)
	before := state.clone()

	if err := applyMetabolism(&state, MetabolismRules{}); err != nil {
		t.Fatalf("applyMetabolism: %v", err)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatalf("zero metabolism changed state")
	}
	if err := applyMetabolism(&state, MetabolismRules{Predator: -1}); err == nil {
		t.Fatalf("expected error for negative metabolism")
	}
}

func TestAgingAgesLivingEntitiesOnly(t *testing.T) {
	state := feedingState(
		[]Creature{herbivore(1, pos(0, 0), 5), {ID: 2, Kind: KindPredator, Position: pos(1, 0)}},
		[]Plant{plant(3, pos(0, 1), 5), {ID: 4, Position: pos(1, 1)}},
	)

	if err := applyAging(&state, AgingRules{}); err != nil {
		t.Fatalf("applyAging: %v", err)
	}

	if got := state.Creatures[1]; got.Age != 1 || got.Version != 1 || !got.IsAlive() {
		t.Fatalf("living creature = %+v, want age 1 version 1 and alive", got)
	}
	if got := state.Plants[3]; got.Age != 1 || got.Version != 1 {
		t.Fatalf("living plant = %+v, want age 1 version 1", got)
	}
	if got := state.Creatures[2]; got.Age != 0 || got.Version != 0 {
		t.Fatalf("dead creature aged: %+v", got)
	}
	if got := state.Plants[4]; got.Age != 0 || got.Version != 0 {
		t.Fatalf("dead plant aged: %+v", got)
	}
}

func TestAgingKillsAtEachKindsLifespan(t *testing.T) {
	aged := func(c Creature, age uint32) Creature { c.Age = age; return c }
	state := feedingState(
		[]Creature{
			aged(herbivore(1, pos(0, 0), 5), 9), // reaches 10: dies
			aged(herbivore(2, pos(1, 0), 5), 8), // reaches 9: lives
			aged(predator(3, pos(2, 0), 5), 9),  // predator lifespan 20: lives
			aged(predator(4, pos(3, 0), 5), 19), // reaches 20: dies
		},
		[]Plant{{ID: 5, Position: pos(0, 1), Energy: 5, Health: 1, Age: 1000}},
	)

	if err := applyAging(&state, AgingRules{Herbivore: 10, Predator: 20}); err != nil {
		t.Fatalf("applyAging: %v", err)
	}

	for id, wantAlive := range map[EntityID]bool{1: false, 2: true, 3: true, 4: false} {
		got := state.Creatures[id]
		if got.IsAlive() != wantAlive {
			t.Fatalf("creature %d = %+v, want alive=%v", id, got, wantAlive)
		}
		if !wantAlive && got.Energy != 0 {
			t.Fatalf("creature %d died of age with energy %d", id, got.Energy)
		}
	}
	// Plant lifespan zero means no limit.
	if !state.Plants[5].IsAlive() {
		t.Fatalf("plant with no lifespan died of age")
	}
}

func TestAgingSaturatesInsteadOfOverflowing(t *testing.T) {
	c := herbivore(1, pos(0, 0), 5)
	c.Age = math.MaxUint32
	state := feedingState([]Creature{c}, nil)

	if err := applyAging(&state, AgingRules{}); err != nil {
		t.Fatalf("applyAging: %v", err)
	}
	if got := state.Creatures[1].Age; got != math.MaxUint32 {
		t.Fatalf("age = %d, want saturated at MaxUint32", got)
	}
}

func TestStepCreatureDyingOfAgeCannotActAndIsRemoved(t *testing.T) {
	w := NewWorld("test", stepConfig(),
		WithRules(Rules{Aging: AgingRules{Herbivore: 5}, Reproduction: defaultReproductionRules}),
		WithIntentGenerator(fixedIntents(Intents{
			Move: []MoveIntent{{ActorID: 1, To: pos(2, 1)}, {ActorID: 2, To: pos(1, 1)}},
		})),
	)
	old := herbivore(0, pos(1, 1), 10)
	old.Age = 4
	w.AddCreature(old)
	w.AddCreature(herbivore(0, pos(1, 2), 10))

	if err := w.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}

	if _, alive := w.Creature(1); alive {
		t.Fatalf("creature that reached its lifespan was not removed")
	}
	// Its cell was released before movement, so creature 2 could move in.
	if got := mustCreature(t, w, 2).Position; got != pos(1, 1) {
		t.Fatalf("creature 2 at %+v, want (1,1) vacated by the old creature", got)
	}
}

func TestStepPlantDyingOfAgeFreesCellForSeedling(t *testing.T) {
	// 2x1 strip: the parent's only neighbor holds a plant that dies of age
	// this tick, so the seedling can take its cell.
	config := WorldConfig{Width: 2, Height: 1, RegionWidth: 2, RegionHeight: 1}
	rules := DefaultRules()
	rules.Environment = seedingRules
	rules.Aging = AgingRules{Plant: 3}
	w := NewWorld("test", config, WithRules(rules))
	w.AddPlant(plant(0, pos(0, 0), 20))
	old := plant(0, pos(1, 0), 5)
	old.Age = 2
	w.AddPlant(old)

	if err := w.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}

	if _, alive := w.Plant(2); alive {
		t.Fatalf("plant that reached its lifespan was not removed")
	}
	if got, ok := w.Plant(3); !ok || got.Position != pos(1, 0) || got.Age != 0 {
		t.Fatalf("seedling = %+v (exists %v), want age 0 at (1,0)", got, ok)
	}
}
