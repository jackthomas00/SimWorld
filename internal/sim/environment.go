package sim

import (
	"fmt"
	"math"
)

// applyEnvironment runs the environment phase on the working state: plants
// grow, then mature plants seed and empty cells may sprout. New plants are visible to later phases of the
// same tick but not to intent generation, which reads the snapshot.
func applyEnvironment(config WorldConfig, state *WorldState, rules EnvironmentRules) error {
	if err := validateEnvironmentRules(rules); err != nil {
		return err
	}
	if err := growPlants(state, rules); err != nil {
		return err
	}
	return placePlants(config, state, rules)
}

func validateEnvironmentRules(rules EnvironmentRules) error {
	if rules.PlantGrowth < 0 {
		return fmt.Errorf("plant growth must be non-negative: %d", rules.PlantGrowth)
	}
	if rules.SeedThreshold < 0 {
		return fmt.Errorf("seed threshold must be non-negative: %d", rules.SeedThreshold)
	}
	if rules.SproutAttempts < 0 || rules.SproutAttempts > MaxSproutAttempts {
		return fmt.Errorf("sprout attempts must be between 0 and %d: %d", MaxSproutAttempts, rules.SproutAttempts)
	}
	if rules.SproutAttempts > 0 && rules.SproutEnergy <= 0 {
		return fmt.Errorf("sprout energy must be positive: %d", rules.SproutEnergy)
	}
	if rules.SeedThreshold == 0 {
		return nil
	}
	if rules.SeedCost < 0 {
		return fmt.Errorf("seed cost must be non-negative: %d", rules.SeedCost)
	}
	if rules.SeedThreshold <= rules.SeedCost {
		return fmt.Errorf("seed threshold %d must exceed seed cost %d", rules.SeedThreshold, rules.SeedCost)
	}
	if rules.SeedlingEnergy <= 0 {
		return fmt.Errorf("seedling energy must be positive: %d", rules.SeedlingEnergy)
	}
	return nil
}

// growPlants adds PlantGrowth to every living plant in PlantIDs order.
func growPlants(state *WorldState, rules EnvironmentRules) error {
	if rules.PlantGrowth == 0 {
		return nil
	}

	for _, id := range state.PlantIDs {
		plant, exists := state.Plants[id]
		if !exists {
			return fmt.Errorf("plant ID slice contains ID %d missing from plant map", id)
		}
		if !plant.IsAlive() {
			continue
		}
		if plant.Energy > math.MaxInt64-rules.PlantGrowth {
			return fmt.Errorf("growing plant %d would overflow its energy", id)
		}
		if err := plant.Grow(rules.PlantGrowth); err != nil {
			return fmt.Errorf("grow plant %d: %w", id, err)
		}
		state.Plants[id] = plant
	}
	return nil
}

// newPlant is a plant chosen during selection, before it has an ID. parent is
// zero for a sprout.
type newPlant struct {
	parent EntityID
	to     Position
	energy int64
}

// placePlants creates seedlings and sprouts using reproduction's two passes.
// Pass 1 selects cells without allocating IDs: first seedlings, in ascending
// parent ID order, then sprouts, in attempt order. Each selected cell is
// claimed, so later candidates cannot reuse it. Pass 2 proves every new plant
// can be allocated an ID, then charges parents and creates plants in
// selection order.
func placePlants(config WorldConfig, state *WorldState, rules EnvironmentRules) error {
	if rules.SeedThreshold == 0 && rules.SproutAttempts == 0 {
		return nil
	}

	occupied := make(map[Position]struct{}, len(state.PlantIDs))
	for _, id := range state.PlantIDs {
		if plant := state.Plants[id]; plant.IsAlive() {
			occupied[plant.Position] = struct{}{}
		}
	}

	// Pass 1: selection.
	selected := selectSeedlings(config, state, rules, occupied)
	selected = append(selected, selectSprouts(config, state, rules, occupied)...)

	// Pass 2: allocation and application.
	if err := checkIDAllocation(state, len(selected)); err != nil {
		return fmt.Errorf("plant placement: %w", err)
	}
	for _, n := range selected {
		if n.parent != 0 {
			parent := state.Plants[n.parent]
			parent.Energy -= rules.SeedCost
			parent.Version++
			state.Plants[n.parent] = parent
		}

		id := state.NextEntity
		state.NextEntity++
		state.Plants[id] = Plant{ID: id, Position: n.to, Energy: n.energy, Health: 1}
		state.PlantIDs = append(state.PlantIDs, id)
	}
	return nil
}

// selectSeedlings picks a free neighboring cell for each plant at or above
// SeedThreshold, in ascending plant ID order.
func selectSeedlings(
	config WorldConfig,
	state *WorldState,
	rules EnvironmentRules,
	occupied map[Position]struct{},
) []newPlant {
	if rules.SeedThreshold == 0 {
		return nil
	}

	var selected []newPlant
	for _, id := range sortedIDs(state.PlantIDs) {
		plant := state.Plants[id]
		if !plant.IsAlive() || plant.Energy < rules.SeedThreshold {
			continue
		}

		// Every eligible plant draws exactly once, whether or not a cell is free.
		rng := NewDecisionRandom(config.Seed, state.Tick, id, DecisionKindPlantSeeding, 0)
		draw := rng.Uint32()

		var free []Position
		for _, o := range neighborOffsets {
			p := Position{X: plant.Position.X + o[0], Y: plant.Position.Y + o[1]}
			if _, taken := occupied[p]; !taken && p.InBounds(config.Width, config.Height) {
				free = append(free, p)
			}
		}
		to, ok := pick(free, draw)
		if !ok {
			continue
		}
		occupied[to] = struct{}{}
		selected = append(selected, newPlant{parent: id, to: to, energy: rules.SeedlingEnergy})
	}
	return selected
}

// selectSprouts tries SproutAttempts uniformly random cells. A cell that
// already holds a living plant, including one selected earlier this tick, is
// skipped. All attempts share one stream owned by the world (actor ID 0), and
// each attempt draws exactly two values, X then Y, whether or not it succeeds.
func selectSprouts(
	config WorldConfig,
	state *WorldState,
	rules EnvironmentRules,
	occupied map[Position]struct{},
) []newPlant {
	if rules.SproutAttempts == 0 {
		return nil
	}

	var selected []newPlant
	rng := NewDecisionRandom(config.Seed, state.Tick, 0, DecisionKindPlantSprouting, 0)
	for range rules.SproutAttempts {
		x := rng.Uint32() % uint32(config.Width)
		y := rng.Uint32() % uint32(config.Height)
		to := Position{X: int32(x), Y: int32(y)}
		if _, taken := occupied[to]; taken {
			continue
		}
		occupied[to] = struct{}{}
		selected = append(selected, newPlant{to: to, energy: rules.SproutEnergy})
	}
	return selected
}

// applyMetabolism charges each living creature its kind's upkeep, in
// CreatureIDs order.
func applyMetabolism(state *WorldState, rules MetabolismRules) error {
	if rules.Herbivore < 0 || rules.Predator < 0 {
		return fmt.Errorf("metabolism must be non-negative: herbivore %d, predator %d", rules.Herbivore, rules.Predator)
	}

	for _, id := range state.CreatureIDs {
		creature, exists := state.Creatures[id]
		if !exists {
			return fmt.Errorf("creature ID slice contains ID %d missing from creature map", id)
		}
		if !creature.IsAlive() {
			continue
		}
		var upkeep int64
		switch creature.Kind {
		case KindHerbivore:
			upkeep = rules.Herbivore
		case KindPredator:
			upkeep = rules.Predator
		}
		if upkeep == 0 {
			continue
		}
		if err := creature.SpendEnergy(upkeep); err != nil {
			return fmt.Errorf("charge metabolism to creature %d: %w", id, err)
		}
		state.Creatures[id] = creature
	}
	return nil
}

// applyAging ages every living entity by one tick, plants in PlantIDs order
// and then creatures in CreatureIDs order, and kills those that reach their
// kind's lifespan. Aging changes committed state, so each aged entity's
// Version increments.
func applyAging(state *WorldState, rules AgingRules) error {
	for _, id := range state.PlantIDs {
		plant, exists := state.Plants[id]
		if !exists {
			return fmt.Errorf("plant ID slice contains ID %d missing from plant map", id)
		}
		if !plant.IsAlive() {
			continue
		}
		plant.Age = nextAge(plant.Age)
		plant.Version++
		if reachedLifespan(plant.Age, rules.Plant) {
			plant.Kill()
		}
		state.Plants[id] = plant
	}

	for _, id := range state.CreatureIDs {
		creature, exists := state.Creatures[id]
		if !exists {
			return fmt.Errorf("creature ID slice contains ID %d missing from creature map", id)
		}
		if !creature.IsAlive() {
			continue
		}
		var lifespan uint32
		switch creature.Kind {
		case KindHerbivore:
			lifespan = rules.Herbivore
		case KindPredator:
			lifespan = rules.Predator
		}
		creature.Age = nextAge(creature.Age)
		creature.Version++
		if reachedLifespan(creature.Age, lifespan) {
			creature.Kill()
		}
		state.Creatures[id] = creature
	}
	return nil
}

func nextAge(age uint32) uint32 {
	if age == math.MaxUint32 {
		return age
	}
	return age + 1
}

func reachedLifespan(age, lifespan uint32) bool {
	return lifespan > 0 && age >= lifespan
}
