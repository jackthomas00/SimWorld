package sim

import "errors"

type EntityKind uint8

const (
	KindPlant EntityKind = iota + 1
	KindHerbivore
	KindPredator
)

var (
	ErrNonPositiveEnergy = errors.New("energy amount must be positive")
	ErrDeadCreature      = errors.New("creature is not alive")
	ErrUnknownCreature   = errors.New("unknown creature")
	ErrOutOfBounds       = errors.New("position is outside world bounds")
)

type Creature struct {
	ID       EntityID   `json:"id"`
	Kind     EntityKind `json:"kind"`
	Position Position   `json:"position"`

	Energy int64  `json:"energy"`
	Age    uint32 `json:"age"`
	Health int32  `json:"health"`

	// Incremented whenever this creature's committed state changes.
	Version uint64 `json:"version"`
}

type Plant struct {
	ID       EntityID `json:"id"`
	Position Position `json:"position"`

	Energy int64  `json:"energy"`
	Age    uint32 `json:"age"`
	Health int32  `json:"health"`

	// Incremented whenever this plant's committed state changes.
	Version uint64 `json:"version"`
}

func (c Creature) IsAlive() bool {
	return c.Health > 0
}

func (c *Creature) AddEnergy(amount int64) error {
	if amount <= 0 {
		return ErrNonPositiveEnergy
	}

	c.Energy += amount
	c.Version++
	return nil
}

func (c *Creature) SpendEnergy(amount int64) error {
	if amount <= 0 {
		return ErrNonPositiveEnergy
	}

	c.Energy -= amount
	if c.Energy <= 0 {
		c.Energy = 0
		c.Health = 0
	}
	c.Version++
	return nil
}

// Kill drains the creature's remaining energy and marks it dead.
func (c *Creature) Kill() {
	c.Energy = 0
	c.Health = 0
	c.Version++
}

func (c *Creature) ConsumeFood(amount int) error {
	return c.AddEnergy(int64(amount))
}

func (c *Creature) Move(to Position, energyCost int64) error {
	if !c.IsAlive() {
		return ErrDeadCreature
	}

	if energyCost < 0 {
		return ErrNonPositiveEnergy
	}

	if energyCost > 0 {
		c.Energy -= energyCost
		if c.Energy <= 0 {
			c.Energy = 0
			c.Health = 0
		}
	}
	c.Position = to
	c.Version++
	return nil
}

func (c *Creature) MoveBy(dx, dy int32, energyCost int64) error {
	return c.Move(Position{X: c.Position.X + dx, Y: c.Position.Y + dy}, energyCost)
}

func (p Plant) IsAlive() bool {
	return p.Health > 0
}

func (p *Plant) Grow(amount int64) error {
	if amount <= 0 {
		return ErrNonPositiveEnergy
	}

	p.Energy += amount
	p.Version++
	return nil
}

// Kill drains the plant's remaining energy and marks it dead.
func (p *Plant) Kill() {
	p.Energy = 0
	p.Health = 0
	p.Version++
}

func (p *Plant) Consume(amount int64) (int64, error) {
	if amount <= 0 {
		return 0, ErrNonPositiveEnergy
	}

	if amount > p.Energy {
		amount = p.Energy
	}

	p.Energy -= amount
	if p.Energy == 0 {
		p.Health = 0
	}
	p.Version++
	return amount, nil
}
