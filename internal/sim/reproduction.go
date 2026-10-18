package sim

import (
	"cmp"
	"fmt"
	"math"
	"slices"
)

// ReproductionIntent asks the parent to place one offspring at To, which must
// be the parent's cell or one of its eight neighbors.
type ReproductionIntent struct {
	ParentID EntityID
	To       Position
}

// ReproductionRejection explains why a reproduction intent was not applied.
type ReproductionRejection uint8

const (
	ReproductionAccepted ReproductionRejection = iota
	ReproductionRejectedDuplicate
	ReproductionRejectedUnknownParent
	ReproductionRejectedDeadParent
	ReproductionRejectedUnsupportedKind
	ReproductionRejectedInsufficientEnergy
	ReproductionRejectedOutOfBounds
	ReproductionRejectedOutOfReach
	ReproductionRejectedOccupied
	ReproductionRejectedLostConflict
)

func (r ReproductionRejection) String() string {
	switch r {
	case ReproductionAccepted:
		return "accepted"
	case ReproductionRejectedDuplicate:
		return "duplicate intent"
	case ReproductionRejectedUnknownParent:
		return "unknown parent"
	case ReproductionRejectedDeadParent:
		return "dead parent"
	case ReproductionRejectedUnsupportedKind:
		return "parent's kind cannot reproduce"
	case ReproductionRejectedInsufficientEnergy:
		return "insufficient energy"
	case ReproductionRejectedOutOfBounds:
		return "out of bounds"
	case ReproductionRejectedOutOfReach:
		return "offspring cell not adjacent to parent"
	case ReproductionRejectedOccupied:
		return "offspring cell occupied"
	case ReproductionRejectedLostConflict:
		return "lost offspring cell conflict"
	default:
		return fmt.Sprintf("ReproductionRejection(%d)", uint8(r))
	}
}

// ReproductionRules configures reproduction resolution for each creature
// kind. Both kinds' rules are validated even if only one kind reproduces.
type ReproductionRules struct {
	Herbivore BirthRules
	Predator  BirthRules
}

// BirthRules configures reproduction for one creature kind.
type BirthRules struct {
	// EnergyCost is charged to each parent whose intent is accepted. A parent
	// must have strictly more energy than the cost, so reproduction never
	// kills the parent.
	EnergyCost int64
	// OffspringEnergy and OffspringHealth are the newborn's starting values.
	// Both must be positive.
	OffspringEnergy int64
	OffspringHealth int32
}

// forKind returns the birth rules for kind, or false if that kind cannot
// reproduce.
func (r ReproductionRules) forKind(kind EntityKind) (BirthRules, bool) {
	switch kind {
	case KindHerbivore:
		return r.Herbivore, true
	case KindPredator:
		return r.Predator, true
	default:
		return BirthRules{}, false
	}
}

func (r BirthRules) validate(kind string) error {
	if r.EnergyCost < 0 {
		return fmt.Errorf("%s reproduction energy cost must be non-negative: %d", kind, r.EnergyCost)
	}
	if r.OffspringEnergy <= 0 {
		return fmt.Errorf("%s offspring energy must be positive: %d", kind, r.OffspringEnergy)
	}
	if r.OffspringHealth <= 0 {
		return fmt.Errorf("%s offspring health must be positive: %d", kind, r.OffspringHealth)
	}
	return nil
}

type ReproductionOutcome struct {
	Intent ReproductionIntent
	Result ReproductionRejection
	// OffspringID is the newborn's ID when the intent is accepted.
	OffspringID EntityID
}

func (o ReproductionOutcome) Accepted() bool {
	return o.Result == ReproductionAccepted
}

type ReproductionResult struct {
	// Outcomes holds one entry per submitted intent, ordered by parent ID and
	// then destination.
	Outcomes []ReproductionOutcome
}

// resolveReproduction resolves reproduction intents against the
// post-feeding working state in two passes. The first selects accepted births
// without allocating IDs; the second verifies that every accepted birth can
// be given an ID before mutating anything, then assigns IDs in ascending
// parent ID order and applies parent costs and newborn creation. An error
// means the tick must abort.
func resolveReproduction(
	config WorldConfig,
	state *WorldState,
	intents []ReproductionIntent,
	rules ReproductionRules,
) (ReproductionResult, error) {
	if err := rules.Herbivore.validate("herbivore"); err != nil {
		return ReproductionResult{}, err
	}
	if err := rules.Predator.validate("predator"); err != nil {
		return ReproductionResult{}, err
	}

	// Creatures that died during movement or feeding do not block births.
	occupancy, err := livingOccupancy(state)
	if err != nil {
		return ReproductionResult{}, err
	}

	outcomes := make([]ReproductionOutcome, len(intents))
	for i, intent := range intents {
		outcomes[i] = ReproductionOutcome{Intent: intent}
	}
	slices.SortStableFunc(outcomes, func(a, b ReproductionOutcome) int {
		return cmp.Or(
			cmp.Compare(a.Intent.ParentID, b.Intent.ParentID),
			cmp.Compare(a.Intent.To.X, b.Intent.To.X),
			cmp.Compare(a.Intent.To.Y, b.Intent.To.Y),
		)
	})

	intentCount := make(map[EntityID]int, len(outcomes))
	for _, o := range outcomes {
		intentCount[o.Intent.ParentID]++
	}

	// Pass 1: validation and conflict resolution. Outcomes are in ascending
	// parent order, so the first valid claimant of each cell is the lowest ID.
	claimed := make(map[Position]struct{})
	accepted := 0
	for i := range outcomes {
		o := &outcomes[i]
		if intentCount[o.Intent.ParentID] > 1 {
			o.Result = ReproductionRejectedDuplicate
			continue
		}
		o.Result = validateReproductionIntent(config, state, occupancy, o.Intent, rules)
		if o.Result != ReproductionAccepted {
			continue
		}
		if _, taken := claimed[o.Intent.To]; taken {
			o.Result = ReproductionRejectedLostConflict
			continue
		}
		claimed[o.Intent.To] = struct{}{}
		accepted++
	}

	// Pass 2a: prove every accepted birth can be allocated an ID before any
	// state changes.
	if err := checkIDAllocation(state, accepted); err != nil {
		return ReproductionResult{}, err
	}

	// Pass 2b: allocation and application.
	for i := range outcomes {
		o := &outcomes[i]
		if o.Result != ReproductionAccepted {
			continue
		}

		parent := state.Creatures[o.Intent.ParentID]
		birth, _ := rules.forKind(parent.Kind)
		if birth.EnergyCost > 0 {
			if err := parent.SpendEnergy(birth.EnergyCost); err != nil {
				return ReproductionResult{}, fmt.Errorf("charge reproduction cost to creature %d: %w", parent.ID, err)
			}
		}
		state.Creatures[parent.ID] = parent

		id := state.NextEntity
		state.NextEntity++
		state.Creatures[id] = Creature{
			ID:       id,
			Kind:     parent.Kind,
			Position: o.Intent.To,
			Energy:   birth.OffspringEnergy,
			Health:   birth.OffspringHealth,
		}
		state.CreatureIDs = append(state.CreatureIDs, id)
		o.OffspringID = id
	}

	return ReproductionResult{Outcomes: outcomes}, nil
}

func validateReproductionIntent(
	config WorldConfig,
	state *WorldState,
	occupancy map[Position]EntityID,
	intent ReproductionIntent,
	rules ReproductionRules,
) ReproductionRejection {
	parent, exists := state.Creatures[intent.ParentID]
	if !exists {
		return ReproductionRejectedUnknownParent
	}
	if !parent.IsAlive() {
		return ReproductionRejectedDeadParent
	}
	birth, canReproduce := rules.forKind(parent.Kind)
	if !canReproduce {
		return ReproductionRejectedUnsupportedKind
	}
	if parent.Energy <= birth.EnergyCost {
		return ReproductionRejectedInsufficientEnergy
	}
	if !intent.To.InBounds(config.Width, config.Height) {
		return ReproductionRejectedOutOfBounds
	}
	if !withinReach(parent.Position, intent.To) {
		return ReproductionRejectedOutOfReach
	}
	if _, occupied := occupancy[intent.To]; occupied {
		return ReproductionRejectedOccupied
	}
	return ReproductionAccepted
}

// checkIDAllocation verifies that count consecutive IDs starting at
// NextEntity are free and that NextEntity can advance past them without
// reaching the reserved maximum value.
func checkIDAllocation(state *WorldState, count int) error {
	if count == 0 {
		return nil
	}
	next := state.NextEntity
	if next == 0 {
		return fmt.Errorf("NextEntity must be nonzero")
	}
	if uint64(count) >= math.MaxUint64-uint64(next) {
		return fmt.Errorf("allocating %d IDs from NextEntity %d would overflow", count, next)
	}
	for id := next; id < next+EntityID(count); id++ {
		if _, exists := state.Creatures[id]; exists {
			return fmt.Errorf("ID %d to be allocated is already used by a creature", id)
		}
		if _, exists := state.Plants[id]; exists {
			return fmt.Errorf("ID %d to be allocated is already used by a plant", id)
		}
	}
	return nil
}
