package sim

import "errors"

type EntityKind uint8

const (
    Plant EntityKind = iota + 1
    Herbivore
    Predator
)

type Entity struct {
    ID       EntityID  `json:"id"`
    Kind     EntityKind `json:"kind"`
    Position Position  `json:"position"`

    Energy   int64 `json:"energy"`
    Age      uint32 `json:"age"`
    Health   int32 `json:"health"`

    // Incremented whenever this entity's committed state changes.
    Version uint64 `json:"version"`
}


func (c Entity) IsAlive() bool {
    return c.Health > 0
}

func (c *Entity) ConsumeFood(amount int) error {
    if amount <= 0 {
        return errors.New("food amount must be positive")
    }

    c.Energy += amount
    return nil
}
