// Command simworld runs population experiments with the default rules and
// reports how each population fared across seeds.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"

	"github.com/jackthomas00/simworld/internal/sim"
)

func main() {
	var (
		width      = flag.Int("width", 80, "world width")
		height     = flag.Int("height", 80, "world height")
		plants     = flag.Int("plants", 1200, "initial plants")
		herbivores = flag.Int("herbivores", 320, "initial herbivores")
		predators  = flag.Int("predators", 48, "initial predators")
		seeds      = flag.String("seeds", "1-8", "seeds to run: a list (1,4,9) or a range (1-8)")
		ticks      = flag.Int("ticks", 2000, "ticks per run")
		every      = flag.Int("every", 0, "with -trace, print a census every N ticks (default ticks/10)")
		trace      = flag.Bool("trace", false, "print each run's census samples")
		parallel   = flag.Int("parallel", runtime.GOMAXPROCS(0), "seeds to run at once")
	)
	flag.Parse()

	seedList, err := parseSeeds(*seeds)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	sampleEvery := *every
	if sampleEvery == 0 {
		sampleEvery = max(*ticks/10, 1)
	}

	exp := sim.DefaultExperiment()
	exp.Config.Width, exp.Config.Height = int32(*width), int32(*height)
	exp.Config.RegionWidth, exp.Config.RegionHeight = int32(*width), int32(*height)
	exp.Config.InitialPlants = *plants
	exp.Config.InitialHerbivores = *herbivores
	exp.Config.InitialPredators = *predators
	exp.Ticks = *ticks
	exp.SampleEvery = sampleEvery

	out := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.AlignRight)
	defer out.Flush()
	fmt.Fprintln(out, "seed\tplants\therbivores\tpredators\tresult\t")

	// Runs are independent and deterministic, so they can execute in any
	// order; results are printed in seed order.
	trials := make([]sim.Trial, len(seedList))
	errs := make([]error, len(seedList))
	var wg sync.WaitGroup
	slots := make(chan struct{}, max(*parallel, 1))
	for i, seed := range seedList {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			trials[i], errs[i] = exp.Run(seed)
		}()
	}
	wg.Wait()

	coexisted := 0
	for i, seed := range seedList {
		trial, err := trials[i], errs[i]
		if err != nil {
			out.Flush()
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if trial.Coexisted() {
			coexisted++
		}
		f := trial.Final
		fmt.Fprintf(out, "%d\t%d\t%d\t%d\t%s\t\n", seed, f.Plants, f.Herbivores, f.Predators, describe(trial))
		if *trace {
			for _, c := range trial.Samples {
				fmt.Fprintf(out, "  t=%d\t%d\t%d\t%d\t\t\n", c.Tick, c.Plants, c.Herbivores, c.Predators)
			}
		}
	}
	fmt.Fprintf(out, "\t\t\t\t%d/%d coexisted\t\n", coexisted, len(seedList))
}

func describe(t sim.Trial) string {
	var parts []string
	if t.PlantsExtinctAt > 0 {
		parts = append(parts, fmt.Sprintf("plants extinct @%d", t.PlantsExtinctAt))
	}
	if t.HerbivoresExtinctAt > 0 {
		parts = append(parts, fmt.Sprintf("herbivores extinct @%d", t.HerbivoresExtinctAt))
	}
	if t.PredatorsExtinctAt > 0 {
		parts = append(parts, fmt.Sprintf("predators extinct @%d", t.PredatorsExtinctAt))
	}
	if len(parts) == 0 {
		return "coexisted"
	}
	return strings.Join(parts, ", ")
}

func parseSeeds(s string) ([]uint64, error) {
	if lo, hi, isRange := strings.Cut(s, "-"); isRange {
		a, errA := strconv.ParseUint(lo, 10, 64)
		b, errB := strconv.ParseUint(hi, 10, 64)
		if errA != nil || errB != nil || a > b {
			return nil, fmt.Errorf("invalid seed range %q", s)
		}
		var seeds []uint64
		for seed := a; ; seed++ {
			seeds = append(seeds, seed)
			if seed == b {
				break
			}
		}
		return seeds, nil
	}
	var seeds []uint64
	for _, part := range strings.Split(s, ",") {
		seed, err := strconv.ParseUint(strings.TrimSpace(part), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid seed %q", part)
		}
		seeds = append(seeds, seed)
	}
	return seeds, nil
}
