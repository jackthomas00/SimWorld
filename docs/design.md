# SimWorld: Technical Design & Implementation Plan

> **Reference, not status.** This is the original long-range design. For current status and the next milestone, see [roadmap.md](roadmap.md). Where this doc and [tick-semantics.md](tick-semantics.md) disagree, tick-semantics.md wins: for example, `Step` mutates a private working copy rather than returning a new `World`, and the phase order differs. Sections 5-7 (distributed protocol, persistence, partitioning) are still the plan for later milestones.

Design v0.1 · Go · Distributed systems first · Ecosystem simulation

The goal is to build a deterministic ecosystem simulator that evolves from a single process into a distributed system of cooperating simulation workers. The ecosystem is the workload; coordination, consistency, recovery, and measurable scaling are the core engineering objectives.

I recommend a modular Go application initially, with a single authoritative simulation process, explicit tick boundaries, deterministic action resolution, and durable checkpoints. We will introduce distributed workers only after the single-process implementation establishes a correctness baseline.

The central engineering question is:

> Can we divide a stateful simulation across multiple workers while preserving the same logical outcomes, recovering from failures, and measuring the cost of coordination?

This is a substantial but manageable portfolio project. It can demonstrate backend engineering, concurrency, distributed protocols, fault injection, and performance analysis without requiring a large team or expensive infrastructure.

## 1. Goals and non-goals

### Goals for the first several versions

- Deterministic simulation from a known initial state and seed.
- Explicit ownership of entities and spatial regions.
- Defined tick semantics and conflict resolution.
- Durable checkpoints and recovery after process failure.
- Distributed execution across multiple worker processes.
- Correct handling of duplicate messages, stale state, and worker restarts.
- Benchmarks comparing sequential, parallel, and distributed execution.

### Non-goals for v0.1

- Multiplayer gameplay or a public hosted platform.
- Arbitrary user-supplied code execution.
- LLM-controlled agents.
- A general-purpose physics engine.
- Kubernetes, service meshes, or elaborate deployment infrastructure.
- Active-active database replication or exactly-once message delivery.

These can come later if they solve a real problem. The first implementation should establish a strong correctness model rather than accumulate infrastructure.

## 2. Architecture and evolution

The architecture should evolve through three deployment stages without rewriting the simulation rules.

Stage 1 — Single process

One Go process owns the entire world. The simulation core has no network or database dependencies.

`API → Scheduler → Simulation Core → State`

Introduce parallel execution and persistence

Stage 2 — Parallel simulation

One coordinator owns tick progression; multiple goroutines calculate independent region results.

Region A

Worker

Region B

Worker

Region C

Worker

Replace local worker calls with a protocol

Stage 3 — Distributed workers

Coordinator

Tick authority, leases, commit decisions, checkpoints

Worker A

Worker B

Worker C

Workers own assigned regions, compute results, and exchange versioned messages. A tick commits only after all required region results are accepted.

The crucial architectural seam is the region worker interface. A local implementation and a remote implementation should eventually implement the same contract.

Do not begin with a distributed database or a general-purpose message broker. For the first distributed prototype, a coordinator and workers communicating over gRPC are enough.

### Component responsibilities

| Component     | Responsibility                                          |
| ------------- | ------------------------------------------------------- |
| `sim`         | World state, entities, rules, deterministic transitions |
| `scheduler`   | Tick lifecycle, cancellation, pacing, pause and resume  |
| `partition`   | Spatial ownership and neighbor relationships            |
| `coordinator` | Worker assignment, barriers, result validation, commit  |
| `worker`      | Region-local computation and protocol handling          |
| `checkpoint`  | Consistent snapshots and recovery metadata              |
| `transport`   | Worker RPCs and protocol versioning                     |
| `api`         | External control and observation endpoints              |
| `metrics`     | Tick timings, retries, queue depth, throughput          |

The `sim` package should remain usable in tests without starting an HTTP server, connecting to PostgreSQL, or launching a worker process.

## 3. Core domain model

The simulation should use integer grid coordinates and discrete time. Avoid continuous physics, floating-point positions, and arbitrary entity scripting in the first version.

### World state

The world is a rectangular grid divided into fixed-size regions. Each entity has a globally unique identifier, a position, and a type-specific state.

I'd use a map for entity lookup and a separate spatial index only if profiling shows that scanning entities is too slow. Prematurely maintaining multiple indexes increases the risk of inconsistent state.

A possible Go model:

```

package sim

type EntityID uint64
type RegionID uint32
type Tick uint64

type Position struct {
    X int32 `json:"x"`
    Y int32 `json:"y"`
}

type EntityKind uint8

const (
    Plant EntityKind = iota + 1
    Herbivore
    Predator
)

type Entity struct {
    ID       EntityID  `json:"id"`
    Kind     EntityKind `json:"kind"`
    Position Position  `json:"position"`

    Energy   int64 `json:"energy"`
    Age      uint32 `json:"age"`
    Health   int32 `json:"health"`

    // Incremented whenever this entity's committed state changes.
    Version uint64 `json:"version"`
}

type WorldConfig struct {
    Width              int32  `json:"width"`
    Height             int32  `json:"height"`
    RegionWidth        int32  `json:"region_width"`
    RegionHeight       int32  `json:"region_height"`
    InitialPlants      int    `json:"initial_plants"`
    InitialHerbivores  int    `json:"initial_herbivores"`
    InitialPredators   int    `json:"initial_predators"`
    Seed               uint64 `json:"seed"`
}

type World struct {
    ID         string
    Tick       Tick
    Config     WorldConfig
    Entities   map[EntityID]Entity
    NextEntity EntityID
}

```

This is a starting model, not the final persistence schema. In production code, I'd also define validation, serialization rules, and explicit constructors rather than allowing arbitrary callers to create invalid worlds.

### Design decisions

Entity IDs must be deterministic. Assign IDs from a monotonic counter owned by the coordinator, or allocate deterministic ID ranges per region. Don't generate IDs using random UUIDs if those IDs affect canonical state hashes.

Use integer resource values initially. For example, energy can be measured in fixed units rather than floating-point quantities. This makes resource accounting and deterministic comparisons easier.

Treat entity versions as committed-state versions. An entity's version increases only when a new state is committed. A worker cannot silently overwrite a newer version using an older observation.

Separate world metadata from region state. The world has a global tick and configuration; each region has a local entity set, ownership metadata, and state version.

### Region and ownership model

```

package partition

import "simworld/internal/sim"

type Region struct {
    ID        sim.RegionID
    MinX      int32
    MinY      int32
    MaxX      int32 // exclusive
    MaxY      int32 // exclusive
    OwnerID   string
    Epoch     uint64
    StateTick sim.Tick
}

func (r Region) Contains(p sim.Position) bool {
    return p.X >= r.MinX && p.X < r.MaxX &&
        p.Y >= r.MinY && p.Y < r.MaxY
}

```

`Epoch` is the region's fencing token. It increases when ownership changes. A result from an old owner must be rejected even if that worker reconnects and attempts to submit stale work.

For the initial world, region boundaries are fixed. Dynamic repartitioning is a later feature because transferring ownership while the simulation is running adds a second consistency problem.

## 4. Deterministic tick semantics

This is the most important part of the design.

A simulation tick is not just a function that mutates entities. It is a transaction-like state transition from one committed world state to the next.

Define the transition as:

[ S{t+1} = F(S_t, C_t, R_t) ]

where:

- (S_t) is the committed world state at tick (t).
- (C_t) is the ordered set of external commands accepted for that tick.
- (R_t) is the deterministic random input for that tick.
- (F) is the simulation's transition function.

For identical inputs and a fixed ruleset, (F) should produce the same output.

### The six-stage tick

### 1

Freeze

Capture the committed state for tick t. No worker may mutate it.

### 2

Observe and plan

Each region reads the same logical tick snapshot and generates proposed actions.

### 3

Collect

The coordinator waits for one valid result from each required region.

### 4

Resolve

Sort actions by a documented total ordering; resolve resource and movement conflicts.

### 5

Validate and commit

Check invariants, build S(t+1), persist the commit record and state.

### 6

Publish

Expose the new tick to observers and accept the next tick's commands.

The state visible to clients must never be a partially committed tick. A client sees tick (t), then tick (t+1), not a mixture of both.

### Example: two herbivores, one plant

Suppose herbivores A and B both try to consume the last unit of a plant.

Each worker proposes an action based on its observations. The engine resolves the proposals using a stable ordering, for example:

1. Action priority.
2. Target entity ID.
3. Acting entity ID.
4. Action sequence number.

If A wins the conflict, A consumes the available resource and B receives no energy from that plant. The exact policy can change, but it must be explicit and independent of network arrival order.

The coordinator must also validate that each action was legal against the tick snapshot. It cannot trust worker results merely because they came from an assigned worker.

### Deterministic randomness

Do not share a single mutable random number generator across concurrent goroutines. The order in which goroutines consume random numbers could change the result.

Instead, derive random streams or random values from stable inputs, such as:

- World seed.
- Tick number.
- Entity ID.
- Decision index.
- Ruleset version.

A counter-based deterministic random function is particularly useful here. It allows an entity's random decision to remain independent of which worker computes it.

### What determinism does and does not guarantee

Deterministic rules alone do not guarantee that every distributed execution matches the single-process execution.

You must also preserve:

- The same input snapshot.
- The same action ordering.
- The same conflict-resolution policy.
- The same random values.
- The same rule and configuration versions.
- The same entity ownership and transfer semantics.

For Go, keep integer arithmetic in the authoritative state transitions where practical. If later introducing floating-point calculations, define the supported platform and numerical reproducibility requirements explicitly.

## 5. Distributed execution protocol

Once the sequential simulator is correct, distribute the computation while keeping the coordinator authoritative.

### Worker interface

The first worker API can be a Go interface:

```

package worker

import (
    "context"
    "simworld/internal/sim"
)

type RegionInput struct {
    WorldID      string
    Tick         sim.Tick
    RegionID     sim.RegionID
    RegionEpoch  uint64
    RulesetHash  string
    SnapshotHash string
    Entities     []sim.Entity
}

type RegionResult struct {
    WorldID     string
    Tick        sim.Tick
    RegionID    sim.RegionID
    RegionEpoch uint64
    SnapshotHash string

    Actions     []sim.Action
    ResultHash  string
}

type Executor interface {
    Compute(
        ctx context.Context,
        input RegionInput,
    ) (RegionResult, error)
}

```

`sim.Action` would be a type defined in the simulation package, containing a tagged action kind and validated payload. The input and result types should be plain data, not pointers to mutable world state.

Implement two versions:

- `LocalExecutor`: runs region calculations inside the process.
- `RemoteExecutor`: sends requests to a remote worker over gRPC.

Both must implement the same logical contract. That lets you compare execution modes without changing the simulation rules.

### One distributed tick

For the initial distributed version, the coordinator waits for every required region result. If a worker fails, the tick remains uncommitted while the coordinator retries the work on a healthy worker.

### Idempotency and stale messages

Each result is associated with a logical work identity:

`(world_id, tick, region_id, region_epoch, snapshot_hash)`

The coordinator must reject results that don't match the current work assignment.

If a worker sends the same result twice, the second result must not cause another state transition. The coordinator can accept a duplicate if its canonical result hash matches the already accepted result; a conflicting duplicate is an error that should be logged and surfaced.

A worker that has lost its lease or has an old region epoch must not be able to commit data.

These rules are more important than choosing between HTTP and gRPC. gRPC is a good fit for the eventual typed worker protocol, but it does not itself provide consistency or exactly-once processing.

### Worker failures and retries

The first failure policy should be simple:

- Worker crashes before returning a result: retry on another worker.
- Response is lost after computation: resend the same logical request.
- Duplicate response: deduplicate.
- Stale response from an old epoch: reject.
- Invalid or inconsistent response: fail the tick and preserve the last committed state.
- Coordinator crashes before commit: recover the last durable committed tick and recompute incomplete work.
- Coordinator crashes after a durable commit: recover the commit record and do not apply the tick again.

For the first implementation, only one coordinator should be active for a world. High availability with competing coordinators is a separate phase requiring leader election or another authoritative coordination mechanism.

## 6. Persistence and crash recovery

A distributed simulation needs to distinguish three things:

1. Work that has been computed.
2. State that has been durably committed.
3. State that has been published to observers.

These events can happen at different times. Treating them as equivalent creates recovery bugs.

### Initial storage design

Use PostgreSQL for durable world metadata, checkpoint records, and committed state. Keep the active simulation state in memory.

Suggested tables:

| Table          | Purpose                                                   |
| -------------- | --------------------------------------------------------- |
| `worlds`       | World ID, configuration, seed, status, ruleset hash       |
| `regions`      | Region bounds, owner, epoch, committed tick               |
| `checkpoints`  | Tick, schema version, checksum, snapshot location         |
| `tick_commits` | Committed tick, input hash, output hash, commit timestamp |
| `world_events` | Optional bounded event history for debugging and analysis |

I would not store a full row for every entity on every tick in the first release. That creates unnecessary write amplification. Start with compressed or serialized snapshots, and introduce more detailed persistence only when the requirements justify it.

### Checkpoint structure

A checkpoint should contain enough information to reconstruct the exact committed world:

- World ID and committed tick.
- World configuration and random seed.
- Entity state and next entity ID.
- Ruleset version or hash.
- Partition layout and region epochs.
- State checksum.
- Serialization schema version.

If the simulator uses mutable random-generator state, include it as well. With counter-based randomness, the seed and deterministic input scheme may be sufficient.

### Atomic commit strategy

For a single-process prototype, the commit can simply replace the in-memory state after validation.

For PostgreSQL-backed execution, use a transaction to write the tick commit record and the associated checkpoint or durable state reference. Only expose the tick after the transaction succeeds.

A practical initial protocol is:

1. Calculate the next state without mutating the committed state.
2. Validate the next state and calculate its canonical hash.
3. Persist the checkpoint and commit record in one database transaction, or use an immutable checkpoint object that was durably written before recording its reference.
4. Update the coordinator's committed tick.
5. Publish the new state to observers.

If checkpoint data is stored outside PostgreSQL, the object must be durable before the transaction references it. A failed transaction may leave an orphaned object, which is acceptable and can be garbage-collected later.

For the distributed version, persist enough information to recover the commit decision and the region results or checkpoint required to reconstruct it. Avoid a design where some regions advance durably while others remain on the old tick.

### Recovery contract

The recovery contract should be explicit:

> After a coordinator restart, the world resumes from the last durable committed tick. Uncommitted work may be recomputed, but a committed tick must never be applied twice.

This is a more realistic and useful guarantee than claiming exactly-once message delivery.

## 7. Spatial partitioning and boundary interactions

Partitioning determines which worker computes which part of the world.

Start with a regular rectangular grid. For example, a 256 × 256 world with 64 × 64 regions has 16 regions.

The correct number of regions depends on workload and overhead; this is only a sample configuration.

### Ownership rules

- Every region has exactly one authoritative owner per ownership epoch.
- Every entity belongs to exactly one region according to its committed position.
- Only the coordinator commits ownership changes.
- A region transfer takes effect at a tick boundary.
- Workers may read neighboring state needed for computation, but cannot independently mutate another region's committed entities.

### Movement across a boundary

Suppose a herbivore moves from Region A into Region B.

During tick (t), its movement is a proposed action. The coordinator validates the destination, resolves collisions, and determines its new position. At commit, the entity is assigned to Region B, and the ownership update becomes effective for tick (t+1).

This avoids a dangerous situation in which both workers believe they own the same creature.

### Neighboring state

Predators may need to inspect herbivores just across a region boundary. There are two reasonable approaches:

- Halo snapshots: send read-only copies of entities in a border area to neighboring workers.
- Coordinator-provided observations: the coordinator constructs the relevant observation data for each worker.

I'd start with coordinator-provided observations. It's simpler to validate. Optimize with halo snapshots when profiling shows the coordinator is doing too much work.

The initial implementation should use fixed regions and no dynamic load balancing. Once correctness is established, region migration and uneven workloads become meaningful experiments.

## 8. API and protocol design

The public API and worker protocol serve different purposes.

### External control API

Use REST for world lifecycle and inspection, and WebSockets later for streaming state updates.

| Method and path                    | Purpose                          |
| ---------------------------------- | -------------------------------- |
| `POST /v1/worlds`                  | Create a world                   |
| `GET /v1/worlds/{id}`              | Read status and committed tick   |
| `GET /v1/worlds/{id}/state`        | Read a consistent state snapshot |
| `POST /v1/worlds/{id}/start`       | Start the simulation             |
| `POST /v1/worlds/{id}/pause`       | Pause at a tick boundary         |
| `POST /v1/worlds/{id}/step`        | Advance one tick while paused    |
| `POST /v1/worlds/{id}/checkpoints` | Request a checkpoint             |
| `POST /v1/worlds/{id}/restore`     | Restore a selected checkpoint    |

A `step` request should return only after the tick commits, or return a clear asynchronous job identifier if you later support long-running requests.

The `state` endpoint must read from a committed snapshot. It should never expose partially updated entity state.

### Internal worker protocol

Use gRPC and Protocol Buffers when remote workers are introduced. The main operations are conceptually:

- `ComputeRegionTick`
- `GetWorkerStatus`
- `CancelRegionTick`
- `HealthCheck`

Include the world ID, tick, region ID, ownership epoch, ruleset hash, and snapshot hash in the computation request. Include the same identity in the response.

Add deadlines, bounded message sizes, cancellation, and protocol versioning. Avoid transmitting the entire world to every worker on every tick; start with region-local state plus the minimum neighbor observations needed for the rules.

## 9. Test strategy: correctness before throughput

Testing should be designed around the distributed guarantees, not just individual functions.

### Layer 1 — Unit tests

Test deterministic rules in isolation:

- Movement respects world boundaries.
- Energy cannot become negative.
- Dead creatures cannot act.
- Plants regenerate according to the rules.
- Consumption cannot exceed available resources.
- Reproduction produces unique IDs.
- Every entity belongs to exactly one region.
- Invalid actions are rejected.

Use Go's standard `testing` package. Use table-driven tests for rules and conflict-resolution cases.

### Layer 2 — Determinism and property tests

Run a world for N ticks twice with the same configuration and seed. Compare canonical state hashes at every tick.

Also test that:

- Changing the seed changes some outcomes for a suitably stochastic scenario.
- Changing worker scheduling does not change committed state.
- Sequential and parallel modes produce identical canonical state.
- Serialization and restoration preserve the state hash.
- The same set of actions in a different network arrival order produces the same result.

For randomized property testing, Go's native fuzzing support can generate small worlds, actions, and edge cases. Keep seeds and failing inputs so bugs can be reproduced.

### Layer 3 — Distributed protocol tests

Build an in-process fake worker that can inject failures deterministically.

| Fault injection                    | Expected behavior                                    |
| ---------------------------------- | ---------------------------------------------------- |
| Drop a response                    | Request is retried; tick is not duplicated           |
| Delay a response                   | Stale or late result is handled safely               |
| Send a duplicate response          | No duplicate state transition                        |
| Send conflicting duplicate results | Tick is rejected or failed visibly                   |
| Return an old epoch                | Result is rejected                                   |
| Crash one worker                   | Work is reassigned; committed state stays intact     |
| Restart coordinator                | Recovery starts from the last durable commit         |
| Corrupt a checkpoint               | Checksum validation prevents restoring invalid state |

Later, add integration tests that launch actual worker processes and terminate them during a tick.

### Layer 4 — Benchmarks

Use `go test -bench` to establish a baseline for the simulation core.

Measure:

- Ticks per second.
- Entities processed per second.
- Time spent generating actions.
- Time spent resolving conflicts.
- Checkpoint latency.
- Serialization and network overhead.
- Coordinator CPU and memory use.
- Worker utilization.
- Recovery time after a worker failure.

Run identical workloads with fixed seeds and configurations. Report the workload size, hardware, Go version, and number of workers.

A useful comparison is sequential versus parallel versus distributed. Distributed execution should be measured against a baseline, not assumed to be faster.

### Core acceptance invariant

For the same initial state and logical inputs:

[ H(S_N^{\text{sequential}}) = H(S_N^{\text{distributed}}) ]

Here, (H) is a canonical hash of the committed state after N ticks.

This equality is a powerful end-to-end test. It does not replace checking intermediate ticks, but it provides a compact way to detect divergence.

One important qualification: compare only execution modes that implement the same simulation semantics. If a later asynchronous mode intentionally changes the timing or ordering of interactions, it may require a different correctness contract.

## 10. Repository structure

Use a Go module with a modular monolith initially. Keep package dependencies pointing inward: the simulation core should not import the API, PostgreSQL driver, or gRPC runtime.

```
simworld/
├── cmd/
│   ├── simworld/
│   │   └── main.go
│   └── simworker/             # Added in distributed phase
│       └── main.go
├── internal/
│   ├── sim/
│   │   ├── world.go
│   │   ├── entity.go
│   │   ├── action.go
│   │   ├── rules.go
│   │   ├── transition.go
│   │   ├── random.go
│   │   └── hash.go
│   ├── scheduler/
│   │   └── scheduler.go
│   ├── partition/
│   │   ├── grid.go
│   │   └── ownership.go
│   ├── worker/
│   │   ├── executor.go
│   │   └── local.go
│   ├── coordinator/           # Added as needed
│   ├── checkpoint/
│   │   ├── snapshot.go
│   │   └── store.go
│   ├── api/
│   │   ├── routes.go
│   │   └── handlers.go
│   └── persistence/
│       └── postgres.go        # Later milestone
├── proto/                     # Distributed phase
│   └── worker/v1/
├── migrations/                # Persistence phase
├── web/                       # Later visualization client
├── testdata/
├── docs/
│   ├── architecture.md
│   ├── tick-semantics.md
│   ├── consistency-model.md
│   └── adr/
├── go.mod
├── go.sum
├── Makefile
├── Dockerfile
└── README.md
```

This is the eventual structure, not a mandate to create every directory on day one. Begin with `sim`, `partition`, `worker`, `checkpoint`, and `cmd/simworld`. Add packages when there is real behavior to put in them.

### Dependency direction

The important rule is that the core should not depend on how it is hosted.

You should be able to import `internal/sim` into a test, create a world, run 1,000 ticks, and inspect the result without starting any services.

## 11. Scoped first implementation: v0.1

The first implementation should be a complete, testable single-process ecosystem—not a distributed skeleton.

## v0.1 completion criteria

A runnable Go application with reproducible simulation results.

Initialize Go module and CLI

Create the module, configuration validation, and a run command.

Implement the world model

Add entity types, integer energy, grid positions, and deterministic IDs.

Implement the tick transition

Use immutable input state, action generation, deterministic conflict resolution, and commit.

Add the ecosystem rules

Plant growth, herbivore consumption, predator hunting, aging, and reproduction.

Add deterministic randomness

Use a seed-based scheme independent of goroutine scheduling.

Implement canonical state hashing

Serialize entities in stable ID order and hash the canonical representation.

Add JSON checkpoint save/load

Persist the complete state and validate it when restoring.

Build the HTTP control API

Create a world, inspect state, pause, resume, and step one tick.

Add automated correctness tests

Test invariants, reproducibility, checkpoint round trips, and invalid actions.

Publish baseline benchmarks

Measure tick throughput for several entity counts.

### Implementation sequence

Step 1: Create the simulation core.

Start with a function that advances a world one tick. It should accept a state snapshot and produce a next state, rather than modifying the input in place.

Conceptually:

```
func Step(
    current World,
    commands []Command,
) (World, []Event, error)
```

You will need to define `Command` and `Event` as well as the rules. Later, refactor this into a region-oriented transition interface. Do not introduce distributed interfaces until the state transition has a clear contract.

Step 2: Implement deterministic action resolution.

Use two phases: first generate proposed actions from the snapshot, then resolve those actions in a stable order. Ensure that resource consumption, movement, death, and reproduction don't accidentally depend on Go map iteration order.

Step 3: Test reproducibility.

Create a seeded world, advance it 1,000 ticks, and record the hash after every tick. Repeat the run and verify the hashes match.

Step 4: Add checkpointing.

Save the world to a versioned JSON snapshot. Load it into a fresh process or test instance and verify that continuing the simulation produces the same subsequent hashes as an uninterrupted run.

Step 5: Add a minimal API.

Only after the core works, expose the simulation through HTTP. Use a single scheduler goroutine to serialize control commands and tick advancement. Avoid concurrent handlers mutating the world directly.

Step 6: Measure.

Run benchmarks with increasing entity counts. Profile the code before optimizing. The first benchmark establishes the baseline against which later parallel and distributed implementations will be judged.

## 12. The first concurrency model

Go makes it easy to launch goroutines, but that does not mean every part of the simulation should be concurrent.

For v0.1, use a single goroutine to own committed world state and process commands. This provides a straightforward serialization point for `step`, `pause`, `resume`, and checkpoint operations.

Later, parallelize the expensive computation phase:

1. Freeze the tick snapshot.
2. Assign independent regions to goroutines.
3. Let each goroutine produce its own action list.
4. Wait for all results.
5. Merge and sort the actions deterministically.
6. Resolve conflicts and commit on the coordinator goroutine.

Workers must never mutate the same entity map concurrently.

This is a good place to use Go's race detector:

```
go test -race ./...
```

Also use:

```
go vet ./...
go test ./...
go test -bench=. -benchmem ./...
```

These tools serve different purposes: the race detector finds data races, `vet` catches suspicious constructs, and benchmarks measure performance. Passing them doesn't prove distributed correctness, but they are useful baseline checks.

## 13. Milestones beyond v0.1

Once the first version is stable, I'd build the distributed capabilities in this order.

| Version | Main capability               | Exit criterion                                        |
| ------- | ----------------------------- | ----------------------------------------------------- |
| v0.1    | Deterministic ecosystem       | Repeatable runs, tests, checkpoints, API              |
| v0.2    | Parallel region execution     | Same state hashes as sequential execution             |
| v0.3    | Coordinator and local workers | Versioned region results and barrier-based ticks      |
| v0.4    | Remote worker processes       | gRPC execution across separate processes              |
| v0.5    | Failure recovery              | Worker retries, fencing, coordinator restart recovery |
| v0.6    | Scaling and observability     | Benchmarks, metrics, and documented bottlenecks       |
| v0.7    | Optional visualization        | Live map, population charts, worker status            |

The sequence is intentional. In particular, do not begin by implementing leader election, dynamic region migration, or asynchronous ticks. Those features add complexity before you have evidence that the basic distributed execution protocol is correct.

## 14. The hardest risks to watch

Nondeterminism hidden in the implementation

Map iteration, random-number consumption, and conflict resolution can all cause runs to diverge. Establish stable ordering and hash-based tests early.

Coordinator bottlenecks

If every worker sends every action to one coordinator, the coordinator may become the limiting factor. Measure action volume, serialization cost, and resolution time before redesigning the protocol.

Checkpointing becomes expensive

Full snapshots can dominate runtime as the world grows. Start simple, then benchmark snapshot compression, snapshot frequency, and incremental persistence.

The distributed version changes the rules

If workers see different snapshots or resolve actions independently, a faster simulation may no longer represent the same world. Make the consistency model explicit and compare results against the sequential reference.

## 15. What I would do first

I would start with these four deliverables, in order:

1. A concise `docs/tick-semantics.md` defining the transition rules, action ordering, and commit boundary.
2. A minimal Go simulation core with seeded world creation and one deterministic `Step` function.
3. A test that runs the same world twice for 1,000 ticks and compares every state hash.
4. A checkpoint round-trip test proving that save, restore, and continued execution preserve the expected results.

Those four deliverables establish the foundation for everything else. They also give you a useful checkpoint before spending time on HTTP, visualization, or remote workers.

The most important design principle: build a correct sequential reference implementation first, then make the execution distributed without changing the simulation's logical semantics. That gives you a clear correctness oracle, a performance baseline, and a coherent story about the engineering problems the project solves.