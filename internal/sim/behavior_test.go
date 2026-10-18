package sim

import (
	"reflect"
	"testing"
)

func behaviorConfig() WorldConfig {
	return WorldConfig{Width: 12, Height: 12, RegionWidth: 6, RegionHeight: 6, Seed: 0xbeef}
}

func generateForTest(t *testing.T, config WorldConfig, state WorldState, rules BehaviorRules) Intents {
	t.Helper()
	generate, err := NewBehaviorGenerator(rules)
	if err != nil {
		t.Fatalf("NewBehaviorGenerator: %v", err)
	}
	intents, err := generate(config, state)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return intents
}

func movesOf(intents Intents, id EntityID) []MoveIntent {
	var out []MoveIntent
	for _, m := range intents.Move {
		if m.ActorID == id {
			out = append(out, m)
		}
	}
	return out
}

func feedsOf(intents Intents, id EntityID) []FeedIntent {
	var out []FeedIntent
	for _, f := range intents.Feed {
		if f.ActorID == id {
			out = append(out, f)
		}
	}
	return out
}

func birthsOf(intents Intents, id EntityID) []ReproductionIntent {
	var out []ReproductionIntent
	for _, r := range intents.Reproduce {
		if r.ParentID == id {
			out = append(out, r)
		}
	}
	return out
}

func TestBehaviorHerbivoreEatsNearestPlantInReach(t *testing.T) {
	state := feedingState(
		[]Creature{herbivore(1, pos(5, 5), 10)},
		[]Plant{plant(20, pos(6, 6), 9), plant(30, pos(5, 5), 2), plant(10, pos(4, 5), 9)},
	)

	intents := generateForTest(t, behaviorConfig(), state, DefaultBehaviorRules())

	// The plant underneath (distance 0) beats lower-ID plants at distance 1,
	// and the bite is capped by the plant's energy.
	want := []FeedIntent{{ActorID: 1, TargetID: 30, Amount: 2}}
	if got := feedsOf(intents, 1); !reflect.DeepEqual(got, want) {
		t.Fatalf("feeds = %+v, want %+v", got, want)
	}
	if got := movesOf(intents, 1); len(got) != 0 {
		t.Fatalf("herbivore eating in place also moved: %+v", got)
	}
}

func TestBehaviorNearestTieBreaksByLowestID(t *testing.T) {
	state := feedingState(
		[]Creature{herbivore(1, pos(5, 5), 10)},
		[]Plant{plant(20, pos(6, 5), 9), plant(10, pos(4, 4), 9)},
	)

	intents := generateForTest(t, behaviorConfig(), state, DefaultBehaviorRules())

	if got := feedsOf(intents, 1); len(got) != 1 || got[0].TargetID != 10 {
		t.Fatalf("feeds = %+v, want target 10", got)
	}
}

func TestBehaviorHerbivoreApproachesAndEatsSameTick(t *testing.T) {
	w := NewWorld("test", behaviorConfig(), WithIntentGenerator(mustBehavior(t, DefaultBehaviorRules())))
	w.AddCreature(herbivore(1, pos(2, 2), 10))
	w.AddPlant(plant(2, pos(4, 2), 9))

	if err := w.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}

	// Plant at distance 2: step to x=3 (cost 1), then bite 3.
	got := mustCreature(t, w, 1)
	if got.Position.X != 3 || got.Energy != 12 {
		t.Fatalf("herbivore = %+v, want x=3 with energy 12", got)
	}
}

func TestBehaviorHerbivoreFleesPredatorInFleeRadius(t *testing.T) {
	state := feedingState(
		[]Creature{herbivore(1, pos(5, 5), 10), predator(2, pos(3, 5), 10)},
		[]Plant{plant(3, pos(5, 5), 9)},
	)

	intents := generateForTest(t, behaviorConfig(), state, DefaultBehaviorRules())

	// The predator is two away, inside the default FleeRadius, so the
	// herbivore abandons the plant under it and moves out of pounce range.
	moves := movesOf(intents, 1)
	if len(moves) != 1 {
		t.Fatalf("herbivore moves = %+v, want one flee move", moves)
	}
	if d := chebyshevDistance(moves[0].To, pos(3, 5)); d != 3 {
		t.Fatalf("fled to %+v at distance %d from predator, want 3", moves[0].To, d)
	}
	if got := feedsOf(intents, 1); len(got) != 0 {
		t.Fatalf("fleeing herbivore also fed: %+v", got)
	}
}

func TestBehaviorCorneredHerbivoreStays(t *testing.T) {
	// Every neighbor of the corner is taken, so no move improves safety.
	state := feedingState(
		[]Creature{
			herbivore(1, pos(0, 0), 10),
			predator(2, pos(1, 0), 10),
			predator(3, pos(0, 1), 10),
			predator(4, pos(1, 1), 10),
		},
		nil,
	)

	intents := generateForTest(t, behaviorConfig(), state, DefaultBehaviorRules())

	if got := movesOf(intents, 1); len(got) != 0 {
		t.Fatalf("cornered herbivore moved: %+v", got)
	}
}

func TestBehaviorPredatorEatsAdjacentHerbivore(t *testing.T) {
	state := feedingState(
		[]Creature{predator(1, pos(5, 5), 10), herbivore(9, pos(6, 6), 4), herbivore(7, pos(6, 4), 4)},
		nil,
	)

	intents := generateForTest(t, behaviorConfig(), state, DefaultBehaviorRules())

	want := []FeedIntent{{ActorID: 1, TargetID: 7}}
	if got := feedsOf(intents, 1); !reflect.DeepEqual(got, want) {
		t.Fatalf("feeds = %+v, want %+v", got, want)
	}
	if got := movesOf(intents, 1); len(got) != 0 {
		t.Fatalf("predator eating in place also moved: %+v", got)
	}
}

func TestBehaviorPredatorPouncesOnHerbivoreTwoAway(t *testing.T) {
	// With FleeRadius 1 the herbivore keeps eating and never sees the pounce.
	rules := DefaultBehaviorRules()
	rules.FleeRadius = 1
	w := NewWorld("test", behaviorConfig(), WithIntentGenerator(mustBehavior(t, rules)))
	w.AddCreature(predator(1, pos(2, 2), 10))
	w.AddCreature(herbivore(2, pos(4, 2), 6))
	w.AddPlant(plant(3, pos(4, 2), 9))

	if err := w.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}

	if _, alive := w.Creature(2); alive {
		t.Fatalf("herbivore survived the pounce")
	}
	// Feeding runs in actor ID order, so predator 1 eats herbivore 2 before
	// the herbivore's own bite: 10 - 1 metabolism - 1 move + 6 = 14.
	if got := mustCreature(t, w, 1); got.Energy != 14 {
		t.Fatalf("predator energy = %d, want 14", got.Energy)
	}
}

func TestBehaviorReproducesWithoutMoving(t *testing.T) {
	rules := DefaultBehaviorRules()
	state := feedingState(
		[]Creature{herbivore(1, pos(5, 5), rules.HerbivoreReproduceAt)},
		[]Plant{plant(2, pos(9, 9), 9)},
	)

	intents := generateForTest(t, behaviorConfig(), state, rules)

	births := birthsOf(intents, 1)
	if len(births) != 1 || chebyshevDistance(births[0].To, pos(5, 5)) != 1 {
		t.Fatalf("births = %+v, want one into a neighbor", births)
	}
	if got := movesOf(intents, 1); len(got) != 0 {
		t.Fatalf("reproducing herbivore moved: %+v", got)
	}
}

func TestBehaviorReproducesAndEatsInReach(t *testing.T) {
	rules := DefaultBehaviorRules()
	rules.PredatorSatedAt = 0 // a sated predator would not hunt
	state := feedingState(
		[]Creature{predator(1, pos(5, 5), rules.PredatorReproduceAt), herbivore(2, pos(5, 6), 4)},
		nil,
	)

	intents := generateForTest(t, behaviorConfig(), state, rules)

	if len(birthsOf(intents, 1)) != 1 || len(feedsOf(intents, 1)) != 1 || len(movesOf(intents, 1)) != 0 {
		t.Fatalf("intents = %+v, want one birth, one feed, no move", intents)
	}
}

func TestBehaviorIgnoresFoodBeyondSenseRadius(t *testing.T) {
	rules := DefaultBehaviorRules()
	state := feedingState(
		[]Creature{herbivore(1, pos(0, 0), 10)},
		[]Plant{plant(2, pos(rules.SenseRadius+1, 0), 9)},
	)

	intents := generateForTest(t, behaviorConfig(), state, rules)

	if got := feedsOf(intents, 1); len(got) != 0 {
		t.Fatalf("fed on unseen plant: %+v", got)
	}
}

func TestBehaviorDoesNotMutateSnapshot(t *testing.T) {
	state := seededWorld(t).State()
	before := state.clone()

	generateForTest(t, stepConfig(), state, DefaultBehaviorRules())

	if !reflect.DeepEqual(state, before) {
		t.Fatalf("generator mutated its snapshot")
	}
}

// TestBehaviorIntentsAreWellFormed checks properties that must hold for every
// seed: one move per actor, moves and births target free in-bounds neighbors,
// and every feed targets real food in the actor's diet.
func TestBehaviorIntentsAreWellFormed(t *testing.T) {
	for seed := uint64(0); seed < 200; seed++ {
		config := behaviorConfig()
		config.Seed = seed
		w := populatedWorld(t, config)
		state := w.State()

		intents := generateForTest(t, config, state, DefaultBehaviorRules())
		occupied := make(map[Position]bool)
		for _, c := range state.Creatures {
			occupied[c.Position] = true
		}

		seen := make(map[EntityID]bool)
		for _, m := range intents.Move {
			from := state.Creatures[m.ActorID].Position
			if seen[m.ActorID] || !m.To.InBounds(config.Width, config.Height) ||
				chebyshevDistance(from, m.To) != 1 || occupied[m.To] {
				t.Fatalf("seed %d: bad move %+v from %+v", seed, m, from)
			}
			seen[m.ActorID] = true
		}
		for _, r := range intents.Reproduce {
			from := state.Creatures[r.ParentID].Position
			if !r.To.InBounds(config.Width, config.Height) || chebyshevDistance(from, r.To) != 1 || occupied[r.To] {
				t.Fatalf("seed %d: bad birth %+v from %+v", seed, r, from)
			}
		}
		for _, f := range intents.Feed {
			actor := state.Creatures[f.ActorID]
			if _, isPlant := state.Plants[f.TargetID]; isPlant {
				if actor.Kind != KindHerbivore || f.Amount <= 0 {
					t.Fatalf("seed %d: bad plant feed %+v by %+v", seed, f, actor)
				}
				continue
			}
			if actor.Kind != KindPredator || state.Creatures[f.TargetID].Kind != KindHerbivore {
				t.Fatalf("seed %d: bad prey feed %+v", seed, f)
			}
		}
	}
}

func TestBehaviorDeterministicPerSeed(t *testing.T) {
	config := behaviorConfig()
	state := populatedWorld(t, config).State()

	a := generateForTest(t, config, state, DefaultBehaviorRules())
	b := generateForTest(t, config, state, DefaultBehaviorRules())
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("same seed produced different intents")
	}

	config.Seed++
	if c := generateForTest(t, config, state, DefaultBehaviorRules()); reflect.DeepEqual(a, c) {
		t.Fatalf("different seeds produced identical intents; randomness unused")
	}
}

func TestBehaviorLongRunStaysValid(t *testing.T) {
	config := behaviorConfig()
	w := populatedWorld(t, config)

	var births, kills bool
	for tick := 0; tick < 300; tick++ {
		before := w.State()
		if err := w.Step(); err != nil {
			t.Fatalf("tick %d: %v", tick+1, err)
		}
		after := w.State()
		if after.NextEntity > before.NextEntity {
			births = true
		}
		for _, id := range before.CreatureIDs {
			if before.Creatures[id].Kind != KindHerbivore {
				continue
			}
			if _, alive := after.Creatures[id]; !alive {
				kills = true
			}
		}
	}
	if !births || !kills {
		t.Fatalf("300 ticks without both births (%v) and deaths (%v); behavior is not exercising the phases", births, kills)
	}
}

func TestNewBehaviorGeneratorRejectsInvalidRules(t *testing.T) {
	for _, mutate := range []func(*BehaviorRules){
		func(r *BehaviorRules) { r.SenseRadius = 0 },
		func(r *BehaviorRules) { r.SenseRadius = MaxSenseRadius + 1 },
		func(r *BehaviorRules) { r.FleeRadius = 0 },
		func(r *BehaviorRules) { r.FleeRadius = r.SenseRadius + 1 },
		func(r *BehaviorRules) { r.HerbivoreBite = 0 },
		func(r *BehaviorRules) { r.HerbivoreReproduceAt = 0 },
		func(r *BehaviorRules) { r.PredatorReproduceAt = -1 },
		func(r *BehaviorRules) { r.PredatorSatedAt = -1 },
	} {
		rules := DefaultBehaviorRules()
		mutate(&rules)
		if _, err := NewBehaviorGenerator(rules); err == nil {
			t.Fatalf("expected error for %+v", rules)
		}
	}
}

func mustBehavior(t *testing.T, rules BehaviorRules) IntentGenerator {
	t.Helper()
	generate, err := NewBehaviorGenerator(rules)
	if err != nil {
		t.Fatalf("NewBehaviorGenerator: %v", err)
	}
	return generate
}

// populatedWorld scatters plants, herbivores and predators on a fixed pattern.
func populatedWorld(t *testing.T, config WorldConfig) *World {
	t.Helper()
	w := NewWorld("test", config, WithIntentGenerator(mustBehavior(t, DefaultBehaviorRules())))
	for i := int32(0); i < config.Width; i++ {
		for j := int32(0); j < config.Height; j++ {
			switch (i*7 + j*3) % 11 {
			case 0:
				w.AddPlant(plant(0, pos(i, j), 15))
			case 1:
				w.AddCreature(herbivore(0, pos(i, j), 12))
			case 2:
				if (i+j)%3 == 0 {
					w.AddCreature(predator(0, pos(i, j), 20))
				}
			}
		}
	}
	return w
}

func TestBehaviorSatedPredatorRestsInsteadOfHunting(t *testing.T) {
	rules := DefaultBehaviorRules()
	rules.PredatorSatedAt = 25
	rules.PredatorReproduceAt = 100
	state := feedingState([]Creature{predator(1, pos(5, 5), 25), herbivore(2, pos(5, 6), 4), herbivore(3, pos(7, 5), 4)}, nil)

	intents := generateForTest(t, behaviorConfig(), state, rules)

	if len(feedsOf(intents, 1)) != 0 || len(movesOf(intents, 1)) != 0 || len(birthsOf(intents, 1)) != 0 {
		t.Fatalf("sated predator acted: %+v", intents)
	}
}

func TestBehaviorSatedPredatorStillReproduces(t *testing.T) {
	rules := DefaultBehaviorRules()
	rules.PredatorSatedAt = 25
	state := feedingState([]Creature{predator(1, pos(5, 5), rules.PredatorReproduceAt), herbivore(2, pos(5, 6), 4)}, nil)

	intents := generateForTest(t, behaviorConfig(), state, rules)

	if len(birthsOf(intents, 1)) != 1 || len(feedsOf(intents, 1)) != 0 {
		t.Fatalf("intents = %+v, want one birth and no feed", intents)
	}
}

func TestBehaviorHungryPredatorHuntsWhenSatiationEnabled(t *testing.T) {
	rules := DefaultBehaviorRules()
	rules.PredatorSatedAt = 25
	state := feedingState([]Creature{predator(1, pos(5, 5), 24), herbivore(2, pos(5, 6), 4)}, nil)

	intents := generateForTest(t, behaviorConfig(), state, rules)

	if got := feedsOf(intents, 1); len(got) != 1 || got[0].TargetID != 2 {
		t.Fatalf("feeds = %+v, want hunt of herbivore 2", got)
	}
}
