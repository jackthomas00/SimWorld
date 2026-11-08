# CLAUDE.md

SimWorld is a deterministic ecosystem simulator in Go (plants, herbivores, predators on a grid). The ecosystem is only the workload. The real goals are atomic ticks, reproducible runs, checkpoints and replay, and eventually a world partitioned across workers that gives the same results as a single process.

## Commands

```bash
go test ./...                     # full suite, about 30s
go test -short ./...              # skips the long ecosystem and 1000-tick hash tests
go test -race ./...               # needed for the concurrent Step/reader tests
go test ./internal/sim -run TestStateHashGolden
go run ./cmd/simworld -seeds 1-8                # seed sweep with default rules
go run ./cmd/simworld -seeds 3 -trace -every 50 # census samples for one run
```

No external dependencies; standard library only (`go 1.25`).

## Where things are

- [docs/roadmap.md](docs/roadmap.md): current milestone and what comes next. Read it first.
- [docs/tick-semantics.md](docs/tick-semantics.md): **the spec**. Phase order, conflict resolution, RNG algorithm, hash and checkpoint guarantees. Each phase has an "Implementation details" section that must match the code.
- [docs/ecosystem-tuning.md](docs/ecosystem-tuning.md): how the default rules were chosen. Tuning is parked.
- [docs/design.md](docs/design.md): long-range design (distributed protocol, partitioning). This is reference material, not what is built today.

Code:

| File (`internal/sim`) | Role |
| --- | --- |
| `world.go` | `World` (mutex-guarded), `WorldState`, options (`WithRules`, `WithBehavior`, `WithIntentGenerator`), `clone` |
| `transition.go` | `World.Step`: the tick pipeline and commit |
| `environment.go` | aging, plant growth, seeding and sprouting, metabolism |
| `movement.go`, `feeding.go`, `reproduction.go` | per-phase resolvers; each returns outcomes and rejections |
| `behavior.go` | `BehaviorRules` and the rule-based intent generator |
| `random.go` | counter-based PCG stream per (seed, tick, actor, decision kind, ordinal) |
| `validate.go` | `validateState`, run at every commit and on restore |
| `hash.go` | canonical SHA-256 state hash |
| `restore.go` | `World.Checkpoint` / `sim.Restore` (in memory) |
| `experiment.go` | `NewPopulatedWorld`, `DefaultExperiment`, trial runner |
| `rules.go` | `Rules` and `DefaultRules` |

`internal/checkpoint` holds versioned JSON checkpoint files: `Save` is atomic (temp file, fsync, rename, dir fsync), and `Load` validates the state and verifies its hash. `cmd/simworld` is the seed-sweep CLI.

## Tick pipeline

`Step` takes the write lock, deep-copies the committed state into a **snapshot** (for intent generation) and a **working** copy (for mutation), then runs: aging, then environment (growth, seeding, sprouting), then metabolism, then intent generation (reads the snapshot S_t, not the post-environment state), then movement, feeding, reproduction, `removeDead`, `Tick+1`, `validateState`, and finally publishes with a single assignment. Any error leaves the committed state untouched.

## Determinism rules (do not break)

- Never let Go map iteration order affect results. Iterate `CreatureIDs` / `PlantIDs`, or sort first (`sortedIDs`). ID-slice order is part of the state and is hashed.
- No `math/rand` or global randomness in simulation code (tests may use it to shuffle inputs). Use `NewDecisionRandom(seed, tick, actor, kind, ordinal)`. Decision-kind IDs in `random.go` are stable; add new ones, never renumber. Actor `0` is the world itself.
- Each decision consumes a fixed, documented number of draws, whatever the outcome.
- Resolvers sort intents explicitly (actor ID, then target or destination) and make every tie-break explicit. Lowest actor ID wins conflicts.
- Rejected births and seedlings do not consume IDs. ID allocation is fully checked before any mutation.
- Overflow (energy, IDs, tick) is an error that aborts the tick; it must never wrap.
- `Step` must not call the public getters, because they take the same lock.

## Tests that guard determinism

- `TestStateHashGolden` (`hash_test.go`) pins one hash. **Any change to simulation results fails it.** Update the golden value only for an intended rule change, and say so in the commit.
- `TestStateHashCoversEveryField`: a new field on `Creature`/`Plant`/`WorldState` must be added to the encoding in `hash.go`, and the hash domain tag's version must be bumped when the layout changes. New state fields also need to be handled in `clone`, `validateState` and the checkpoint JSON in `internal/checkpoint`.
- `TestDefaultEcosystemCoexists` pins seeds 1-4 coexisting with the default rules.
- Checkpoint resume tests: save after N ticks, load, run M more; the hashes must equal an uninterrupted N+M run.
- Rollback tests compare the full committed state, not just the tick.

## Conventions

- When behavior changes, update `docs/tick-semantics.md` (the spec and the implementation details) in the same change. Update `docs/roadmap.md` when a milestone item lands.
- Rules are validated when their phase runs, so invalid rules make `Step` fail rather than panic. `BehaviorRules.Validate` covers the behavior rules.
- Worlds built with `WithIntentGenerator` cannot be checkpointed. Use `WithBehavior` for anything that must be saved.
- Don't re-tune ecosystem weights (`DefaultRules`, `DefaultBehaviorRules`) unless asked. The current focus is milestone 2 (hashing, checkpoints, replay; CLI save/resume and per-sample hashes are next).
- Commit messages: short imperative subject ("Add checkpoint files with hash-verified load and atomic save").
