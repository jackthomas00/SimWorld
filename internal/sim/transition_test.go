package sim

import (
	"errors"
	"math"
	"math/rand/v2"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func stepConfig() WorldConfig {
	return WorldConfig{Width: 10, Height: 10, RegionWidth: 5, RegionHeight: 5, Seed: 0x5eed}
}

// fixedIntents returns a generator that emits the same intents every tick.
func fixedIntents(intents Intents) IntentGenerator {
	return func(WorldConfig, WorldState) (Intents, error) { return intents, nil }
}

// assertStepFails runs Step, requires an error whose message starts with
// phase, and requires the full committed state to be unchanged.
func assertStepFails(t *testing.T, w *World, phase string) error {
	t.Helper()
	before := w.State()
	err := w.Step()
	if err == nil {
		t.Fatalf("Step succeeded, want %s failure", phase)
	}
	if !strings.HasPrefix(err.Error(), phase) {
		t.Fatalf("Step error = %q, want %s failure", err, phase)
	}
	if after := w.State(); !reflect.DeepEqual(before, after) {
		t.Fatalf("failed Step changed committed state:\nbefore %+v\nafter  %+v", before, after)
	}
	return err
}

// --- applyEnvironment ---

func TestApplyEnvironmentGrowsLivingPlantsOnly(t *testing.T) {
	state := feedingState(nil, []Plant{plant(1, pos(0, 0), 5), {ID: 2, Position: pos(1, 0)}})

	if err := applyEnvironment(stepConfig(), &state, EnvironmentRules{PlantGrowth: 3}); err != nil {
		t.Fatalf("applyEnvironment: %v", err)
	}

	if got := state.Plants[1]; got.Energy != 8 || got.Version != 1 {
		t.Fatalf("living plant = %+v, want energy 8 version 1", got)
	}
	if got := state.Plants[2]; got.Energy != 0 || got.Version != 0 {
		t.Fatalf("depleted plant regrew: %+v", got)
	}
}

func TestApplyEnvironmentZeroGrowthIsNoop(t *testing.T) {
	state := feedingState(nil, []Plant{plant(1, pos(0, 0), 5)})
	before := state.clone()

	if err := applyEnvironment(stepConfig(), &state, EnvironmentRules{}); err != nil {
		t.Fatalf("applyEnvironment: %v", err)
	}
	if !reflect.DeepEqual(state, before) {
		t.Fatalf("zero growth changed state")
	}
}

func TestApplyEnvironmentErrors(t *testing.T) {
	state := feedingState(nil, []Plant{plant(1, pos(0, 0), math.MaxInt64)})
	if err := applyEnvironment(stepConfig(), &state, EnvironmentRules{PlantGrowth: 1}); err == nil {
		t.Fatalf("expected overflow error")
	}
	if err := applyEnvironment(stepConfig(), &state, EnvironmentRules{PlantGrowth: -1}); err == nil {
		t.Fatalf("expected negative growth error")
	}
}

// --- removeDead ---

func TestRemoveDeadRemovesFromMapsAndSlices(t *testing.T) {
	state := feedingState(
		[]Creature{
			herbivore(1, pos(0, 0), 5),
			{ID: 2, Kind: KindHerbivore, Position: pos(1, 0)},
			herbivore(3, pos(2, 0), 5),
		},
		[]Plant{{ID: 4, Position: pos(0, 1)}, plant(5, pos(1, 1), 5)},
	)

	removeDead(&state)

	if want := []EntityID{1, 3}; !reflect.DeepEqual(state.CreatureIDs, want) {
		t.Fatalf("CreatureIDs = %v, want %v", state.CreatureIDs, want)
	}
	if want := []EntityID{5}; !reflect.DeepEqual(state.PlantIDs, want) {
		t.Fatalf("PlantIDs = %v, want %v", state.PlantIDs, want)
	}
	if _, exists := state.Creatures[2]; exists {
		t.Fatalf("dead creature still in map")
	}
	if _, exists := state.Plants[4]; exists {
		t.Fatalf("depleted plant still in map")
	}
	if err := validateState(stepConfig(), state); err != nil {
		t.Fatalf("state invalid after cleanup: %v", err)
	}
}

// --- Step: success paths ---

func TestStepEmptyTickAdvancesExactlyOnce(t *testing.T) {
	w := NewWorld("test", stepConfig(), WithRules(Rules{Reproduction: defaultReproductionRules}))
	w.AddCreature(herbivore(0, pos(1, 1), 10))
	w.AddPlant(plant(0, pos(2, 2), 5))
	before := w.State()

	if err := w.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}

	after := w.State()
	if after.Tick != 1 {
		t.Fatalf("Tick = %d, want 1", after.Tick)
	}
	// Aging always runs: every entity is one tick older, with a new version.
	before.Tick = 1
	for id, c := range before.Creatures {
		c.Age, c.Version = c.Age+1, c.Version+1
		before.Creatures[id] = c
	}
	for id, p := range before.Plants {
		p.Age, p.Version = p.Age+1, p.Version+1
		before.Plants[id] = p
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("empty tick changed more than the tick and ages:\nwant %+v\ngot  %+v", before, after)
	}
}

func TestStepRunsEveryPhase(t *testing.T) {
	w := NewWorld("test", stepConfig(),
		WithRules(Rules{
			Environment:  EnvironmentRules{PlantGrowth: 2},
			Movement:     MovementRules{EnergyCost: 1},
			Feeding:      FeedingRules{EnergyCost: 1},
			Reproduction: sameBirthRules(BirthRules{EnergyCost: 5, OffspringEnergy: 4, OffspringHealth: 1}),
		}),
		WithIntentGenerator(fixedIntents(Intents{
			Move:      []MoveIntent{{ActorID: 1, To: pos(2, 1)}},
			Feed:      []FeedIntent{{ActorID: 1, TargetID: 3, Amount: 6}, {ActorID: 2, TargetID: 4}},
			Reproduce: []ReproductionIntent{{ParentID: 1, To: pos(2, 2)}},
		})),
	)
	w.AddCreature(herbivore(1, pos(1, 1), 10))
	w.AddCreature(predator(2, pos(5, 5), 10))
	w.AddPlant(plant(3, pos(2, 1), 4))
	w.AddCreature(herbivore(4, pos(5, 6), 3))

	if err := w.Step(); err != nil {
		t.Fatalf("Step: %v", err)
	}

	// Herbivore 1: 10 - 1 move - 1 feed + 6 plant - 5 birth = 9, at (2,1).
	if got := mustCreature(t, w, 1); got.Position != pos(2, 1) || got.Energy != 9 {
		t.Fatalf("creature 1 = %+v, want at (2,1) with energy 9", got)
	}
	// Plant 3 grew 4 -> 6 before feeding, then was fully eaten and removed.
	if _, exists := w.Plant(3); exists {
		t.Fatalf("depleted plant 3 was not removed")
	}
	// Predator 2 ate herbivore 4: 10 - 1 + 3 = 12. Herbivore 4 was removed.
	if got := mustCreature(t, w, 2); got.Energy != 12 {
		t.Fatalf("predator energy = %d, want 12", got.Energy)
	}
	if _, exists := w.Creature(4); exists {
		t.Fatalf("eaten herbivore 4 was not removed")
	}
	// Offspring gets the next ID.
	if got := mustCreature(t, w, 5); got.Position != pos(2, 2) || got.Energy != 4 {
		t.Fatalf("offspring = %+v, want at (2,2) with energy 4", got)
	}
	if got := w.State(); got.Tick != 1 || got.NextEntity != 6 {
		t.Fatalf("tick %d NextEntity %d, want 1 and 6", got.Tick, got.NextEntity)
	}
}

func mustCreature(t *testing.T, w *World, id EntityID) Creature {
	t.Helper()
	c, ok := w.Creature(id)
	if !ok {
		t.Fatalf("creature %d missing", id)
	}
	return c
}

// --- Step: failure paths leave the committed state unchanged ---

func TestStepTickOverflow(t *testing.T) {
	w := NewWorld("test", stepConfig())
	w.AddCreature(herbivore(0, pos(1, 1), 10))
	w.state.Tick = math.MaxUint64

	err := assertStepFails(t, w, "tick overflow")
	if !errors.Is(err, ErrTickOverflow) {
		t.Fatalf("error = %v, want ErrTickOverflow", err)
	}
}

func TestStepEnvironmentFailureRollsBack(t *testing.T) {
	w := NewWorld("test", stepConfig())
	w.AddPlant(plant(0, pos(0, 0), 5))
	w.AddPlant(plant(0, pos(1, 0), math.MaxInt64))

	assertStepFails(t, w, "environment")
}

func TestStepIntentGenerationFailureRollsBack(t *testing.T) {
	w := NewWorld("test", stepConfig(), WithIntentGenerator(func(_ WorldConfig, s WorldState) (Intents, error) {
		// A misbehaving generator must not be able to touch committed state.
		delete(s.Creatures, 1)
		s.CreatureIDs[0] = 99
		return Intents{}, errors.New("boom")
	}))
	w.AddCreature(herbivore(0, pos(1, 1), 10))

	assertStepFails(t, w, "intent generation")
}

func TestStepMovementFailureRollsBack(t *testing.T) {
	w := NewWorld("test", stepConfig(), WithRules(Rules{Movement: MovementRules{EnergyCost: -1}}))
	w.AddCreature(herbivore(0, pos(1, 1), 10))
	w.AddPlant(plant(0, pos(2, 2), 5))

	assertStepFails(t, w, "movement")
}

func TestStepReproductionAllocationFailureRollsBack(t *testing.T) {
	w := NewWorld("test", stepConfig(),
		WithRules(Rules{
			Environment:  EnvironmentRules{PlantGrowth: 1},
			Movement:     MovementRules{EnergyCost: 1},
			Reproduction: defaultReproductionRules,
		}),
		WithIntentGenerator(fixedIntents(Intents{
			Move: []MoveIntent{{ActorID: 1, To: pos(1, 2)}},
			Reproduce: []ReproductionIntent{
				{ParentID: 1, To: pos(1, 3)},
				{ParentID: 2, To: pos(5, 6)},
			},
		})),
	)
	w.AddCreature(herbivore(1, pos(1, 1), 10))
	w.AddCreature(herbivore(2, pos(5, 5), 10))
	// Leaves NextEntity at MaxUint64-1: zero births fit, so two must fail.
	w.AddPlant(plant(math.MaxUint64-2, pos(8, 8), 5))

	assertStepFails(t, w, "reproduction")
}

func TestStepInvalidFinalStateRejected(t *testing.T) {
	w := NewWorld("test", stepConfig())
	w.AddCreature(herbivore(0, pos(1, 1), 10))
	// No phase checks the position of a creature that does not act, so only
	// commit validation can catch this.
	w.AddCreature(herbivore(0, pos(50, 50), 10))

	assertStepFails(t, w, "commit rejected")
}

// --- Step: determinism ---

// randomWalk is a deterministic generator that exercises every phase. Each
// decision draws from its own PRNG stream, exactly one draw per decision.
func randomWalk(config WorldConfig, s WorldState) (Intents, error) {
	var intents Intents
	plantAt := make(map[Position]EntityID, len(s.PlantIDs))
	for _, id := range s.PlantIDs {
		plantAt[s.Plants[id].Position] = id
	}

	for _, id := range s.CreatureIDs {
		c := s.Creatures[id]
		if !c.IsAlive() {
			continue
		}

		move := NewDecisionRandom(config.Seed, s.Tick, id, DecisionKindMovementIntent, 0)
		intents.Move = append(intents.Move, MoveIntent{ActorID: id, To: neighbor(c.Position, move.Uint32())})

		if plantID, ok := plantAt[c.Position]; ok {
			intents.Feed = append(intents.Feed, FeedIntent{ActorID: id, TargetID: plantID, Amount: 2})
		}

		birth := NewDecisionRandom(config.Seed, s.Tick, id, DecisionKindReproductionIntent, 0)
		to := neighbor(c.Position, birth.Uint32())
		if c.Energy > 15 {
			intents.Reproduce = append(intents.Reproduce, ReproductionIntent{ParentID: id, To: to})
		}
	}
	return intents, nil
}

// neighbor maps a random draw to one of the eight cells around p. Out-of-bounds
// results are left for the resolvers to reject.
func neighbor(p Position, draw uint32) Position {
	d := int32(draw % 8)
	if d >= 4 {
		d++ // skip the center cell
	}
	return Position{X: p.X + d%3 - 1, Y: p.Y + d/3 - 1}
}

func seededWorld(t *testing.T) *World {
	t.Helper()
	w := NewWorld("test", stepConfig(), WithIntentGenerator(randomWalk))
	for i := int32(0); i < 8; i++ {
		w.AddCreature(herbivore(0, pos(i, i), 20))
		w.AddPlant(plant(0, pos(9-i, i), 30))
	}
	return w
}

func TestStepSameSeedProducesIdenticalState(t *testing.T) {
	a, b := seededWorld(t), seededWorld(t)

	for tick := 0; tick < 50; tick++ {
		if err := a.Step(); err != nil {
			t.Fatalf("world a tick %d: %v", tick, err)
		}
		if err := b.Step(); err != nil {
			t.Fatalf("world b tick %d: %v", tick, err)
		}
		if !reflect.DeepEqual(a.State(), b.State()) {
			t.Fatalf("worlds diverged at tick %d", tick+1)
		}
	}

	// Guard against a vacuous pass: the run must have exercised the phases.
	final := a.State()
	if final.NextEntity <= 17 {
		t.Fatalf("no births in 50 ticks; generator is not exercising reproduction")
	}
}

func TestStepCompetingMovesSameWinnerEveryRun(t *testing.T) {
	moves := []MoveIntent{
		{ActorID: 3, To: pos(2, 2)},
		{ActorID: 1, To: pos(2, 2)},
		{ActorID: 2, To: pos(2, 2)},
	}
	rng := rand.New(rand.NewPCG(1, 2))

	for run := 0; run < 50; run++ {
		shuffled := append([]MoveIntent(nil), moves...)
		rng.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

		w := NewWorld("test", stepConfig(), WithIntentGenerator(fixedIntents(Intents{Move: shuffled})))
		w.AddCreature(herbivore(1, pos(1, 2), 10))
		w.AddCreature(herbivore(2, pos(3, 2), 10))
		w.AddCreature(herbivore(3, pos(2, 3), 10))

		if err := w.Step(); err != nil {
			t.Fatalf("Step: %v", err)
		}
		if got := mustCreature(t, w, 1).Position; got != pos(2, 2) {
			t.Fatalf("run %d order %v: creature 1 at %+v, want winner at (2,2)", run, shuffled, got)
		}
	}
}

// --- Step: concurrency ---

// growthWorld returns a world where every plant's energy is a known function
// of the tick, so any reader can check that tick and entities match.
func growthWorld() (*World, int64) {
	const base, growth = 100, 3
	w := NewWorld("test", stepConfig(), WithRules(Rules{
		Environment:  EnvironmentRules{PlantGrowth: growth},
		Reproduction: defaultReproductionRules,
	}))
	for i := int32(0); i < 10; i++ {
		w.AddPlant(plant(0, pos(i, 0), base))
	}
	return w, growth
}

func assertConsistent(t *testing.T, s WorldState, growth int64) {
	t.Helper()
	want := 100 + int64(s.Tick)*growth
	for _, id := range s.PlantIDs {
		if got := s.Plants[id].Energy; got != want {
			t.Errorf("tick %d: plant %d energy %d, want %d (torn read)", s.Tick, id, got, want)
			return
		}
	}
}

func TestStepConcurrentReadersSeeCompleteStates(t *testing.T) {
	w, growth := growthWorld()
	const ticks = 200

	var wg sync.WaitGroup
	done := make(chan struct{})
	for r := 0; r < 4; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var last Tick
			for {
				select {
				case <-done:
					return
				default:
				}
				s := w.State()
				if s.Tick < last {
					t.Errorf("tick went backwards: %d after %d", s.Tick, last)
					return
				}
				last = s.Tick
				assertConsistent(t, s, growth)
			}
		}()
	}

	for i := 0; i < ticks; i++ {
		if err := w.Step(); err != nil {
			t.Fatalf("Step: %v", err)
		}
	}
	close(done)
	wg.Wait()
}

func TestStepConcurrentCallsSerialize(t *testing.T) {
	w, growth := growthWorld()
	const workers, perWorker = 8, 25

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWorker; j++ {
				if err := w.Step(); err != nil {
					t.Errorf("Step: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	s := w.State()
	if s.Tick != workers*perWorker {
		t.Fatalf("Tick = %d, want %d", s.Tick, workers*perWorker)
	}
	assertConsistent(t, s, growth)
}
