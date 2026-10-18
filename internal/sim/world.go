package sim

import "sync"

type EntityID uint64
type RegionID uint32
type Tick uint64

type Position struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
}

func (p Position) InBounds(width, height int32) bool {
	return p.X >= 0 && p.X < width && p.Y >= 0 && p.Y < height
}

type WorldConfig struct {
	Width             int32  `json:"width"`
	Height            int32  `json:"height"`
	RegionWidth       int32  `json:"region_width"`
	RegionHeight      int32  `json:"region_height"`
	InitialPlants     int    `json:"initial_plants"`
	InitialHerbivores int    `json:"initial_herbivores"`
	InitialPredators  int    `json:"initial_predators"`
	Seed              uint64 `json:"seed"`
}

type WorldState struct {
	Tick Tick

	CreatureIDs []EntityID
	Creatures   map[EntityID]Creature
	PlantIDs    []EntityID
	Plants      map[EntityID]Plant

	NextEntity EntityID
}

type World struct {
	mu       sync.RWMutex
	id       string
	config   WorldConfig
	rules    Rules
	generate IntentGenerator
	state    WorldState
}

// WorldOption customizes a World at construction.
type WorldOption func(*World)

// WithRules replaces DefaultRules.
func WithRules(rules Rules) WorldOption {
	return func(w *World) { w.rules = rules }
}

// WithIntentGenerator sets the intent generator Step uses. Without one, every
// tick runs with no intents.
func WithIntentGenerator(generate IntentGenerator) WorldOption {
	return func(w *World) { w.generate = generate }
}

func NewWorld(id string, config WorldConfig, opts ...WorldOption) *World {
	w := &World{
		id:       id,
		config:   config,
		rules:    DefaultRules(),
		generate: NoIntents,
		state: WorldState{
			Creatures:  make(map[EntityID]Creature),
			Plants:     make(map[EntityID]Plant),
			NextEntity: 1,
		},
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

func (w *World) ID() string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.id
}

func (w *World) Config() WorldConfig {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.config
}

func (w *World) State() WorldState {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.state.clone()
}

func (w *World) Tick() Tick {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.state.Tick
}

func (w *World) AddCreature(creature Creature) EntityID {
	w.mu.Lock()
	defer w.mu.Unlock()

	if creature.ID == 0 {
		creature.ID = w.nextEntityID()
	} else if creature.ID >= w.state.NextEntity {
		w.state.NextEntity = creature.ID + 1
	}

	w.ensureEntityStores()
	if _, exists := w.state.Creatures[creature.ID]; !exists {
		w.state.CreatureIDs = append(w.state.CreatureIDs, creature.ID)
	}
	w.state.Creatures[creature.ID] = creature
	return creature.ID
}

func (w *World) AddPlant(plant Plant) EntityID {
	w.mu.Lock()
	defer w.mu.Unlock()

	if plant.ID == 0 {
		plant.ID = w.nextEntityID()
	} else if plant.ID >= w.state.NextEntity {
		w.state.NextEntity = plant.ID + 1
	}

	w.ensureEntityStores()
	if _, exists := w.state.Plants[plant.ID]; !exists {
		w.state.PlantIDs = append(w.state.PlantIDs, plant.ID)
	}
	w.state.Plants[plant.ID] = plant
	return plant.ID
}

func (w *World) Creature(id EntityID) (Creature, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	creature, exists := w.state.Creatures[id]
	return creature, exists
}

func (w *World) Plant(id EntityID) (Plant, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	plant, exists := w.state.Plants[id]
	return plant, exists
}

func (w *World) CreatureList() []Creature {
	w.mu.RLock()
	defer w.mu.RUnlock()

	creatures := make([]Creature, 0, len(w.state.CreatureIDs))
	for _, id := range w.state.CreatureIDs {
		creature, exists := w.state.Creatures[id]
		if exists {
			creatures = append(creatures, creature)
		}
	}
	return creatures
}

func (w *World) PlantList() []Plant {
	w.mu.RLock()
	defer w.mu.RUnlock()

	plants := make([]Plant, 0, len(w.state.PlantIDs))
	for _, id := range w.state.PlantIDs {
		plant, exists := w.state.Plants[id]
		if exists {
			plants = append(plants, plant)
		}
	}
	return plants
}

func (w *World) nextEntityID() EntityID {
	if w.state.NextEntity == 0 {
		w.state.NextEntity = 1
	}

	id := w.state.NextEntity
	w.state.NextEntity++
	return id
}

func (w *World) ensureEntityStores() {
	if w.state.Creatures == nil {
		w.state.Creatures = make(map[EntityID]Creature)
	}
	if w.state.Plants == nil {
		w.state.Plants = make(map[EntityID]Plant)
	}
}

func (s WorldState) clone() WorldState {
	next := WorldState{
		Tick:        s.Tick,
		NextEntity:  s.NextEntity,
		Creatures:   make(map[EntityID]Creature, len(s.Creatures)),
		Plants:      make(map[EntityID]Plant, len(s.Plants)),
		CreatureIDs: append([]EntityID(nil), s.CreatureIDs...),
		PlantIDs:    append([]EntityID(nil), s.PlantIDs...),
	}

	for id, creature := range s.Creatures {
		next.Creatures[id] = creature
	}

	for id, plant := range s.Plants {
		next.Plants[id] = plant
	}

	return next
}
