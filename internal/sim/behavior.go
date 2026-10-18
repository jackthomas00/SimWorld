package sim

import "fmt"

// BehaviorRules configures the herbivore and predator intent generator.
type BehaviorRules struct {
	// SenseRadius is the Chebyshev distance at which a creature sees food,
	// prey and predators. It must be between 1 and MaxSenseRadius.
	SenseRadius int32
	// FleeRadius is the distance at which a herbivore notices a predator and
	// flees instead of foraging. It must be between 1 and SenseRadius. At 1,
	// herbivores cannot see a predator that is two cells away, which can step
	// in and eat in the same tick.
	FleeRadius int32
	// HerbivoreBite is the most energy a herbivore requests from a plant in
	// one feeding. It must be positive.
	HerbivoreBite int64
	// HerbivoreReproduceAt and PredatorReproduceAt are the energy levels at
	// which each kind tries to reproduce. Both must be positive. They should
	// exceed the kind's BirthRules.EnergyCost, or every attempt is rejected.
	HerbivoreReproduceAt int64
	PredatorReproduceAt  int64
	// PredatorSatedAt, when positive, is the energy at which a predator stops
	// hunting: it reproduces if it can and otherwise rests in place. This
	// caps how fast predators convert prey into energy. Zero disables it.
	PredatorSatedAt int64
}

// MaxSenseRadius bounds the square each creature scans per tick.
const MaxSenseRadius = 64

func DefaultBehaviorRules() BehaviorRules {
	return BehaviorRules{
		SenseRadius:          3,
		FleeRadius:           3,
		HerbivoreBite:        3,
		HerbivoreReproduceAt: 40,
		PredatorReproduceAt:  80,
		PredatorSatedAt:      30,
	}
}

// Validate reports whether the rules can drive a generator.
func (rules BehaviorRules) Validate() error {
	if rules.SenseRadius < 1 || rules.SenseRadius > MaxSenseRadius {
		return fmt.Errorf("sense radius must be between 1 and %d: %d", MaxSenseRadius, rules.SenseRadius)
	}
	if rules.FleeRadius < 1 || rules.FleeRadius > rules.SenseRadius {
		return fmt.Errorf("flee radius must be between 1 and sense radius %d: %d", rules.SenseRadius, rules.FleeRadius)
	}
	if rules.HerbivoreBite <= 0 {
		return fmt.Errorf("herbivore bite must be positive: %d", rules.HerbivoreBite)
	}
	if rules.PredatorSatedAt < 0 {
		return fmt.Errorf("predator satiation must be non-negative: %d", rules.PredatorSatedAt)
	}
	if rules.HerbivoreReproduceAt <= 0 || rules.PredatorReproduceAt <= 0 {
		return fmt.Errorf("reproduction thresholds must be positive: herbivore %d, predator %d",
			rules.HerbivoreReproduceAt, rules.PredatorReproduceAt)
	}

	return nil
}

// NewBehaviorGenerator returns an IntentGenerator that drives herbivores and
// predators. Intents depend only on the snapshot, the config and the rules.
//
// Herbivores, in priority order:
//  1. If a predator is within FleeRadius, flee: move to the free neighbor farthest from
//     the nearest sensed predator, if that improves on staying. Nothing else.
//  2. If energy is at least HerbivoreReproduceAt and a neighbor is free,
//     reproduce there and do not move.
//  3. If a plant is in reach, eat the nearest one and do not move.
//  4. If a plant is sensed, step toward the nearest one, and also eat it if
//     the destination will be in reach.
//  5. Otherwise wander.
//
// Predators follow the same steps 2-5 with herbivores as food, and do not flee.
// A predator at or above PredatorSatedAt only reproduces or rests.
//
// Randomness: each living creature draws exactly one value from its movement
// stream and one from its reproduction stream every tick, used or not.
// Feeding decisions are deterministic and draw nothing.
func NewBehaviorGenerator(rules BehaviorRules) (IntentGenerator, error) {
	if err := rules.Validate(); err != nil {
		return nil, err
	}

	return func(config WorldConfig, snapshot WorldState) (Intents, error) {
		b := newBehavior(config, snapshot, rules)
		return b.generate(), nil
	}, nil
}

// behavior holds read-only indexes over one snapshot.
type behavior struct {
	config     WorldConfig
	snapshot   WorldState
	rules      BehaviorRules
	creatureAt map[Position]EntityID   // living creatures
	plantsAt   map[Position][]EntityID // living plants, ascending ID
}

func newBehavior(config WorldConfig, snapshot WorldState, rules BehaviorRules) *behavior {
	b := &behavior{
		config:     config,
		snapshot:   snapshot,
		rules:      rules,
		creatureAt: make(map[Position]EntityID, len(snapshot.CreatureIDs)),
		plantsAt:   make(map[Position][]EntityID, len(snapshot.PlantIDs)),
	}
	for _, id := range snapshot.CreatureIDs {
		if c, ok := snapshot.Creatures[id]; ok && c.IsAlive() {
			b.creatureAt[c.Position] = id
		}
	}
	for _, id := range sortedIDs(snapshot.PlantIDs) {
		if p, ok := snapshot.Plants[id]; ok && p.IsAlive() {
			b.plantsAt[p.Position] = append(b.plantsAt[p.Position], id)
		}
	}
	return b
}

func (b *behavior) generate() Intents {
	var out Intents
	for _, id := range sortedIDs(b.snapshot.CreatureIDs) {
		c, ok := b.snapshot.Creatures[id]
		if !ok || !c.IsAlive() {
			continue
		}

		moveRNG := NewDecisionRandom(b.config.Seed, b.snapshot.Tick, id, DecisionKindMovementIntent, 0)
		birthRNG := NewDecisionRandom(b.config.Seed, b.snapshot.Tick, id, DecisionKindReproductionIntent, 0)
		moveDraw, birthDraw := moveRNG.Uint32(), birthRNG.Uint32()

		switch c.Kind {
		case KindHerbivore:
			b.herbivore(c, moveDraw, birthDraw, &out)
		case KindPredator:
			b.predator(c, moveDraw, birthDraw, &out)
		}
	}
	return out
}

func (b *behavior) herbivore(c Creature, moveDraw, birthDraw uint32, out *Intents) {
	predators := b.kindAt(KindPredator)
	if len(b.within(c.Position, b.rules.FleeRadius, predators)) > 0 {
		threats := b.within(c.Position, b.rules.SenseRadius, predators)
		if to, ok := b.flee(c.Position, threats, moveDraw); ok {
			out.Move = append(out.Move, MoveIntent{ActorID: c.ID, To: to})
		}
		return
	}

	feed := func(target EntityID) FeedIntent {
		amount := min(b.rules.HerbivoreBite, b.snapshot.Plants[target].Energy)
		return FeedIntent{ActorID: c.ID, TargetID: target, Amount: amount}
	}
	b.forage(c, b.rules.HerbivoreReproduceAt, b.plantsAtCell, feed, moveDraw, birthDraw, out)
}

func (b *behavior) predator(c Creature, moveDraw, birthDraw uint32, out *Intents) {
	if b.rules.PredatorSatedAt > 0 && c.Energy >= b.rules.PredatorSatedAt {
		if c.Energy >= b.rules.PredatorReproduceAt {
			if to, ok := pick(b.freeNeighbors(c.Position), birthDraw); ok {
				out.Reproduce = append(out.Reproduce, ReproductionIntent{ParentID: c.ID, To: to})
			}
		}
		return
	}

	feed := func(target EntityID) FeedIntent {
		return FeedIntent{ActorID: c.ID, TargetID: target}
	}
	b.forage(c, b.rules.PredatorReproduceAt, b.kindAt(KindHerbivore), feed, moveDraw, birthDraw, out)
}

// forage implements the shared steps: reproduce, eat in reach, approach food,
// or wander.
func (b *behavior) forage(
	c Creature,
	reproduceAt int64,
	food func(Position) []EntityID,
	feed func(EntityID) FeedIntent,
	moveDraw, birthDraw uint32,
	out *Intents,
) {
	staying := false
	if c.Energy >= reproduceAt {
		if to, ok := pick(b.freeNeighbors(c.Position), birthDraw); ok {
			out.Reproduce = append(out.Reproduce, ReproductionIntent{ParentID: c.ID, To: to})
			staying = true
		}
	}

	target, targetPos, found := b.nearest(c.Position, b.rules.SenseRadius, food)
	if found && withinReach(c.Position, targetPos) {
		out.Feed = append(out.Feed, feed(target))
		return
	}
	if staying {
		return
	}

	if found {
		if to, ok := b.approach(c.Position, targetPos, moveDraw); ok {
			out.Move = append(out.Move, MoveIntent{ActorID: c.ID, To: to})
			if withinReach(to, targetPos) {
				out.Feed = append(out.Feed, feed(target))
			}
		}
		return
	}

	if to, ok := b.wander(c.Position, moveDraw); ok {
		out.Move = append(out.Move, MoveIntent{ActorID: c.ID, To: to})
	}
}

// flee picks the free neighbor that maximizes the distance to the nearest
// threat. It reports false when no neighbor improves on staying.
func (b *behavior) flee(from Position, threats []Position, draw uint32) (Position, bool) {
	safety := func(p Position) int64 {
		nearest := int64(-1)
		for _, t := range threats {
			if d := chebyshevDistance(p, t); nearest < 0 || d < nearest {
				nearest = d
			}
		}
		return nearest
	}
	best, bestScore := bestBy(b.freeNeighbors(from), safety)
	if len(best) == 0 || bestScore <= safety(from) {
		return Position{}, false
	}
	return pick(best, draw)
}

// approach picks the free neighbor closest to target. It reports false when
// no neighbor is closer than the current cell.
func (b *behavior) approach(from, target Position, draw uint32) (Position, bool) {
	best, bestScore := bestBy(b.freeNeighbors(from), func(p Position) int64 { return -chebyshevDistance(p, target) })
	if len(best) == 0 || -bestScore >= chebyshevDistance(from, target) {
		return Position{}, false
	}
	return pick(best, draw)
}

// wander picks uniformly among the free neighbors and staying put. Staying
// produces no intent.
func (b *behavior) wander(from Position, draw uint32) (Position, bool) {
	to, _ := pick(append(b.freeNeighbors(from), from), draw)
	return to, to != from
}

// freeNeighbors lists in-bounds neighbors with no living creature, in
// neighborOffsets order.
func (b *behavior) freeNeighbors(from Position) []Position {
	free := make([]Position, 0, len(neighborOffsets))
	for _, o := range neighborOffsets {
		p := Position{X: from.X + o[0], Y: from.Y + o[1]}
		if !p.InBounds(b.config.Width, b.config.Height) {
			continue
		}
		if _, occupied := b.creatureAt[p]; occupied {
			continue
		}
		free = append(free, p)
	}
	return free
}

// nearest finds the entity returned by at that is closest to from within
// radius, breaking distance ties by lowest ID.
func (b *behavior) nearest(from Position, radius int32, at func(Position) []EntityID) (EntityID, Position, bool) {
	var (
		best     EntityID
		bestPos  Position
		bestDist int64 = -1
	)
	b.scan(from, radius, func(p Position) {
		d := chebyshevDistance(from, p)
		for _, id := range at(p) {
			if bestDist < 0 || d < bestDist || (d == bestDist && id < best) {
				best, bestPos, bestDist = id, p, d
			}
		}
	})
	return best, bestPos, bestDist >= 0
}

// within lists the cells within radius of from that hold at least one entity
// returned by at.
func (b *behavior) within(from Position, radius int32, at func(Position) []EntityID) []Position {
	var cells []Position
	b.scan(from, radius, func(p Position) {
		if len(at(p)) > 0 {
			cells = append(cells, p)
		}
	})
	return cells
}

// scan visits every in-bounds cell within radius of from in row-major order.
func (b *behavior) scan(from Position, radius int32, visit func(Position)) {
	r := int64(radius)
	for y := int64(from.Y) - r; y <= int64(from.Y)+r; y++ {
		for x := int64(from.X) - r; x <= int64(from.X)+r; x++ {
			if x < 0 || y < 0 || x >= int64(b.config.Width) || y >= int64(b.config.Height) {
				continue
			}
			visit(Position{X: int32(x), Y: int32(y)})
		}
	}
}

func (b *behavior) kindAt(kind EntityKind) func(Position) []EntityID {
	return func(p Position) []EntityID {
		id, ok := b.creatureAt[p]
		if !ok || b.snapshot.Creatures[id].Kind != kind {
			return nil
		}
		return []EntityID{id}
	}
}

func (b *behavior) plantsAtCell(p Position) []EntityID {
	return b.plantsAt[p]
}

// bestBy returns the candidates with the highest score, in input order.
func bestBy(candidates []Position, score func(Position) int64) ([]Position, int64) {
	var (
		best      []Position
		bestScore int64
	)
	for _, p := range candidates {
		s := score(p)
		switch {
		case len(best) == 0 || s > bestScore:
			best, bestScore = []Position{p}, s
		case s == bestScore:
			best = append(best, p)
		}
	}
	return best, bestScore
}
