# Roadmap

The ecosystem is a workload, not the goal. Each milestone must leave the simulation provably deterministic, so the next one has a reference to check against.

## 1. Simulation core: done

- Creature and plant state behind a `World` with an atomic `Step` that runs on a private working copy, validates it, and then publishes it.
- Fixed phase order: environment (aging, growth, seeding and sprouting, metabolism), intents, movement, feeding, reproduction, cleanup. Deterministic conflict resolution throughout.
- Counter-based randomness keyed by (seed, tick, actor, decision kind, ordinal).
- Rule-based herbivore and predator behavior, an experiment harness, and a seed-sweep CLI.
- Integration tests for rollback, determinism and concurrent access.
- Ecosystem tuning reached about 70-80% coexistence over 4000 ticks and is **parked**; see [ecosystem-tuning.md](ecosystem-tuning.md).

## 2. Hashing, checkpoints and replay: next

The goal is to prove that two runs are identical, and that a run can stop and resume without changing its outcome.

1. **Canonical state hash.** Encode `WorldState` in a fixed binary layout: tick, `NextEntity`, then every creature and plant in ascending ID order, field by field. Hash the result with SHA-256. Do not hash JSON or map iteration order.
   - *Exit:* two runs from the same seed produce the same hash at every tick for 1000 ticks. A golden-hash test pins the hash for one seed at one tick, so any change to the rules or to determinism fails it.
2. **Checkpoint save and load.** Use a versioned JSON snapshot holding the schema version, world ID, config, `Rules`, `BehaviorRules`, the full `WorldState` and its hash.
   - On load, run `validateState`, recompute the hash, and reject any mismatch.
   - The intent generator is a func, so a checkpoint must store `BehaviorRules` and rebuild the generator from them.
   - *Exit:* run N ticks, save, load into a fresh `World` and run M more ticks. The hashes must match an uninterrupted N+M run at every tick.
3. **CLI support.** Print a hash per sample in `-trace`, and add save and resume flags.
4. **Event log (optional).** The movement, feeding and reproduction resolvers already return outcomes and rejections, and these could become a per-tick event record. Defer this until replay or debugging needs it.

## 3. Control API

- A single scheduler goroutine owns the world.
- HTTP endpoints for create, inspect, step, pause and resume.

## 4. Parallel execution

- Profile, then compute intents per region on goroutines and merge them deterministically.
- *Exit:* the same state hashes as sequential execution, plus a benchmark of the speedup.

## 5. Distributed workers

- Region ownership, a coordinator, barrier-based ticks and worker failure recovery.
- See [design.md](design.md) §5-7.
