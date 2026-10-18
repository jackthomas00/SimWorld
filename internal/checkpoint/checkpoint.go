// Package checkpoint saves sim checkpoints to versioned JSON files and loads
// them back.
//
// Decode checks integrity: the format version, the shape of the file and the
// stored state hash. Semantic validation of the state is left to sim.Restore,
// which applies the same checks as a commit.
package checkpoint

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/jackthomas00/simworld/internal/sim"
)

// Format is the file format version. Bump it whenever file changes shape or a
// type it embeds changes its JSON encoding.
const Format = 1

var (
	ErrFormat       = errors.New("unsupported checkpoint format")
	ErrHashMismatch = errors.New("checkpoint state does not match its hash")
)

// file is the on-disk layout. Entities are stored as lists in ID-slice order,
// which rebuilds both the slices and the maps of sim.WorldState, so the two
// can never disagree in a file.
//
// The hash covers the world state only. Config, rules and behavior are
// checked when the restored world validates them.
type file struct {
	Format     int                `json:"format"`
	Hash       string             `json:"hash"`
	WorldID    string             `json:"world_id"`
	Config     sim.WorldConfig    `json:"config"`
	Rules      sim.Rules          `json:"rules"`
	Behavior   *sim.BehaviorRules `json:"behavior"`
	Tick       sim.Tick           `json:"tick"`
	NextEntity sim.EntityID       `json:"next_entity"`
	Creatures  []sim.Creature     `json:"creatures"`
	Plants     []sim.Plant        `json:"plants"`
}

// Encode writes cp to w.
func Encode(w io.Writer, cp sim.Checkpoint) error {
	state := cp.State
	f := file{
		Format:     Format,
		Hash:       state.Hash().String(),
		WorldID:    cp.ID,
		Config:     cp.Config,
		Rules:      cp.Rules,
		Behavior:   cp.Behavior,
		Tick:       state.Tick,
		NextEntity: state.NextEntity,
		Creatures:  make([]sim.Creature, 0, len(state.CreatureIDs)),
		Plants:     make([]sim.Plant, 0, len(state.PlantIDs)),
	}
	for _, id := range state.CreatureIDs {
		c, ok := state.Creatures[id]
		if !ok {
			return fmt.Errorf("creature %d is in the ID slice but not the map", id)
		}
		f.Creatures = append(f.Creatures, c)
	}
	for _, id := range state.PlantIDs {
		p, ok := state.Plants[id]
		if !ok {
			return fmt.Errorf("plant %d is in the ID slice but not the map", id)
		}
		f.Plants = append(f.Plants, p)
	}
	return json.NewEncoder(w).Encode(f)
}

// Decode reads one checkpoint from r and verifies its hash.
func Decode(r io.Reader) (sim.Checkpoint, error) {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()

	var f file
	if err := dec.Decode(&f); err != nil {
		return sim.Checkpoint{}, fmt.Errorf("decode checkpoint: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return sim.Checkpoint{}, errors.New("decode checkpoint: unexpected data after checkpoint")
	}
	if f.Format != Format {
		return sim.Checkpoint{}, fmt.Errorf("%w: %d, want %d", ErrFormat, f.Format, Format)
	}

	state := sim.WorldState{
		Tick:        f.Tick,
		NextEntity:  f.NextEntity,
		CreatureIDs: make([]sim.EntityID, 0, len(f.Creatures)),
		Creatures:   make(map[sim.EntityID]sim.Creature, len(f.Creatures)),
		PlantIDs:    make([]sim.EntityID, 0, len(f.Plants)),
		Plants:      make(map[sim.EntityID]sim.Plant, len(f.Plants)),
	}
	// Duplicate IDs collapse in the map but stay in the slice, where
	// sim.Restore's validation rejects them.
	for _, c := range f.Creatures {
		state.CreatureIDs = append(state.CreatureIDs, c.ID)
		state.Creatures[c.ID] = c
	}
	for _, p := range f.Plants {
		state.PlantIDs = append(state.PlantIDs, p.ID)
		state.Plants[p.ID] = p
	}
	if got := state.Hash().String(); got != f.Hash {
		return sim.Checkpoint{}, fmt.Errorf("%w: stored %s, computed %s", ErrHashMismatch, f.Hash, got)
	}

	return sim.Checkpoint{
		ID:       f.WorldID,
		Config:   f.Config,
		Rules:    f.Rules,
		Behavior: f.Behavior,
		State:    state,
	}, nil
}

// Save writes cp to path atomically: readers see either the previous file or
// the complete new one, even after a crash. The data is written to a temporary
// file in the same directory, synced, and renamed over path, and then the
// directory is synced so the rename itself is durable.
func Save(path string, cp sim.Checkpoint) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("save checkpoint: %w", err)
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()

	buf := bufio.NewWriter(tmp)
	if err := Encode(buf, cp); err != nil {
		return fmt.Errorf("save checkpoint: %w", err)
	}
	if err := buf.Flush(); err != nil {
		return fmt.Errorf("save checkpoint: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("save checkpoint: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("save checkpoint: %w", err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("save checkpoint: %w", err)
	}
	return syncDir(dir)
}

// Load reads and verifies the checkpoint at path. Pass the result to
// sim.Restore to get a running world.
func Load(path string) (sim.Checkpoint, error) {
	f, err := os.Open(path)
	if err != nil {
		return sim.Checkpoint{}, fmt.Errorf("load checkpoint: %w", err)
	}
	defer f.Close()

	return Decode(bufio.NewReader(f))
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("save checkpoint: %w", err)
	}
	defer d.Close()

	if err := d.Sync(); err != nil {
		return fmt.Errorf("save checkpoint: sync directory: %w", err)
	}
	return nil
}
