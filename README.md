# SimWorld

A deterministic ecosystem simulator in Go, built to grow from one process into a distributed system of simulation workers.

The ecosystem (plants, herbivores, predators on a grid) is the workload. The engineering goals are these:

- deterministic, atomic ticks;
- reproducible runs;
- checkpoints and recovery;
- eventually, a world partitioned across workers that still produces the same results as a single process.

## Running

```bash
go test ./...                     # full suite (about 30s); -short skips the long ecosystem test
go test -race ./...               # includes concurrent Step/reader tests
go run ./cmd/simworld -seeds 1-8  # run the default ecosystem across seeds
go run ./cmd/simworld -seeds 3 -trace -every 50
```

## Layout

| Path | Contents |
| ---- | -------- |
| `internal/sim` | World state, the tick pipeline (`transition.go`), per-phase resolvers, validation, decision randomness, behavior and experiment harness |
| `internal/checkpoint` | Versioned JSON checkpoint files: hash-verified load, atomic save |
| `cmd/simworld` | CLI for running seed sweeps |

## Docs

| Doc | Read it for |
| --- | ----------- |
| [docs/roadmap.md](docs/roadmap.md) | Where the project is and what comes next |
| [docs/tick-semantics.md](docs/tick-semantics.md) | The spec: what one tick does, in what order, and what is guaranteed |
| [docs/ecosystem-tuning.md](docs/ecosystem-tuning.md) | How the default rules were tuned, and the open ecosystem problems (parked) |
| [docs/design.md](docs/design.md) | Long-range design reference: distributed protocol, persistence, partitioning |
