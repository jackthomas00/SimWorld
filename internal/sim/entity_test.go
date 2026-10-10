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
