# Tick Semantics

## 1. Purpose

A tick is one atomic simulation transition from committed state \(S_t\) to committed state \(S_{t+1}\).

The simulation must produce the same result when given the same initial state, configuration, seed, and ordered inputs, regardless of map iteration order or goroutine scheduling.

These rules define the initial single-process implementation. Distributed execution must preserve the same semantics.

## 2. State and Tick Numbering

- `World.Tick` identifies the most recently committed tick.
- A new world starts at tick `0`.
- Executing a tick transforms state \(S_t\) into \(S_{t+1}\).
- Entity IDs are stable and unique within a world. `NextEntity` supplies IDs for newly created entities.
- Positions use zero-based coordinates: `0 <= X < Width` and `0 <= Y < Height`.
- Invalid dimensions or invalid initial state must be rejected before simulation begins.

## 3. Transition Rules

Each tick executes the following phases in order:

1. **Snapshot:** Read the last committed world state. No changes are visible to readers during the transition.
2. **Environment:** Apply scheduled environmental changes, such as plant growth.
3. **Intent generation:** Each eligible entity chooses its action using the tick's starting snapshot and deterministic randomness. Intent generation must not mutate the world.
4. **Movement:** Resolve movement intents. Moves outside the world bounds are rejected. Conflicting moves are resolved deterministically.
5. **Feeding:** Resolve feeding intents against the post-movement working state. An entity or food source cannot be consumed more than once.
6. **Reproduction:** Resolve reproduction intents against the working state. New entities receive unique IDs in deterministic order.
7. **Cleanup:** Apply deaths and remove entities that no longer exist. Enforce entity and world invariants.
8. **Commit:** Publish the completed state and advance `World.Tick` by one.

If a phase fails, abort the tick and retain the previously committed state.

## 4. Action Ordering and Conflict Resolution

- Entity iteration order must never depend on Go map iteration order. Process entities in ascending `EntityID` order unless a phase specifies another explicit ordering.
- Intents are resolved by phase, then by an explicitly defined action priority, then by ascending actor ID.
- When multiple entities compete for the same resource, the first valid intent under this ordering wins. Later conflicting intents fail without partially applying their effects.
- An intent that is no longer valid when resolved is rejected.
- Random decisions must use a deterministic random source derived from the world seed and a defined simulation position, such as the tick and entity ID. Do not use package-global randomness.
- All tie-breaking rules must be explicit. Scheduler timing, goroutine completion order, and map iteration must not influence outcomes.

## 5. Working State and Commit Boundary

All phase mutations apply to a private working state, not the committed world state.

The implementation must:

- Avoid exposing partially applied changes to readers.
- Commit only after all phases succeed and invariants pass.
- On success, publish the new entity state and increment `World.Tick` exactly once.
- On failure, discard the working state and leave the committed state unchanged.

The initial implementation may use a deep copy of mutable state to achieve isolation. Optimizations such as copy-on-write can be introduced later without changing the observable semantics.

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
