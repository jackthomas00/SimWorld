package sim

import "fmt"

func validateState(
	config WorldConfig,
	state WorldState,
) error {
	if config.Width <= 0 {
		return fmt.Errorf("world width must be positive: %d", config.Width)
	}
	if config.Height <= 0 {
		return fmt.Errorf("world height must be positive: %d", config.Height)
	}
	if config.RegionWidth <= 0 {
		return fmt.Errorf("region width must be positive: %d", config.RegionWidth)
	}
	if config.RegionHeight <= 0 {
		return fmt.Errorf("region height must be positive: %d", config.RegionHeight)
	}
	if config.RegionWidth > config.Width {
		return fmt.Errorf("region width %d exceeds world width %d", config.RegionWidth, config.Width)
	}
	if config.RegionHeight > config.Height {
		return fmt.Errorf("region height %d exceeds world height %d", config.RegionHeight, config.Height)
	}

	seen := make(map[EntityID]string, len(state.Creatures)+len(state.Plants))
	creaturePositions := make(map[Position]EntityID, len(state.Creatures))

	if err := validateCreatures(config, state, seen, creaturePositions); err != nil {
		return err
	}
	if err := validatePlants(config, state, seen); err != nil {
		return err
	}
	if err := validateNextEntity(state.NextEntity, seen); err != nil {
		return err
	}

	return nil
}

func validateCreatures(
	config WorldConfig,
	state WorldState,
	seen map[EntityID]string,
	creaturePositions map[Position]EntityID,
) error {
	creatureIDs := make(map[EntityID]struct{}, len(state.CreatureIDs))
	for index, id := range state.CreatureIDs {
		if id == 0 {
			return fmt.Errorf("creature ID slice contains zero ID at index %d", index)
		}
		if _, duplicate := creatureIDs[id]; duplicate {
			return fmt.Errorf("creature ID slice contains duplicate ID %d", id)
		}
		if _, exists := state.Creatures[id]; !exists {
			return fmt.Errorf("creature ID slice contains ID %d missing from creature map", id)
		}
		creatureIDs[id] = struct{}{}
	}

	for id, creature := range state.Creatures {
		if id == 0 {
			return fmt.Errorf("creature map contains zero ID")
		}
		if creature.ID != id {
			return fmt.Errorf("creature map key %d does not match creature ID %d", id, creature.ID)
		}
		if _, exists := creatureIDs[id]; !exists {
			return fmt.Errorf("creature map contains ID %d missing from creature ID slice", id)
		}
		if owner, duplicate := seen[id]; duplicate {
			return fmt.Errorf("creature ID %d duplicates %s", id, owner)
		}
		seen[id] = "existing entity ID"

		if !creature.Position.InBounds(config.Width, config.Height) {
			return fmt.Errorf("creature %d position (%d,%d) is outside world bounds %dx%d", id, creature.Position.X, creature.Position.Y, config.Width, config.Height)
		}
		if err := validateEnergyHealth("creature", id, creature.Energy, creature.Health); err != nil {
			return err
		}
		if creature.IsAlive() {
			if occupant, occupied := creaturePositions[creature.Position]; occupied {
				return fmt.Errorf("living creatures %d and %d occupy position (%d,%d)", occupant, id, creature.Position.X, creature.Position.Y)
			}
			creaturePositions[creature.Position] = id
		}
	}

	return nil
}

func validatePlants(config WorldConfig, state WorldState, seen map[EntityID]string) error {
	plantIDs := make(map[EntityID]struct{}, len(state.PlantIDs))
	for index, id := range state.PlantIDs {
		if id == 0 {
			return fmt.Errorf("plant ID slice contains zero ID at index %d", index)
		}
		if _, duplicate := plantIDs[id]; duplicate {
			return fmt.Errorf("plant ID slice contains duplicate ID %d", id)
		}
		if _, exists := state.Plants[id]; !exists {
			return fmt.Errorf("plant ID slice contains ID %d missing from plant map", id)
		}
		plantIDs[id] = struct{}{}
	}

	for id, plant := range state.Plants {
		if id == 0 {
			return fmt.Errorf("plant map contains zero ID")
		}
		if plant.ID != id {
			return fmt.Errorf("plant map key %d does not match plant ID %d", id, plant.ID)
		}
		if _, exists := plantIDs[id]; !exists {
			return fmt.Errorf("plant map contains ID %d missing from plant ID slice", id)
		}
		if owner, duplicate := seen[id]; duplicate {
			return fmt.Errorf("plant ID %d duplicates %s", id, owner)
		}
		seen[id] = "existing entity ID"

		if !plant.Position.InBounds(config.Width, config.Height) {
			return fmt.Errorf("plant %d position (%d,%d) is outside world bounds %dx%d", id, plant.Position.X, plant.Position.Y, config.Width, config.Height)
		}
		if err := validateEnergyHealth("plant", id, plant.Energy, plant.Health); err != nil {
			return err
		}
	}

	return nil
}

func validateEnergyHealth(entityType string, id EntityID, energy int64, health int32) error {
	if energy < 0 {
		return fmt.Errorf("%s %d has negative energy %d", entityType, id, energy)
	}
	if health < 0 {
		return fmt.Errorf("%s %d has negative health %d", entityType, id, health)
	}
	if health == 0 && energy != 0 {
		return fmt.Errorf("%s %d has zero health with nonzero energy %d", entityType, id, energy)
	}
	if health > 0 && energy == 0 {
		return fmt.Errorf("%s %d has positive health with zero energy", entityType, id)
	}

	return nil
}

func validateNextEntity(next EntityID, seen map[EntityID]string) error {
	if next == 0 {
		return fmt.Errorf("NextEntity must be nonzero")
	}
	if next == EntityID(^uint64(0)) {
		return fmt.Errorf("NextEntity %d cannot be allocated without overflow", next)
	}
	if owner, exists := seen[next]; exists {
		return fmt.Errorf("NextEntity %d collides with %s", next, owner)
	}

	return nil
}
