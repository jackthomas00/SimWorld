# Tick Semantics

## 1. Purpose

A tick is one atomic simulation transition from committed state S_t to committed state S_{t+1}.

The simulation must produce the same result when given the same initial state, configuration, seed, and ordered inputs, regardless of map iteration order or goroutine scheduling.

These rules define the initial single-process implementation. Distributed execution must preserve the same semantics.

## 2. State and Tick Numbering

- `World.Tick` identifies the most recently committed tick.
- A new world starts at tick `0`.
- Executing a tick transforms state S_t into S_{t+1}.
- Entity IDs are stable and unique within a world. `NextEntity` supplies IDs for newly created entities and must not wrap on overflow.
- Explicit IDs in an initial or loaded world must be unique across both creature and plant stores.
- Positions use zero-based coordinates: `0 <= X < Width` and `0 <= Y < Height`.
- Invalid dimensions or invalid initial state must be rejected before simulation begins.



## 3. Transition Rules

Each tick executes the following phases in order:

1. **Snapshot:** Capture the immutable committed state S_t. No changes are visible to readers during the transition.
2. **Environment:** Apply scheduled environmental changes, such as plant growth, to the private working state.
3. **Intent generation:** Each eligible entity chooses its action using the immutable snapshot S_t, not the post-environment working state. Intent generation must not mutate the world.
4. **Movement:** Resolve movement intents. Moves outside the world bounds are rejected. Conflicting moves are resolved deterministically.
5. **Feeding:** Resolve feeding intents against the post-movement working state.
6. **Reproduction:** Resolve reproduction intents against the working state. New entities receive unique IDs in deterministic order.
7. **Cleanup:** Physically remove dead or depleted entities from entity stores. Enforce entity and world invariants.
8. **Commit:** Publish the completed state and advance `World.Tick` by one.

If a phase fails, abort the tick and retain the previously committed state.

## 4. Action Ordering and Conflict Resolution

- Entity iteration order must never depend on Go map iteration order. Process entities in ascending `EntityID` order unless a phase specifies another explicit ordering.
- Intents are resolved by phase, then by an explicitly defined action priority, then by ascending actor ID.
- Each actor may have at most one accepted action per applicable phase. An actor may have one accepted movement action and one accepted feeding action in the same tick, provided it remains alive and eligible when each action is resolved.
- An intent that is no longer valid when resolved is rejected.
- Random decisions must use the simulation PRNG defined in [Deterministic Randomness](#deterministic-randomness). Do not use package-global randomness.
- All tie-breaking rules must be explicit. Scheduler timing, goroutine completion order, and map iteration must not influence outcomes.



### Death and Depletion

An entity that becomes dead in the working state is immediately ineligible for subsequent actions. Physical removal from entity stores occurs during cleanup. Dead entities are not removed midway through intent generation.

Occupancy and resource availability are released at the points defined by the phase rules:

- A creature that dies during movement no longer blocks movement once movement resolution completes.
- A creature that dies during feeding cannot reproduce.
- A plant depleted during feeding cannot be fed upon again.
- Dead or depleted entities are physically removed during cleanup.



### Movement

At commit, multiple living creatures must not occupy the same cell. Movement resolution uses deterministic dependency resolution, not literal sequential mutation:

1. Collect all movement intents that are valid against the movement-phase starting state.
2. Resolve destination conflicts by keeping only the lowest actor ID for each destination cell.
3. Build dependencies where a winning move into an occupied cell requires the current occupant to vacate.
4. Accept a movement chain if it terminates in an empty cell and every move in the chain won its destination conflict.
5. Reject cycles, including two-creature swaps.
6. Apply all accepted moves simultaneously to the working state.


| Situation                                | Initial rule                                                                               |
| ---------------------------------------- | ------------------------------------------------------------------------------------------ |
| Move into an empty cell                  | Allowed                                                                                    |
| Move into an occupied cell               | Allowed only when the occupant is part of an accepted movement chain that vacates the cell |
| Two creatures target the same empty cell | Lowest actor ID wins                                                                       |
| Two creatures swap cells                 | Rejected                                                                                   |
| Creature moves onto a plant              | Allowed                                                                                    |
| Creature dies during movement            | Its cell becomes available after movement resolution completes                             |


The movement-phase starting occupancy is determined before any moves are applied. For example, if creature `10` moves into creature `20`'s cell and creature `20` has a winning move into an empty cell, both moves may be accepted and then applied together. If the dependency chain cannot be proven to terminate in an empty cell, the dependent moves are rejected.

If an accepted move causes the actor to die, the move still resolves as part of the simultaneous movement application. The dead actor's final cell is treated as unoccupied for later phases, but the actor remains in the entity store until cleanup.

Implementation details (`resolveMovement` in `internal/sim/movement.go`):

- Intents are sorted by actor ID, then destination X, then destination Y, so the result never depends on submission order.
- An intent is rejected before conflict resolution if its actor submitted more than one movement intent (all of that actor's intents are rejected), the actor is unknown or already dead, the destination is out of bounds, or the destination equals the actor's current cell.
- Only living creatures occupy cells in the starting occupancy. Creatures that were already dead do not block movement.
- Every winning move depends on at most one other winning move: the one belonging to the occupant of its destination. Each chain is followed until it reaches an empty cell (all moves accepted), an occupant without a winning move (all moves rejected as blocked), or a cycle (all moves in the cycle rejected). Positions are not mutated until every intent has a verdict.
- Every cell in a cycle is claimed by a member of that cycle. A move from outside the cycle into one of its cells therefore loses the destination conflict.
- Each accepted move charges a flat `MovementRules.EnergyCost`, which must be non-negative. Rejected moves are free. If the cost brings energy to zero, the creature dies in its destination cell.
- Once all accepted moves are applied, occupancy is recomputed from living creatures only and returned to later phases. This is the point where cells belonging to creatures that died during movement are released.

### Feeding

Feeding conflicts are resolved separately from basic action validity:

- An actor cannot have more than one accepted feeding action in a tick.
- Food energy and prey availability are tracked in the private working state.
- Plant feeding: Multiple actors may target the same plant. Each feeding intent is resolved in order against the plant energy remaining at that moment.
- Plant feeding succeeds only if the plant has at least the full requested amount. If the remaining plant energy is less than the requested amount, the intent is rejected. Partial plant feeding is not allowed in the initial rules.
- A plant depleted during feeding cannot provide energy again later in the same tick.
- Prey feeding: Only one successful consumption can kill and consume a given prey creature. Once a prey creature has been successfully consumed, later feeding intents targeting that prey are rejected.
- A creature that becomes dead during feeding is immediately ineligible for later feeding intents and reproduction.

Implementation details (`resolveFeeding` in `internal/sim/feeding.go`):

- Intents are sorted by actor ID, then target ID, then amount, and resolved one at a time in that order. Each intent is fully validated before any of its effects apply, and its effects are visible to every later intent.
- An actor that submitted more than one feeding intent has all of them rejected.
- Diet: herbivores eat plants; predators eat herbivores. Every other pairing is rejected.
- Reach: the target must be in the actor's cell or one of its eight neighbors (Chebyshev distance at most 1).
- Each accepted intent charges a flat `FeedingRules.EnergyCost`, which must be non-negative. The actor needs strictly more energy than the cost, so feeding never kills the creature that feeds. Rejected intents are free.
- Plant intents carry a positive `Amount`. The plant must hold at least that much energy; a plant drained to zero is depleted.
- Prey is consumed whole: the predator gains all of the prey's current energy, including anything the prey ate earlier in the same phase, and the prey dies.
- An energy overflow on the actor is treated as an invariant violation and aborts the tick.



### Reproduction and IDs

Reproduction intents are resolved in deterministic order. Candidate births are first validated and conflict-resolved without allocating IDs. Rejected births do not consume IDs.


| Condition                                                                   | Result                                                                    |
| --------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| Parent is dead, became dead earlier in the tick, or lacks sufficient energy | Reject reproduction intent                                                |
| Offspring position is invalid or occupied                                   | Reject reproduction intent                                                |
| Two offspring target the same cell                                          | Lowest parent actor ID wins unless a narrower phase rule is defined later |
| `NextEntity` is zero or already in use                                      | Treat as an invariant violation and abort the tick                        |
| The next ID cannot be allocated without overflow                            | Abort the tick                                                            |
| A valid reproduction loses a conflict                                       | Reject that reproduction without partial effects                          |


After valid births are selected, accepted births allocate IDs in ascending parent actor ID order unless a narrower phase rule is defined later. Each accepted birth receives the current `NextEntity` value, then `NextEntity` increments by one. If allocation encounters an invariant violation or overflow, the tick aborts and the previously committed state is retained.

Implementation details (`resolveReproduction` in `internal/sim/reproduction.go`):

- Intents are sorted by parent ID, then destination X, then destination Y. A parent that submitted more than one reproduction intent has all of them rejected.
- Each creature kind has its own `BirthRules` in `ReproductionRules`; both kinds' rules are validated at the start of the phase. A parent whose kind has no birth rules is rejected.
- The parent must exist, be alive, and have strictly more energy than its kind's `EnergyCost`, so reproduction never kills the parent.
- The offspring cell must be in bounds, adjacent to the parent (Chebyshev distance at most 1), and free of living creatures in the post-feeding state. Creatures that died earlier in the tick do not block it, and plants never block it.
- Pass 1 validates intents and resolves cell conflicts without allocating IDs. Pass 2 first verifies every ID from `NextEntity` to `NextEntity + accepted - 1` is unused and that `NextEntity` can advance past them without reaching the reserved value `MaxUint64`, and only then mutates state. A failed allocation check leaves the working state untouched.
- Newborns copy the parent's kind and start with that kind's `OffspringEnergy` and `OffspringHealth` (both must be positive), age `0`, and version `0`.

### Deterministic Randomness

The simulation PRNG is PCG-XSH-RR 64/32. Each entity decision uses an independent per-decision stream initialized from the world seed, tick number, actor ID, decision kind, and decision ordinal.

All arithmetic in this section uses unsigned 64-bit wraparound unless a narrower type is stated. Right shifts are logical right shifts.

Decision-kind IDs are stable:


| Decision kind       | ID  |
| ------------------- | --- |
| Movement intent     | `1` |
| Feeding intent      | `2` |
| Reproduction intent | `3` |
| Plant seeding       | `4` |
| Plant sprouting     | `5` |
| Initial placement   | `6` |

Sprouting and initial placement are not per-entity decisions. They use actor ID `0`, which is never a valid entity ID, as the world's own actor.


The decision ordinal is a zero-based unsigned 64-bit integer. If an entity can make only one decision of a given kind during a tick, the ordinal is `0`. If an entity can make multiple same-kind decisions in one tick, the ordinal must be assigned by an explicit deterministic rule for that decision kind.

Seed derivation uses SplitMix64:

```text
splitmix64_next(state):
    state = state + 0x9e3779b97f4a7c15
    z = state
    z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
    z = (z ^ (z >> 27)) * 0x94d049bb133111eb
    return state, z ^ (z >> 31)
```

The seed tuple is encoded as unsigned 64-bit values in this exact order: world seed, tick number, actor ID, decision-kind ID, decision ordinal. To derive a PCG stream:

```text
splitmix_state = 0
for value in seed_tuple:
    splitmix_state = splitmix_state + value
    splitmix_state, initial_state = splitmix64_next(splitmix_state)

splitmix_state, stream = splitmix64_next(splitmix_state)
pcg_state = 0
pcg_inc = (stream << 1) | 1
pcg_advance()
pcg_state = pcg_state + initial_state
pcg_advance()
```

`stream` is therefore the SplitMix64 output after one additional state advance following the final seed-tuple value.

PCG advancement and output use:

```text
pcg_advance():
    old_state = pcg_state
    pcg_state = old_state * 6364136223846793005 + pcg_inc
    xorshifted = uint32(((old_state >> 18) ^ old_state) >> 27)
    rot = uint32(old_state >> 59)
    return uint32((xorshifted >> rot) | (xorshifted << ((-rot) & 31)))
```

`uint32(x)` keeps the low 32 bits of `x`.

Decision code must consume a fixed, documented number of random draws for each decision kind. Rejection, conflicts, and later phase outcomes must not change how many draws were consumed during intent generation.

Test vector:


| Field                                                  | Value                                                                |
| ------------------------------------------------------ | -------------------------------------------------------------------- |
| World seed                                             | `0x0123456789abcdef`                                                 |
| Tick                                                   | `42`                                                                 |
| Actor ID                                               | `1001`                                                               |
| Decision kind                                          | Movement intent (`1`)                                                |
| Decision ordinal                                       | `0`                                                                  |
| PCG state after initialization and before first output | `0x94800feadc2e8b4b`                                                 |
| PCG increment                                          | `0xa08f22dde802ee77`                                                 |
| First five outputs                                     | `0x5656e401`, `0x13b469ef`, `0x01104b72`, `0xacc421f5`, `0x3e603862` |




## 5. Working State and Commit Boundary

All phase mutations apply to a private working state, not the committed world state.

The implementation must:

- Avoid exposing partially applied changes to readers.
- Commit only after all phases succeed and invariants pass.
- Build the entire next state privately, including all maps, slices, and other mutable data reachable from the world state.
- Validate the completed working state before publication.
- On success, publish the new entity state and increment `World.Tick` exactly once as one observable transition.
- On failure, discard the working state and leave the committed state unchanged.
- Treat tick-number overflow as a failure.

The initial implementation may use a deep copy of mutable state to achieve isolation, but the copy must include every mutable value referenced by the working state. A failed phase must not mutate committed maps, slices, or nested mutable objects.

Because the current `World` shape exposes maps and slices directly, the first implementation must protect `Step()` and all reads of committed world state with a single world-level mutex. Replacing maps, slices, or fields one at a time is not an atomic publication mechanism and does not satisfy the reader guarantee.

A later implementation may instead store the committed state behind a single immutable state pointer and publish the completed next state with one atomic pointer swap. That refactor must stop exposing mutable committed maps and slices directly, or must expose only immutable snapshots. Optimizations such as copy-on-write can be introduced later without changing the observable semantics.

Implementation details (`World.Step` in `internal/sim/transition.go`):

- `Step` holds the world mutex for the whole transition and never calls the public getters, which take the same lock.
- The intent generator receives its own deep copy of S_t, separate from the working state, so a generator cannot change committed state even when it fails.
- Environment, order: aging, then plant growth, then seeding and sprouting, then metabolism.
- Environment, aging: every living plant (in `PlantIDs` order) and creature (in `CreatureIDs` order) ages by one tick, and its `Version` increments. One whose age reaches its kind's `AgingRules` lifespan dies; a lifespan of `0` means no limit. Age saturates at the maximum `uint32` value instead of overflowing. Aging runs first, so cells of plants that die of age are free for that tick's seedlings and sprouts, and cells of creatures that die of age are free for movement. Entities created later in the tick start at age `0`.
- Environment, growth: every living plant gains `EnvironmentRules.PlantGrowth` (non-negative) in `PlantIDs` order. Growth is uncapped. Depleted plants do not regrow. Energy overflow aborts the tick.
- Environment, seeding (`internal/sim/environment.go`): enabled when `SeedThreshold` is positive. After growth, each living plant with at least `SeedThreshold` energy places one seedling, using the same two passes as reproduction:
  1. Plants are visited in ascending ID order. Each eligible plant draws exactly one value from its plant-seeding stream (seed tuple: world seed, S_t's tick, plant ID, `4`, `0`) and picks uniformly among its in-bounds neighbors with no living plant. Cells claimed by lower-ID plants this tick are excluded. Creatures and depleted plants do not block a cell. A plant with no free neighbor does not seed and pays nothing.
  2. Sprouting selection, below, runs next and claims cells the same way.
  3. ID allocation for every selected seedling and sprout is checked before any mutation. Then seedlings are created in parent ID order, each parent paying `SeedCost`, followed by sprouts in attempt order. New plants get the next ID, health `1`, age `0`, and version `0`.

- Environment, sprouting: enabled when `SproutAttempts` is positive, at most `MaxSproutAttempts`. All attempts share one stream (seed tuple: world seed, S_t's tick, `0`, `5`, `0`), and each attempt draws exactly two values: X is the first modulo `Width`, Y the second modulo `Height`. A chosen cell without a living plant, including one claimed by a seedling this tick, gets a plant with `SproutEnergy`. Sprouting acts as a seed bank: plants cannot go permanently extinct.
- Environment, metabolism: after plants are placed, each living creature pays its kind's `MetabolismRules` upkeep in `CreatureIDs` order. A creature whose energy reaches zero dies and is rejected by later phases in the same tick, even though intent generation (reading S_t) may have produced intents for it.

  `SeedThreshold` must exceed `SeedCost`, so seeding never kills the parent. Seedlings do not grow in the tick they appear, and they are not visible to that tick's intent generation, which reads S_t. Because the environment runs before reproduction, seedlings in a tick receive lower IDs than that tick's births.
- Cleanup removes dead creatures and depleted plants from both the maps and the ID slices, keeping survivors in order. The working state's `Tick` is then set to S_t's tick plus one and the whole state is validated. Publication is one assignment of the working state, so tick and entities change together.

## 6. Determinism and Testing

For identical initial state, configuration, seed, and inputs, a tick must produce an identical committed result.

Tests must cover:

- Tick advancement and failure rollback.
- Out-of-bounds movement.
- Competing actions and resource conflicts.
- Deterministic entity creation and ID allocation.
- Repeatability across repeated runs with the same seed.
- Invariant preservation at the commit boundary.



## 7. Future Distributed Execution

Regions may eventually execute on separate workers, but region ownership must not change the rules above.

Cross-region actions must be coordinated before commit. A tick cannot be committed while required region results are missing or unresolved. Retries must not apply the same action twice.

Distributed coordination, worker failure recovery, and durable commits will be specified separately before multi-worker execution is implemented.