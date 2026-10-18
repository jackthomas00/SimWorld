package sim

import (
	"testing"
)

// hashedWorld returns a size x size world driven by the default rules and
// behavior, populated at DefaultExperiment's densities.
func hashedWorld(t *testing.T, size int32, seed uint64) *World {
	t.Helper()

	e := DefaultExperiment()
	config := e.Config
	scale := func(n int) int { return n * int(size) * int(size) / int(config.Width*config.Height) }
	config.InitialPlants = scale(config.InitialPlants)
	config.InitialHerbivores = scale(config.InitialHerbivores)
	config.InitialPredators = scale(config.InitialPredators)
	config.Width, config.Height = size, size
	config.RegionWidth, config.RegionHeight = size, size
	config.Seed = seed

	w, err := NewPopulatedWorld("hash", config, e.Initial, WithRules(e.Rules), WithBehavior(e.Behavior))
	if err != nil {
		t.Fatalf("NewPopulatedWorld: %v", err)
	}
	return w
}

func TestStateHashMatchesAcrossRunsEveryTick(t *testing.T) {
	if testing.Short() {
		t.Skip("runs two 48x48 worlds for 1000 ticks")
	}
	// Seed 5 keeps all three kinds alive for the whole run at this size.
	// Smaller worlds collapse within a few hundred ticks; see
	// docs/ecosystem-tuning.md.
	a, b := hashedWorld(t, 48, 5), hashedWorld(t, 48, 5)
	if a.StateHash() != b.StateHash() {
		t.Fatalf("initial hashes differ")
	}

	for tick := 1; tick <= 1000; tick++ {
		if err := a.Step(); err != nil {
			t.Fatalf("world a tick %d: %v", tick, err)
		}
		if err := b.Step(); err != nil {
			t.Fatalf("world b tick %d: %v", tick, err)
		}
		if ha, hb := a.StateHash(), b.StateHash(); ha != hb {
			t.Fatalf("hashes diverged at tick %d: %s != %s", tick, ha, hb)
		}
	}

	// Guard against a vacuous pass: creatures must have been acting all run.
	c := CensusOf(a.State())
	if c.Herbivores == 0 || c.Predators == 0 {
		t.Fatalf("a creature kind went extinct (%+v); pick a seed that coexists", c)
	}
}

func TestStateHashDiffersAcrossSeeds(t *testing.T) {
	if hashedWorld(t, 24, 1).StateHash() == hashedWorld(t, 24, 2).StateHash() {
		t.Fatalf("different seeds produced the same hash")
	}
}

// TestStateHashGolden pins the hash of one run. It fails whenever a change
// alters simulation results, intended or not. If the change was intended,
// update the constant and say why in the commit message.
func TestStateHashGolden(t *testing.T) {
	const want = "422276c1881b615e3e8a3685beb70bd6b14ff0a7da73b03306b8acfd4570b54c"

	w := hashedWorld(t, 24, 42)
	for tick := 1; tick <= 200; tick++ {
		if err := w.Step(); err != nil {
			t.Fatalf("tick %d: %v", tick, err)
		}
	}
	if got := w.StateHash().String(); got != want {
		t.Fatalf("hash of the 24x24 world after 200 ticks of seed 42 = %s, want %s", got, want)
	}
}

func TestStateHashIgnoresMapIterationAndCopies(t *testing.T) {
	state := hashedWorld(t, 24, 3).State()
	want := state.Hash()
	for range 20 {
		if got := state.clone().Hash(); got != want {
			t.Fatalf("clone hashed to %s, want %s", got, want)
		}
	}
}

func TestStateHashCoversEveryField(t *testing.T) {
	base := func() WorldState {
		return WorldState{
			Tick:        5,
			NextEntity:  4,
			CreatureIDs: []EntityID{1, 2},
			Creatures: map[EntityID]Creature{
				1: {ID: 1, Kind: KindHerbivore, Position: Position{X: 1, Y: 2}, Energy: 10, Age: 3, Health: 1, Version: 4},
				2: {ID: 2, Kind: KindPredator, Position: Position{X: 3, Y: 4}, Energy: 20, Age: 5, Health: 1, Version: 6},
			},
			PlantIDs: []EntityID{3},
			Plants: map[EntityID]Plant{
				3: {ID: 3, Position: Position{X: 5, Y: 6}, Energy: 7, Age: 8, Health: 1, Version: 9},
			},
		}
	}
	creature := func(s *WorldState, f func(*Creature)) {
		c := s.Creatures[1]
		f(&c)
		s.Creatures[1] = c
	}
	plant := func(s *WorldState, f func(*Plant)) {
		p := s.Plants[3]
		f(&p)
		s.Plants[3] = p
	}

	tests := []struct {
		name   string
		mutate func(*WorldState)
	}{
		{"tick", func(s *WorldState) { s.Tick++ }},
		{"next entity", func(s *WorldState) { s.NextEntity++ }},
		{"creature order", func(s *WorldState) { s.CreatureIDs = []EntityID{2, 1} }},
		{"creature removed", func(s *WorldState) { s.CreatureIDs = s.CreatureIDs[:1] }},
		{"creature kind", func(s *WorldState) { creature(s, func(c *Creature) { c.Kind = KindPredator }) }},
		{"creature x", func(s *WorldState) { creature(s, func(c *Creature) { c.Position.X++ }) }},
		{"creature y", func(s *WorldState) { creature(s, func(c *Creature) { c.Position.Y++ }) }},
		{"creature energy", func(s *WorldState) { creature(s, func(c *Creature) { c.Energy++ }) }},
		{"creature age", func(s *WorldState) { creature(s, func(c *Creature) { c.Age++ }) }},
		{"creature health", func(s *WorldState) { creature(s, func(c *Creature) { c.Health++ }) }},
		{"creature version", func(s *WorldState) { creature(s, func(c *Creature) { c.Version++ }) }},
		{"plant removed", func(s *WorldState) { s.PlantIDs = nil }},
		{"plant x", func(s *WorldState) { plant(s, func(p *Plant) { p.Position.X++ }) }},
		{"plant y", func(s *WorldState) { plant(s, func(p *Plant) { p.Position.Y++ }) }},
		{"plant energy", func(s *WorldState) { plant(s, func(p *Plant) { p.Energy++ }) }},
		{"plant age", func(s *WorldState) { plant(s, func(p *Plant) { p.Age++ }) }},
		{"plant health", func(s *WorldState) { plant(s, func(p *Plant) { p.Health++ }) }},
		{"plant version", func(s *WorldState) { plant(s, func(p *Plant) { p.Version++ }) }},
		// The same IDs moved between the creature and plant lists must not
		// collide, which the length prefixes guarantee.
		{"entity moved between lists", func(s *WorldState) {
			s.CreatureIDs = []EntityID{1}
			s.PlantIDs = []EntityID{2, 3}
		}},
	}

	want := base().Hash()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := base()
			tt.mutate(&s)
			if s.Hash() == want {
				t.Fatalf("changing %s did not change the hash", tt.name)
			}
		})
	}
}
