package checkpoint

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jackthomas00/simworld/internal/sim"
)

// testWorld returns a 24x24 world populated at DefaultExperiment's densities
// and driven by the default behavior. Seed 42 keeps all three kinds alive for
// the first few hundred ticks.
func testWorld(t *testing.T) *sim.World {
	t.Helper()

	e := sim.DefaultExperiment()
	config := e.Config
	config.Width, config.Height = 24, 24
	config.RegionWidth, config.RegionHeight = 24, 24
	config.InitialPlants, config.InitialHerbivores, config.InitialPredators = 108, 28, 4
	config.Seed = 42

	w, err := sim.NewPopulatedWorld("checkpoint", config, e.Initial, sim.WithRules(e.Rules), sim.WithBehavior(e.Behavior))
	if err != nil {
		t.Fatalf("NewPopulatedWorld: %v", err)
	}
	return w
}

func step(t *testing.T, w *sim.World, ticks int) {
	t.Helper()
	for range ticks {
		if err := w.Step(); err != nil {
			t.Fatalf("tick %d: %v", w.Tick()+1, err)
		}
	}
}

func checkpointOf(t *testing.T, w *sim.World) sim.Checkpoint {
	t.Helper()
	cp, err := w.Checkpoint()
	if err != nil {
		t.Fatalf("Checkpoint: %v", err)
	}
	return cp
}

// TestSaveLoadResumesIdentically is the milestone's exit test: a run that is
// saved to disk, loaded and resumed matches an uninterrupted run at every tick.
func TestSaveLoadResumesIdentically(t *testing.T) {
	const n, m = 150, 150

	reference := testWorld(t)
	want := make([]sim.StateHash, 0, n+m)
	for range n + m {
		step(t, reference, 1)
		want = append(want, reference.StateHash())
	}

	original := testWorld(t)
	step(t, original, n)
	path := filepath.Join(t.TempDir(), "world.json")
	if err := Save(path, checkpointOf(t, original)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	cp, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	restored, err := sim.Restore(cp)
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if restored.StateHash() != want[n-1] {
		t.Fatalf("loaded hash differs from the original at tick %d", n)
	}
	for tick := n + 1; tick <= n+m; tick++ {
		step(t, restored, 1)
		if restored.StateHash() != want[tick-1] {
			t.Fatalf("resumed world diverged at tick %d", tick)
		}
	}
}

func TestLoadPreservesCheckpoint(t *testing.T) {
	w := testWorld(t)
	step(t, w, 20)
	want := checkpointOf(t, w)

	var buf bytes.Buffer
	if err := Encode(&buf, want); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(&buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}

	if got.State.Hash() != want.State.Hash() {
		t.Fatalf("state hash changed across encoding")
	}
	got.State, want.State = sim.WorldState{}, sim.WorldState{}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("checkpoint changed across encoding:\n got %+v\nwant %+v", got, want)
	}
}

func TestLoadWithoutBehavior(t *testing.T) {
	config := sim.WorldConfig{Width: 4, Height: 4, RegionWidth: 4, RegionHeight: 4}
	cp := checkpointOf(t, sim.NewWorld("empty", config))

	var buf bytes.Buffer
	if err := Encode(&buf, cp); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(&buf)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.Behavior != nil {
		t.Fatalf("decoded behavior %+v, want nil", *got.Behavior)
	}
	if _, err := sim.Restore(got); err != nil {
		t.Fatalf("Restore: %v", err)
	}
}

// encoded returns a valid checkpoint file, modified by edit.
func encoded(t *testing.T, edit func(*file)) []byte {
	t.Helper()

	var buf bytes.Buffer
	if err := Encode(&buf, checkpointOf(t, testWorld(t))); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	var f file
	if err := json.Unmarshal(buf.Bytes(), &f); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	edit(&f)
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	return data
}

func TestDecodeRejectsCorruptFiles(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{"tampered energy", encoded(t, func(f *file) { f.Creatures[0].Energy++ }), ErrHashMismatch.Error()},
		{"tampered tick", encoded(t, func(f *file) { f.Tick++ }), ErrHashMismatch.Error()},
		{"reordered plants", encoded(t, func(f *file) { f.Plants[0], f.Plants[1] = f.Plants[1], f.Plants[0] }), ErrHashMismatch.Error()},
		{"future format", encoded(t, func(f *file) { f.Format = Format + 1 }), ErrFormat.Error()},
		{"unknown field", append(bytes.TrimSuffix(encoded(t, func(*file) {}), []byte("}")), []byte(`,"extra":1}`)...), "unknown field"},
		{"trailing data", append(encoded(t, func(*file) {}), []byte(` {}`)...), "unexpected data"},
		{"truncated", encoded(t, func(*file) {})[:100], "decode checkpoint"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(bytes.NewReader(tt.data))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Decode error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDecodeErrorsAreTyped(t *testing.T) {
	_, err := Decode(bytes.NewReader(encoded(t, func(f *file) { f.Tick++ })))
	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("Decode error = %v, want ErrHashMismatch", err)
	}
}

func TestSaveReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "world.json")
	w := testWorld(t)

	if err := Save(path, checkpointOf(t, w)); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	step(t, w, 5)
	if err := Save(path, checkpointOf(t, w)); err != nil {
		t.Fatalf("second Save: %v", err)
	}

	cp, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cp.State.Tick != 5 {
		t.Fatalf("loaded tick %d, want the second save's tick 5", cp.State.Tick)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries, want only the checkpoint", len(entries))
	}
}

func TestSaveFailureLeavesNoFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "world.json")

	// An ID in the slice without a map entry makes Encode fail mid-save.
	cp := checkpointOf(t, testWorld(t))
	cp.State.CreatureIDs = append(cp.State.CreatureIDs, 999999)
	if err := Save(path, cp); err == nil {
		t.Fatalf("Save of a broken state succeeded")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("failed Save left %d entries behind", len(entries))
	}
}
