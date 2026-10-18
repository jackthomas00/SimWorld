package sim

import "testing"

func TestConsumeFood(t *testing.T) {
	creature := Creature{
		ID:     1,
		Energy: 10,
	}

	err := creature.ConsumeFood(5)
	if err != nil {
		t.Fatalf("ConsumeFood returned an error: %v", err)
	}

	if creature.Energy != 15 {
		t.Fatalf("expected energy 15, got %d", creature.Energy)
	}
}

func TestConsumeInvalidFood(t *testing.T) {
	creature := Creature{ID: 1, Energy: 10}

	err := creature.ConsumeFood(0)
	if err == nil {
		t.Fatal("expected an error for zero food")
	}

	if creature.Energy != 10 {
		t.Fatalf("energy changed after invalid action: %d", creature.Energy)
	}
}

// func TestMoveCreatureSpendsEnergy(t *testing.T) {
// 	creature := Creature{
// 		ID:       1,
// 		Position: Position{X: 1, Y: 1},
// 		Energy:   10,
// 		Health:   1,
// 	}

// 	err := creature.MoveBy(2, -1, 3)
// 	if err != nil {
// 		t.Fatalf("MoveBy returned an error: %v", err)
// 	}

// 	if creature.Position != (Position{X: 3, Y: 0}) {
// 		t.Fatalf("expected position (3,0), got (%d,%d)", creature.Position.X, creature.Position.Y)
// 	}
// 	if creature.Energy != 7 {
// 		t.Fatalf("expected energy 7, got %d", creature.Energy)
// 	}
// }

func TestWorldStoresCreaturesAndPlantsInMapsAndSlices(t *testing.T) {
	world := NewWorld("test", WorldConfig{Width: 10, Height: 10})

	creatureID := world.AddCreature(Creature{
		Kind:     KindHerbivore,
		Position: Position{X: 1, Y: 1},
		Energy:   10,
		Health:   1,
	})
	plantID := world.AddPlant(Plant{
		Position: Position{X: 2, Y: 2},
		Energy:   5,
		Health:   1,
	})

	state := world.State()
	if _, exists := state.Creatures[creatureID]; !exists {
		t.Fatalf("creature %d missing from map", creatureID)
	}
	if _, exists := state.Plants[plantID]; !exists {
		t.Fatalf("plant %d missing from map", plantID)
	}
	if len(state.CreatureIDs) != 1 || state.CreatureIDs[0] != creatureID {
		t.Fatalf("creature ID slice not updated: %v", state.CreatureIDs)
	}
	if len(state.PlantIDs) != 1 || state.PlantIDs[0] != plantID {
		t.Fatalf("plant ID slice not updated: %v", state.PlantIDs)
	}
}

// func TestWorldMoveCreatureRejectsOutOfBoundsMove(t *testing.T) {
// 	world := NewWorld("test", WorldConfig{Width: 3, Height: 3})
// 	creatureID := world.AddCreature(Creature{
// 		Position: Position{X: 1, Y: 1},
// 		Energy:   10,
// 		Health:   1,
// 	})

// 	err := world.MoveCreature(creatureID, Position{X: 3, Y: 1}, 1)
// 	if err != ErrOutOfBounds {
// 		t.Fatalf("expected ErrOutOfBounds, got %v", err)
// 	}

// 	creature, exists := world.Creature(creatureID)
// 	if !exists {
// 		t.Fatalf("creature %d missing from world", creatureID)
// 	}
// 	if creature.Position != (Position{X: 1, Y: 1}) {
// 		t.Fatalf("creature moved after invalid action: %+v", creature.Position)
// 	}
// }

func TestWorldStateReturnsCopy(t *testing.T) {
	world := NewWorld("test", WorldConfig{Width: 3, Height: 3})
	creatureID := world.AddCreature(Creature{
		Position: Position{X: 1, Y: 1},
		Energy:   10,
		Health:   1,
	})

	state := world.State()
	state.CreatureIDs[0] = 99
	delete(state.Creatures, creatureID)

	fresh := world.State()
	if fresh.CreatureIDs[0] != creatureID {
		t.Fatalf("snapshot slice mutation changed world state: %v", fresh.CreatureIDs)
	}
	if _, exists := fresh.Creatures[creatureID]; !exists {
		t.Fatalf("snapshot map mutation removed creature %d", creatureID)
	}
}

func TestWorldConfigIsImmutableAfterConstruction(t *testing.T) {
	config := WorldConfig{Width: 3, Height: 3, Seed: 7}
	world := NewWorld("test", config)

	config.Width = 99
	got := world.Config()
	if got.Width != 3 {
		t.Fatalf("source config mutation changed world config: %+v", got)
	}

	got.Height = 99
	if world.Config().Height != 3 {
		t.Fatalf("returned config mutation changed world config: %+v", world.Config())
	}
}
