package sim

import "fmt"

// InitialEnergy sets the starting energy of each kind placed by
// NewPopulatedWorld. Every value used must be positive.
type InitialEnergy struct {
	Plant     int64
	Herbivore int64
	Predator  int64
}

// NewPopulatedWorld creates a world holding config.InitialHerbivores,
// config.InitialPredators and config.InitialPlants entities at random cells.
// Creatures occupy distinct cells, as do plants; a plant may share a cell
// with a creature. IDs go to herbivores, then predators, then plants.
//
// Placement is a deterministic function of config. Creature cells come from a
// Fisher-Yates shuffle of every cell, drawn from the stream (seed, tick 0,
// actor 0, DecisionKindInitialPlacement, ordinal 0); plant cells come from a
// second shuffle using ordinal 1. Each shuffle draws exactly cells-1 values.
//
// Starting ages are staggered so the founders do not all reach their lifespan
// on the same tick. Each entity, in ID order, draws one value from ordinal 2;
// its age is that value modulo its kind's lifespan from the world's
// AgingRules, or zero when the kind has no lifespan.
func NewPopulatedWorld(id string, config WorldConfig, energy InitialEnergy, opts ...WorldOption) (*World, error) {
	w := NewWorld(id, config, opts...)
	if err := validateState(config, w.State()); err != nil {
		return nil, err
	}

	cells := int64(config.Width) * int64(config.Height)
	creatures := int64(config.InitialHerbivores) + int64(config.InitialPredators)
	switch {
	case config.InitialHerbivores < 0 || config.InitialPredators < 0 || config.InitialPlants < 0:
		return nil, fmt.Errorf("initial populations must be non-negative: %d plants, %d herbivores, %d predators",
			config.InitialPlants, config.InitialHerbivores, config.InitialPredators)
	case creatures > cells:
		return nil, fmt.Errorf("%d creatures do not fit in %d cells", creatures, cells)
	case int64(config.InitialPlants) > cells:
		return nil, fmt.Errorf("%d plants do not fit in %d cells", config.InitialPlants, cells)
	}
	for _, e := range []struct {
		kind   string
		count  int
		energy int64
	}{
		{"plant", config.InitialPlants, energy.Plant},
		{"herbivore", config.InitialHerbivores, energy.Herbivore},
		{"predator", config.InitialPredators, energy.Predator},
	} {
		if e.count > 0 && e.energy <= 0 {
			return nil, fmt.Errorf("initial %s energy must be positive: %d", e.kind, e.energy)
		}
	}

	lifespans := w.rules.Aging
	ages := NewDecisionRandom(config.Seed, 0, 0, DecisionKindInitialPlacement, 2)
	startAge := func(lifespan uint32) uint32 {
		draw := ages.Uint32()
		if lifespan == 0 {
			return 0
		}
		return draw % lifespan
	}

	creatureCells := shuffledCells(config, 0)
	for i := range config.InitialHerbivores {
		w.AddCreature(Creature{
			Kind: KindHerbivore, Position: creatureCells[i],
			Energy: energy.Herbivore, Health: 1, Age: startAge(lifespans.Herbivore),
		})
	}
	for i := range config.InitialPredators {
		w.AddCreature(Creature{
			Kind: KindPredator, Position: creatureCells[config.InitialHerbivores+i],
			Energy: energy.Predator, Health: 1, Age: startAge(lifespans.Predator),
		})
	}
	plantCells := shuffledCells(config, 1)
	for i := range config.InitialPlants {
		w.AddPlant(Plant{
			Position: plantCells[i],
			Energy:   energy.Plant, Health: 1, Age: startAge(lifespans.Plant),
		})
	}
	return w, nil
}

func shuffledCells(config WorldConfig, ordinal uint64) []Position {
	cells := make([]Position, 0, int(config.Width)*int(config.Height))
	for y := range config.Height {
		for x := range config.Width {
			cells = append(cells, Position{X: x, Y: y})
		}
	}
	rng := NewDecisionRandom(config.Seed, 0, 0, DecisionKindInitialPlacement, ordinal)
	for i := len(cells) - 1; i > 0; i-- {
		j := int(rng.Uint32() % uint32(i+1))
		cells[i], cells[j] = cells[j], cells[i]
	}
	return cells
}

// Census summarizes one committed state.
type Census struct {
	Tick            Tick
	Plants          int
	Herbivores      int
	Predators       int
	PlantEnergy     int64
	HerbivoreEnergy int64
	PredatorEnergy  int64
}

func CensusOf(state WorldState) Census {
	c := Census{Tick: state.Tick}
	for _, id := range state.PlantIDs {
		if p := state.Plants[id]; p.IsAlive() {
			c.Plants++
			c.PlantEnergy += p.Energy
		}
	}
	for _, id := range state.CreatureIDs {
		creature := state.Creatures[id]
		if !creature.IsAlive() {
			continue
		}
		switch creature.Kind {
		case KindHerbivore:
			c.Herbivores++
			c.HerbivoreEnergy += creature.Energy
		case KindPredator:
			c.Predators++
			c.PredatorEnergy += creature.Energy
		}
	}
	return c
}

// Experiment describes a population run, repeated across seeds.
type Experiment struct {
	// Config supplies the dimensions and initial populations. Its Seed is
	// replaced by each trial's seed.
	Config   WorldConfig
	Rules    Rules
	Behavior BehaviorRules
	Initial  InitialEnergy
	Ticks    int
	// SampleEvery records a census every this many ticks. Zero records only
	// the first and final states.
	SampleEvery int
}

// Trial is the outcome of one seeded run.
type Trial struct {
	Seed    uint64
	Samples []Census
	Final   Census
	// Extinction ticks are the first tick a population that started above
	// zero reached zero, or zero if it never did.
	PlantsExtinctAt     Tick
	HerbivoresExtinctAt Tick
	PredatorsExtinctAt  Tick
}

// Coexisted reports whether every population that started above zero
// survived the whole run.
func (t Trial) Coexisted() bool {
	return t.PlantsExtinctAt == 0 && t.HerbivoresExtinctAt == 0 && t.PredatorsExtinctAt == 0
}

// DefaultExperiment is the setup DefaultRules and DefaultBehaviorRules were
// tuned against: an 80x80 world run for 2000 ticks.
func DefaultExperiment() Experiment {
	return Experiment{
		Config: WorldConfig{
			Width: 80, Height: 80, RegionWidth: 80, RegionHeight: 80,
			InitialPlants: 1200, InitialHerbivores: 320, InitialPredators: 48,
		},
		Rules:    DefaultRules(),
		Behavior: DefaultBehaviorRules(),
		Initial:  InitialEnergy{Plant: 10, Herbivore: 15, Predator: 60},
		Ticks:    2000,
	}
}

func (e Experiment) Run(seed uint64) (Trial, error) {
	if err := e.Behavior.Validate(); err != nil {
		return Trial{}, err
	}
	config := e.Config
	config.Seed = seed
	w, err := NewPopulatedWorld("experiment", config, e.Initial, WithRules(e.Rules), WithBehavior(e.Behavior))
	if err != nil {
		return Trial{}, err
	}

	trial := Trial{Seed: seed}
	census := CensusOf(w.State())
	start := census
	trial.Samples = append(trial.Samples, census)
	for tick := 1; tick <= e.Ticks; tick++ {
		if err := w.Step(); err != nil {
			return trial, fmt.Errorf("seed %d tick %d: %w", seed, tick, err)
		}
		census = CensusOf(w.State())
		markExtinct(&trial.PlantsExtinctAt, start.Plants, census.Plants, census.Tick)
		markExtinct(&trial.HerbivoresExtinctAt, start.Herbivores, census.Herbivores, census.Tick)
		markExtinct(&trial.PredatorsExtinctAt, start.Predators, census.Predators, census.Tick)
		if e.SampleEvery > 0 && tick%e.SampleEvery == 0 && tick != e.Ticks {
			trial.Samples = append(trial.Samples, census)
		}
	}
	if e.Ticks > 0 {
		trial.Samples = append(trial.Samples, census)
	}
	trial.Final = census
	return trial, nil
}

func (e Experiment) RunSeeds(seeds ...uint64) ([]Trial, error) {
	trials := make([]Trial, 0, len(seeds))
	for _, seed := range seeds {
		trial, err := e.Run(seed)
		if err != nil {
			return trials, err
		}
		trials = append(trials, trial)
	}
	return trials, nil
}

func markExtinct(at *Tick, start, now int, tick Tick) {
	if *at == 0 && start > 0 && now == 0 {
		*at = tick
	}
}
