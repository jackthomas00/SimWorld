# Ecosystem Tuning

How `DefaultRules`, `DefaultBehaviorRules` and `DefaultExperiment` were chosen, and what is still unsolved.

> **Status: parked (2026-10-18).** About 70-80% coexistence over 4000 ticks is good enough as a workload for the systems work. That work (hashing, checkpoints, replay, partitioning) is the point of this project. Do not re-tune unless a later milestone needs a longer-lived ecosystem. When you return, start from "Still unsolved" below.

Reproduce any result here with the CLI. Runs are deterministic, so the same flags and seeds give the same output:

```bash
go run ./cmd/simworld -seeds 1-8
go run ./cmd/simworld -seeds 9-24 -ticks 4000
go run ./cmd/simworld -seeds 3 -trace -every 50
```

## Result

On `DefaultExperiment` (80x80, 1200 plants, 320 herbivores, 48 predators), "coexisted" means plants, herbivores and predators were all still alive at the end of the run. All rows use the current defaults, including aging.

| Seeds | Ticks | Coexisted | Role |
| ----- | ----- | --------- | ---- |
| 1-8 | 2000 | 8/8 | tuning set; seeds 1-4 pinned by `TestDefaultEcosystemCoexists` |
| 25-40 | 4000 | 14/16 | used to choose the lifespans |
| 41-56 | 4000 | 13/16 | held out |
| 57-72 | 4000 | 10/16 | held out |

The honest estimate is the held-out seeds, roughly 70-80% coexistence over 4000 ticks. Before tuning began, every configuration tried went extinct, usually within 150 ticks.

`TestDefaultEcosystemCoexists` is skipped under `go test -short`.

## What failed, in order

Each step fixed the dominant failure and exposed the next one.

1. **Plants were non-renewable.** Nothing created plants, so grazing drove the count to zero and everything starved. *Fix:* seeding, then sprouting. Seeding alone was not enough: herbivores ate every plant before any reached the seeding threshold. Sprouting guarantees plants always recover.
2. **Idle predators never starved.** Only movement cost energy, so a fed predator could wait out a famine. *Fix:* metabolism (`MetabolismRules`), a per-tick upkeep. Predators pay 1. Herbivore metabolism 1 starved predators early in every run, so herbivores pay 0.
3. **Predators converted prey to offspring almost 1:1.** A kill yields about 12-20 energy, and a predator birth cost 10, shared with herbivores. *Fix:* per-kind `BirthRules`. A predator birth now costs 50, and the newborn starts with 25.
4. **Paradox of enrichment.** Abundant plants let herbivores fill more than a third of the grid. Packed herbivores cannot flee, so each predator kills every tick and predators overshoot. Two changes helped most:
   - **Larger world.** Moving from 40x40 to 80x80 at the same density was the single biggest improvement, because space lets local crashes recover from neighboring regions.
   - **Predator satiation** (`PredatorSatedAt`). A predator at or above it stops hunting, which caps the kill rate.

5. **Immortal herbivores filled the grid.** When predators died out, herbivores bred into every cell. *Change:* aging with per-kind lifespans (`AgingRules`): herbivores live 400 ticks, predators 800. Founders get random starting ages so they do not all die on the same tick.
   - Choosing a setting was a small sweep. A predator lifespan of 100 killed predators in every run by tick 100, because they could not reach the reproduction threshold within one lifetime.
   - Paired against no aging, 400/800 won 14/16 to 11/16 on seeds 25-40 but tied 13/16 to 13/16 on held-out seeds 41-56. The coexistence gain is modest and within noise.
   - Aging did **not** stop the takeover: after predators die, births still outpace deaths and herbivores refill the grid. It was kept because it is at least as good and gives creatures finite lives.

## Herbivore metabolism

Herbivores pay no per-tick upkeep. This is deliberate for now, not an oversight. With herbivore metabolism 1, coexistence on seeds 57-72 (4000 ticks) dropped from 10/16 to:

| Bite | Coexisted |
| ---- | --------- |
| 3 | 1/16 |
| 4 | 1/16 |
| 5 | 1/16 |
| 6 | 0/16 |

Herbivores thin out early, predators starve, mostly between ticks 70 and 600, and sometimes herbivores follow.

The cause is integer energy resolution. A bite is 3 energy, so the smallest possible upkeep, 1, is a third of a herbivore's intake, far harsher than the predator's 1 against kills worth 12-40. Real herbivore metabolism needs finer granularity: either scale all energy values up (for example 10x, then herbivore upkeep 1 against bites of 30), or charge upkeep every N ticks. Either is a re-tune of every energy-denominated default.

## A quirk worth knowing

The tuned `PredatorSatedAt` (30) is below `PredatorReproduceAt` (80). A predator stops hunting at 30, so it reaches 80 only by eating a herbivore that pushes it over in one meal. That happens with fat prey: crowded herbivores that have no free cell to reproduce into keep eating and building up energy. Predator births therefore depend on herbivore crowding. That is a plausible density-dependent feedback, but it emerged from the sweep rather than being designed. Changes to herbivore behavior may shift it.

## Still unsolved

The held-out failures fall into two groups:

- **Herbivores fill the grid after predators die.** Aging did not fix this: births outpace deaths. *Candidate fixes:* herbivore metabolism at finer energy resolution (see above), or density-dependent reproduction, for example requiring a minimum number of free neighboring cells.
- **Late predator booms** still wipe out herbivores, after which predators starve. *Candidate fix:* a predator gain smaller than the prey's full energy (conversion efficiency below 100%).

The remaining risk is a structural limit of small worlds: predator-prey models on finite grids eventually drift to extinction. Larger worlds push that further out, but they cost more time per tick.

## Method

Experiments ran through `Experiment.Run` with populations scaled to world area, 4-8 seeds per configuration, in parallel. The sweeps covered:

- reproduction thresholds and per-kind birth costs;
- metabolism per kind;
- flee and sense radius;
- satiation;
- starting predator energy;
- world size.

The best configuration was then checked on 16 seeds it had never seen, for twice as many ticks.
