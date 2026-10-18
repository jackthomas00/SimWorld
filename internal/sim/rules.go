package sim

// Rules configures every phase of a tick.
type Rules struct {
	Environment  EnvironmentRules
	Aging        AgingRules
	Metabolism   MetabolismRules
	Movement     MovementRules
	Feeding      FeedingRules
	Reproduction ReproductionRules
}

// EnvironmentRules configures the environment phase.
type EnvironmentRules struct {
	// PlantGrowth is added to every living plant each tick. It must be
	// non-negative. Depleted plants do not regrow.
	PlantGrowth int64

	// SeedThreshold enables seeding when positive: after growth, every living
	// plant with at least this much energy places one seedling on a
	// neighboring cell with no living plant. It must exceed SeedCost, so
	// seeding never kills the parent.
	SeedThreshold int64
	// SeedCost is charged to the parent for each seedling. It must be
	// non-negative.
	SeedCost int64
	// SeedlingEnergy is a new plant's starting energy. It must be positive
	// when seeding is enabled.
	SeedlingEnergy int64

	// SproutAttempts enables spontaneous sprouting when positive: each tick,
	// this many cells are chosen uniformly at random, and each chosen cell
	// with no living plant grows a new plant with SproutEnergy energy. It
	// models a seed bank, so plants cannot go permanently extinct. It must be
	// at most MaxSproutAttempts.
	SproutAttempts int32
	SproutEnergy   int64
}

// AgingRules sets each kind's lifespan in ticks; zero means no limit. At the
// start of the environment phase every living plant and creature ages by one
// tick, and one whose age reaches its kind's lifespan dies. Age saturates at
// the maximum uint32 value rather than overflowing.
type AgingRules struct {
	Plant     uint32
	Herbivore uint32
	Predator  uint32
}

// MetabolismRules sets the energy each living creature spends every tick just
// to stay alive, whether or not it acts. It is charged in the environment
// phase, after plants grow. A creature whose energy reaches zero dies and
// cannot act later in the tick. Both values must be non-negative.
type MetabolismRules struct {
	Herbivore int64
	Predator  int64
}

// MaxSproutAttempts bounds the per-tick work spent on sprouting.
const MaxSproutAttempts = 1 << 16

// DefaultRules returns the rules a world uses unless WithRules overrides them.
// Together with DefaultBehaviorRules they were tuned with DefaultExperiment;
// see docs/ecosystem-tuning.md.
func DefaultRules() Rules {
	return Rules{
		Environment: EnvironmentRules{
			PlantGrowth:    1,
			SeedThreshold:  20,
			SeedCost:       10,
			SeedlingEnergy: 5,
			SproutAttempts: 10,
			SproutEnergy:   5,
		},
		Aging:      AgingRules{Herbivore: 400, Predator: 800},
		Metabolism: MetabolismRules{Herbivore: 0, Predator: 1},
		Movement:   MovementRules{EnergyCost: 1},
		Feeding:    FeedingRules{EnergyCost: 0},
		Reproduction: ReproductionRules{
			Herbivore: BirthRules{EnergyCost: 10, OffspringEnergy: 5, OffspringHealth: 1},
			Predator:  BirthRules{EnergyCost: 50, OffspringEnergy: 25, OffspringHealth: 1},
		},
	}
}
