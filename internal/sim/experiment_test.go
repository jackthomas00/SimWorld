package sim

import (
	"reflect"
	"sync"
	"testing"
)

func smallExperiment() Experiment {
	e := DefaultExperiment()
	e.Config.Width, e.Config.Height = 12, 12
	e.Config.RegionWidth, e.Config.RegionHeight = 12, 12
	e.Config.InitialPlants, e.Config.InitialHerbivores, e.Config.InitialPredators = 30, 10, 2
	e.Ticks = 40
	return e
}

func TestNewPopulatedWorldPlacesRequestedPopulations(t *testing.T) {
	config := smallExperiment().Config
	config.Seed = 3
	w, err := NewPopulatedWorld("test", config, InitialEnergy{Plant: 7, Herbivore: 8, Predator: 9})
	if err != nil {
		t.Fatalf("NewPopulatedWorld: %v", err)
	}
	state := w.State()

	if err := validateState(config, state); err != nil {
		t.Fatalf("populated world invalid: %v", err)
	}
	c := CensusOf(state)
	want := Census{Plants: 30, Herbivores: 10, Predators: 2, PlantEnergy: 210, HerbivoreEnergy: 80, PredatorEnergy: 18}
	if c != want {
		t.Fatalf("census = %+v, want %+v", c, want)
	}

	plantCells := make(map[Position]bool)
	for _, p := range state.Plants {
		if plantCells[p.Position] {
			t.Fatalf("two plants share cell %+v", p.Position)
		}
		plantCells[p.Position] = true
	}
	// IDs: herbivores, then predators, then plants.
	if state.Creatures[1].Kind != KindHerbivore || state.Creatures[11].Kind != KindPredator {
		t.Fatalf("creature IDs not assigned herbivores first")
	}
	if _, ok := state.Plants[13]; !ok {
		t.Fatalf("plant IDs do not follow creatures")
	}
}

func TestNewPopulatedWorldIsDeterministic(t *testing.T) {
	config := smallExperiment().Config
	energy := InitialEnergy{Plant: 7, Herbivore: 8, Predator: 9}

	a, _ := NewPopulatedWorld("a", config, energy)
	b, _ := NewPopulatedWorld("b", config, energy)
	if !reflect.DeepEqual(a.State(), b.State()) {
		t.Fatalf("same config produced different placements")
	}

	config.Seed++
	c, _ := NewPopulatedWorld("c", config, energy)
	if reflect.DeepEqual(a.State(), c.State()) {
		t.Fatalf("different seeds produced identical placements")
	}
}

func TestNewPopulatedWorldRejectsInvalidSetups(t *testing.T) {
	energy := InitialEnergy{Plant: 1, Herbivore: 1, Predator: 1}
	for name, mutate := range map[string]func(*WorldConfig, *InitialEnergy){
		"too many creatures": func(c *WorldConfig, _ *InitialEnergy) { c.InitialHerbivores = 140; c.InitialPredators = 5 },
		"too many plants":    func(c *WorldConfig, _ *InitialEnergy) { c.InitialPlants = 145 },
		"negative count":     func(c *WorldConfig, _ *InitialEnergy) { c.InitialPredators = -1 },
		"zero energy":        func(_ *WorldConfig, e *InitialEnergy) { e.Herbivore = 0 },
		"bad dimensions":     func(c *WorldConfig, _ *InitialEnergy) { c.Width = 0 },
	} {
		config, e := smallExperiment().Config, energy
		mutate(&config, &e)
		if _, err := NewPopulatedWorld("test", config, e); err == nil {
			t.Fatalf("%s: expected error", name)
		}
	}
}

func TestExperimentRunIsDeterministicAndSampled(t *testing.T) {
	e := smallExperiment()
	e.SampleEvery = 10

	a, err := e.Run(5)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	b, _ := e.Run(5)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("same seed produced different trials")
	}

	var ticks []Tick
	for _, s := range a.Samples {
		ticks = append(ticks, s.Tick)
	}
	if want := []Tick{0, 10, 20, 30, 40}; !reflect.DeepEqual(ticks, want) {
		t.Fatalf("sample ticks = %v, want %v", ticks, want)
	}
	if a.Final != a.Samples[len(a.Samples)-1] {
		t.Fatalf("final census does not match last sample")
	}
}

func TestExperimentRecordsExtinction(t *testing.T) {
	e := smallExperiment()
	e.Config.InitialHerbivores = 0
	e.Initial.Predator = 25
	e.Rules.Metabolism.Predator = 20 // 25 -> 5 at tick 1, starved at tick 2

	trial, err := e.Run(1)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if trial.PredatorsExtinctAt != 2 {
		t.Fatalf("PredatorsExtinctAt = %d, want 2", trial.PredatorsExtinctAt)
	}
	if trial.HerbivoresExtinctAt != 0 {
		t.Fatalf("herbivores that never existed marked extinct at %d", trial.HerbivoresExtinctAt)
	}
	if trial.Coexisted() {
		t.Fatalf("trial with an extinction reported coexistence")
	}
}

// TestDefaultEcosystemCoexists guards the tuned defaults. These seeds
// coexisted when the defaults were chosen (docs/ecosystem-tuning.md); because
// runs are deterministic, a failure here means a rule or behavior change
// altered the dynamics, not flakiness. Re-tune or re-pick seeds deliberately.
func TestDefaultEcosystemCoexists(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: runs four 80x80 worlds for 2000 ticks")
	}
	e := DefaultExperiment()
	seeds := []uint64{1, 2, 3, 4}

	trials := make([]Trial, len(seeds))
	errs := make([]error, len(seeds))
	var wg sync.WaitGroup
	for i, seed := range seeds {
		wg.Add(1)
		go func() {
			defer wg.Done()
			trials[i], errs[i] = e.Run(seed)
		}()
	}
	wg.Wait()

	for i, trial := range trials {
		if errs[i] != nil {
			t.Fatalf("seed %d: %v", seeds[i], errs[i])
		}
		if !trial.Coexisted() {
			t.Errorf("seed %d did not coexist: herbivores extinct @%d, predators extinct @%d, plants extinct @%d",
				trial.Seed, trial.HerbivoresExtinctAt, trial.PredatorsExtinctAt, trial.PlantsExtinctAt)
		}
	}
}

func TestNewPopulatedWorldStaggersStartingAges(t *testing.T) {
	config := smallExperiment().Config
	energy := InitialEnergy{Plant: 7, Herbivore: 8, Predator: 9}
	rules := DefaultRules()
	rules.Aging = AgingRules{Herbivore: 50, Predator: 0, Plant: 30}

	w, err := NewPopulatedWorld("test", config, energy, WithRules(rules))
	if err != nil {
		t.Fatalf("NewPopulatedWorld: %v", err)
	}

	herbivoreAges := make(map[uint32]bool)
	for _, c := range w.CreatureList() {
		switch c.Kind {
		case KindHerbivore:
			if c.Age >= 50 {
				t.Fatalf("herbivore age %d not below lifespan 50", c.Age)
			}
			herbivoreAges[c.Age] = true
		case KindPredator:
			if c.Age != 0 {
				t.Fatalf("predator with no lifespan has starting age %d", c.Age)
			}
		}
	}
	if len(herbivoreAges) < 2 {
		t.Fatalf("herbivore starting ages not staggered: %v", herbivoreAges)
	}
	for _, p := range w.PlantList() {
		if p.Age >= 30 {
			t.Fatalf("plant age %d not below lifespan 30", p.Age)
		}
	}
}
