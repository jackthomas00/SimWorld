package sim

import (
	"cmp"
	"fmt"
	"math"
	"slices"
)

// FeedIntent asks the actor to eat the entity identified by TargetID. Entity
// IDs are unique across creature and plant stores, so the target's kind is
// determined by which store holds it.
type FeedIntent struct {
	ActorID  EntityID
	TargetID EntityID
	// Amount is the energy requested from a plant. It must be positive for
	// plant targets and is ignored for prey, which is consumed whole.
	Amount int64
}

// FeedRejection explains why a feeding intent was not applied.
type FeedRejection uint8

const (
	FeedAccepted FeedRejection = iota
	FeedRejectedDuplicate
	FeedRejectedUnknownActor
	FeedRejectedDeadActor
	FeedRejectedInsufficientEnergy
	FeedRejectedUnknownTarget
	FeedRejectedWrongDiet
	FeedRejectedOutOfReach
	FeedRejectedInvalidAmount
	FeedRejectedTargetDepleted
	FeedRejectedInsufficientFood
)

func (r FeedRejection) String() string {
	switch r {
	case FeedAccepted:
		return "accepted"
	case FeedRejectedDuplicate:
		return "duplicate intent"
	case FeedRejectedUnknownActor:
		return "unknown actor"
	case FeedRejectedDeadActor:
		return "dead actor"
	case FeedRejectedInsufficientEnergy:
		return "insufficient energy"
	case FeedRejectedUnknownTarget:
		return "unknown target"
	case FeedRejectedWrongDiet:
		return "target is not in actor's diet"
	case FeedRejectedOutOfReach:
		return "target out of reach"
	case FeedRejectedInvalidAmount:
		return "invalid plant amount"
	case FeedRejectedTargetDepleted:
		return "target already depleted or consumed"
	case FeedRejectedInsufficientFood:
		return "plant has less energy than requested"
	default:
		return fmt.Sprintf("FeedRejection(%d)", uint8(r))
	}
}

// FeedingRules configures feeding resolution.
type FeedingRules struct {
	// EnergyCost is charged to each actor whose feeding intent is accepted.
	// An actor must have strictly more energy than the cost, so feeding never
	// kills the actor that feeds. Rejected intents cost nothing.
	EnergyCost int64
}

type FeedOutcome struct {
	Intent FeedIntent
	Result FeedRejection
	// Gained is the energy the actor received before paying EnergyCost.
	Gained int64
}

func (o FeedOutcome) Accepted() bool {
	return o.Result == FeedAccepted
}

type FeedingResult struct {
	// Outcomes holds one entry per submitted intent, ordered by actor ID,
	// then target ID, then amount.
	Outcomes []FeedOutcome
}

// resolveFeeding resolves feeding intents sequentially in ascending actor ID
// order against the post-movement working state. Each intent is checked in
// full before any of its effects are applied, so a rejected intent leaves the
// state untouched. Effects of accepted intents are visible to later intents:
// a depleted plant or consumed prey cannot be eaten again, and a creature
// consumed as prey cannot act afterwards.
func resolveFeeding(
	state *WorldState,
	intents []FeedIntent,
	rules FeedingRules,
) (FeedingResult, error) {
	if rules.EnergyCost < 0 {
		return FeedingResult{}, fmt.Errorf("feeding energy cost must be non-negative: %d", rules.EnergyCost)
	}

	outcomes := make([]FeedOutcome, len(intents))
	for i, intent := range intents {
		outcomes[i] = FeedOutcome{Intent: intent}
	}
	slices.SortStableFunc(outcomes, func(a, b FeedOutcome) int {
		return cmp.Or(
			cmp.Compare(a.Intent.ActorID, b.Intent.ActorID),
			cmp.Compare(a.Intent.TargetID, b.Intent.TargetID),
			cmp.Compare(a.Intent.Amount, b.Intent.Amount),
		)
	})

	intentCount := make(map[EntityID]int, len(outcomes))
	for _, o := range outcomes {
		intentCount[o.Intent.ActorID]++
	}

	for i := range outcomes {
		o := &outcomes[i]
		if intentCount[o.Intent.ActorID] > 1 {
			o.Result = FeedRejectedDuplicate
			continue
		}
		o.Result = validateFeedIntent(state, o.Intent, rules)
		if o.Result != FeedAccepted {
			continue
		}
		gained, err := applyFeedIntent(state, o.Intent, rules)
		if err != nil {
			return FeedingResult{}, err
		}
		o.Gained = gained
	}

	return FeedingResult{Outcomes: outcomes}, nil
}

// validateFeedIntent checks the intent against the current working state
// without mutating it.
func validateFeedIntent(state *WorldState, intent FeedIntent, rules FeedingRules) FeedRejection {
	actor, exists := state.Creatures[intent.ActorID]
	if !exists {
		return FeedRejectedUnknownActor
	}
	if !actor.IsAlive() {
		return FeedRejectedDeadActor
	}
	if actor.Energy <= rules.EnergyCost {
		return FeedRejectedInsufficientEnergy
	}

	if plant, isPlant := state.Plants[intent.TargetID]; isPlant {
		if actor.Kind != KindHerbivore {
			return FeedRejectedWrongDiet
		}
		if !withinReach(actor.Position, plant.Position) {
			return FeedRejectedOutOfReach
		}
		if intent.Amount <= 0 {
			return FeedRejectedInvalidAmount
		}
		if !plant.IsAlive() {
			return FeedRejectedTargetDepleted
		}
		if plant.Energy < intent.Amount {
			return FeedRejectedInsufficientFood
		}
		return FeedAccepted
	}

	if prey, isCreature := state.Creatures[intent.TargetID]; isCreature {
		if actor.Kind != KindPredator || prey.Kind != KindHerbivore {
			return FeedRejectedWrongDiet
		}
		if !withinReach(actor.Position, prey.Position) {
			return FeedRejectedOutOfReach
		}
		if !prey.IsAlive() {
			return FeedRejectedTargetDepleted
		}
		return FeedAccepted
	}

	return FeedRejectedUnknownTarget
}

// applyFeedIntent applies an intent that validateFeedIntent accepted and
// returns the energy the actor gained.
func applyFeedIntent(state *WorldState, intent FeedIntent, rules FeedingRules) (int64, error) {
	actor := state.Creatures[intent.ActorID]

	var gained int64
	if plant, isPlant := state.Plants[intent.TargetID]; isPlant {
		gained = intent.Amount
		if err := checkEnergyHeadroom(actor, gained, rules.EnergyCost); err != nil {
			return 0, err
		}
		taken, err := plant.Consume(gained)
		if err != nil {
			return 0, fmt.Errorf("consume plant %d: %w", intent.TargetID, err)
		}
		if taken != gained {
			return 0, fmt.Errorf("plant %d yielded %d energy, want %d", intent.TargetID, taken, gained)
		}
		state.Plants[intent.TargetID] = plant
	} else {
		prey := state.Creatures[intent.TargetID]
		gained = prey.Energy
		if err := checkEnergyHeadroom(actor, gained, rules.EnergyCost); err != nil {
			return 0, err
		}
		prey.Kill()
		state.Creatures[intent.TargetID] = prey
	}

	if rules.EnergyCost > 0 {
		if err := actor.SpendEnergy(rules.EnergyCost); err != nil {
			return 0, fmt.Errorf("charge feeding cost to creature %d: %w", intent.ActorID, err)
		}
	}
	if err := actor.AddEnergy(gained); err != nil {
		return 0, fmt.Errorf("credit food to creature %d: %w", intent.ActorID, err)
	}
	state.Creatures[intent.ActorID] = actor
	return gained, nil
}

// checkEnergyHeadroom rejects a meal that would overflow the actor's energy.
// Reaching this is an invariant violation rather than an ordinary rejection.
func checkEnergyHeadroom(actor Creature, gained, cost int64) error {
	if actor.Energy-cost > math.MaxInt64-gained {
		return fmt.Errorf("feeding creature %d would overflow its energy", actor.ID)
	}
	return nil
}

// withinReach reports whether b is in a's cell or one of its eight neighbors.
func withinReach(a, b Position) bool {
	return chebyshevDistance(a, b) <= 1
}

func chebyshevDistance(a, b Position) int64 {
	dx := int64(a.X) - int64(b.X)
	dy := int64(a.Y) - int64(b.Y)
	return max(dx, -dx, dy, -dy)
}
