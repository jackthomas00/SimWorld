package sim

import (
	"cmp"
	"fmt"
	"slices"
)

type MoveIntent struct {
	ActorID EntityID
	To      Position
}

// MoveRejection explains why a movement intent was not applied.
type MoveRejection uint8

const (
	MoveAccepted MoveRejection = iota
	MoveRejectedDuplicate
	MoveRejectedUnknownActor
	MoveRejectedDeadActor
	MoveRejectedOutOfBounds
	MoveRejectedNoop
	MoveRejectedLostConflict
	MoveRejectedBlocked
	MoveRejectedCycle
)

func (r MoveRejection) String() string {
	switch r {
	case MoveAccepted:
		return "accepted"
	case MoveRejectedDuplicate:
		return "duplicate intent"
	case MoveRejectedUnknownActor:
		return "unknown actor"
	case MoveRejectedDeadActor:
		return "dead actor"
	case MoveRejectedOutOfBounds:
		return "out of bounds"
	case MoveRejectedNoop:
		return "destination is current position"
	case MoveRejectedLostConflict:
		return "lost destination conflict"
	case MoveRejectedBlocked:
		return "blocked by occupant that does not vacate"
	case MoveRejectedCycle:
		return "movement cycle"
	default:
		return fmt.Sprintf("MoveRejection(%d)", uint8(r))
	}
}

// MovementRules configures movement resolution.
type MovementRules struct {
	// EnergyCost is charged to each creature whose move is accepted. Rejected
	// moves cost nothing. A creature whose energy reaches zero dies, but its
	// move still completes.
	EnergyCost int64
}

type MoveOutcome struct {
	Intent MoveIntent
	Result MoveRejection
}

func (o MoveOutcome) Accepted() bool {
	return o.Result == MoveAccepted
}

type MovementResult struct {
	// Outcomes holds one entry per submitted intent, ordered by actor ID and
	// then destination.
	Outcomes []MoveOutcome

	// Occupancy maps each cell to the living creature occupying it once
	// movement resolution completes. Creatures that died during movement are
	// excluded: this is the point at which their cells are released.
	Occupancy map[Position]EntityID
}

// moveCandidate tracks one intent through resolution.
type moveCandidate struct {
	intent MoveIntent
	from   Position
	result MoveRejection
	// resolved is set once dependency analysis reaches a verdict.
	resolved bool
}

// resolveMovement resolves movement intents against the working state and
// applies accepted moves simultaneously. All conflict and dependency analysis
// uses the occupancy at the start of the movement phase; positions are not
// mutated until every intent has a verdict.
func resolveMovement(
	config WorldConfig,
	state *WorldState,
	intents []MoveIntent,
	rules MovementRules,
) (MovementResult, error) {
	if rules.EnergyCost < 0 {
		return MovementResult{}, fmt.Errorf("movement energy cost must be non-negative: %d", rules.EnergyCost)
	}

	startOccupancy, err := livingOccupancy(state)
	if err != nil {
		return MovementResult{}, err
	}

	// Stage 1: intent collection in a total order that is independent of the
	// order intents were submitted in.
	candidates := make([]*moveCandidate, len(intents))
	for i, intent := range intents {
		candidates[i] = &moveCandidate{intent: intent}
	}
	slices.SortStableFunc(candidates, func(a, b *moveCandidate) int {
		return cmp.Or(
			cmp.Compare(a.intent.ActorID, b.intent.ActorID),
			cmp.Compare(a.intent.To.X, b.intent.To.X),
			cmp.Compare(a.intent.To.Y, b.intent.To.Y),
		)
	})

	intentCount := make(map[EntityID]int, len(candidates))
	for _, c := range candidates {
		intentCount[c.intent.ActorID]++
	}

	for _, c := range candidates {
		c.result = validateMoveIntent(config, state, c, intentCount)
	}

	// Stage 2: conflict resolution. Candidates are in ascending actor order,
	// so the first valid claimant of each destination is the lowest ID.
	winners := make(map[EntityID]*moveCandidate)
	claimed := make(map[Position]struct{})
	for _, c := range candidates {
		if c.result != MoveAccepted {
			continue
		}
		if _, taken := claimed[c.intent.To]; taken {
			c.result = MoveRejectedLostConflict
			continue
		}
		claimed[c.intent.To] = struct{}{}
		winners[c.intent.ActorID] = c
	}

	// Stage 3: dependency analysis. Each winner depends on at most one other
	// winner (the creature occupying its destination), so the graph is a set
	// of chains, each ending in an empty cell, a non-vacating occupant, or a
	// cycle.
	for _, c := range candidates {
		if c.result == MoveAccepted && !c.resolved {
			resolveMoveChain(c, startOccupancy, winners)
		}
	}

	// Stage 4: simultaneous application.
	for _, c := range candidates {
		if c.result != MoveAccepted {
			continue
		}
		creature := state.Creatures[c.intent.ActorID]
		if err := creature.Move(c.intent.To, rules.EnergyCost); err != nil {
			return MovementResult{}, fmt.Errorf("apply move for creature %d: %w", c.intent.ActorID, err)
		}
		state.Creatures[c.intent.ActorID] = creature
	}

	// Stage 5: release cells of creatures that died during movement.
	endOccupancy, err := livingOccupancy(state)
	if err != nil {
		return MovementResult{}, fmt.Errorf("movement produced invalid occupancy: %w", err)
	}

	outcomes := make([]MoveOutcome, len(candidates))
	for i, c := range candidates {
		outcomes[i] = MoveOutcome{Intent: c.intent, Result: c.result}
	}
	return MovementResult{Outcomes: outcomes, Occupancy: endOccupancy}, nil
}

func validateMoveIntent(
	config WorldConfig,
	state *WorldState,
	c *moveCandidate,
	intentCount map[EntityID]int,
) MoveRejection {
	if intentCount[c.intent.ActorID] > 1 {
		return MoveRejectedDuplicate
	}
	creature, exists := state.Creatures[c.intent.ActorID]
	if !exists {
		return MoveRejectedUnknownActor
	}
	if !creature.IsAlive() {
		return MoveRejectedDeadActor
	}
	if !c.intent.To.InBounds(config.Width, config.Height) {
		return MoveRejectedOutOfBounds
	}
	c.from = creature.Position
	if c.intent.To == c.from {
		return MoveRejectedNoop
	}
	return MoveAccepted
}

// resolveMoveChain follows the dependency chain starting at start and assigns
// a verdict to every unresolved candidate along it.
func resolveMoveChain(
	start *moveCandidate,
	startOccupancy map[Position]EntityID,
	winners map[EntityID]*moveCandidate,
) {
	var path []*moveCandidate
	onPath := make(map[*moveCandidate]int)

	settle := func(members []*moveCandidate, result MoveRejection) {
		for _, m := range members {
			m.result = result
			m.resolved = true
		}
	}

	for current := start; ; {
		if current.resolved {
			// Joined a chain that already has a verdict.
			if current.result == MoveAccepted {
				settle(path, MoveAccepted)
			} else {
				settle(path, MoveRejectedBlocked)
			}
			return
		}
		if index, seen := onPath[current]; seen {
			settle(path[index:], MoveRejectedCycle)
			settle(path[:index], MoveRejectedBlocked)
			return
		}
		onPath[current] = len(path)
		path = append(path, current)

		occupant, occupied := startOccupancy[current.intent.To]
		if !occupied {
			settle(path, MoveAccepted)
			return
		}
		next, vacates := winners[occupant]
		if !vacates {
			settle(path, MoveRejectedBlocked)
			return
		}
		current = next
	}
}

// livingOccupancy maps each cell to the living creature in it. Dead creatures
// awaiting cleanup do not occupy cells.
func livingOccupancy(state *WorldState) (map[Position]EntityID, error) {
	occupancy := make(map[Position]EntityID, len(state.CreatureIDs))
	for _, id := range state.CreatureIDs {
		creature, exists := state.Creatures[id]
		if !exists || !creature.IsAlive() {
			continue
		}
		if other, taken := occupancy[creature.Position]; taken {
			return nil, fmt.Errorf("living creatures %d and %d occupy position (%d,%d)", other, id, creature.Position.X, creature.Position.Y)
		}
		occupancy[creature.Position] = id
	}
	return occupancy, nil
}
