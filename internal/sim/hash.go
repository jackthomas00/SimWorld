package sim

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// StateHash is the SHA-256 digest of a WorldState's canonical encoding.
type StateHash [sha256.Size]byte

func (h StateHash) String() string {
	return hex.EncodeToString(h[:])
}

// stateHashDomain prefixes every encoding. Bump the version whenever the
// layout below changes, so hashes from different layouts can never match.
const stateHashDomain = "simworld/state/v1"

// Hash returns the hash of the state's canonical encoding: a fixed big-endian
// layout with no padding, independent of map iteration order.
//
//	domain string
//	Tick u64, NextEntity u64
//	len(CreatureIDs) u64, then for each ID in slice order:
//	  ID u64, Kind u8, X i32, Y i32, Energy i64, Age u32, Health i32, Version u64
//	len(PlantIDs) u64, then for each ID in slice order:
//	  ID u64, X i32, Y i32, Energy i64, Age u32, Health i32, Version u64
//
// The ID slices are encoded in their stored order, not sorted, because that
// order decides phase iteration order and so is part of the state. Entities
// are read from the maps by ID; for a state that passes validation the slices
// and maps hold exactly the same IDs, so every entity is covered.
func (s WorldState) Hash() StateHash {
	const creatureSize, plantSize = 8 + 1 + 4 + 4 + 8 + 4 + 4 + 8, 8 + 4 + 4 + 8 + 4 + 4 + 8
	buf := make([]byte, 0, len(stateHashDomain)+8*4+
		creatureSize*len(s.CreatureIDs)+plantSize*len(s.PlantIDs))

	buf = append(buf, stateHashDomain...)
	buf = binary.BigEndian.AppendUint64(buf, uint64(s.Tick))
	buf = binary.BigEndian.AppendUint64(buf, uint64(s.NextEntity))

	buf = binary.BigEndian.AppendUint64(buf, uint64(len(s.CreatureIDs)))
	for _, id := range s.CreatureIDs {
		c := s.Creatures[id]
		buf = binary.BigEndian.AppendUint64(buf, uint64(id))
		buf = append(buf, byte(c.Kind))
		buf = binary.BigEndian.AppendUint32(buf, uint32(c.Position.X))
		buf = binary.BigEndian.AppendUint32(buf, uint32(c.Position.Y))
		buf = binary.BigEndian.AppendUint64(buf, uint64(c.Energy))
		buf = binary.BigEndian.AppendUint32(buf, c.Age)
		buf = binary.BigEndian.AppendUint32(buf, uint32(c.Health))
		buf = binary.BigEndian.AppendUint64(buf, c.Version)
	}

	buf = binary.BigEndian.AppendUint64(buf, uint64(len(s.PlantIDs)))
	for _, id := range s.PlantIDs {
		p := s.Plants[id]
		buf = binary.BigEndian.AppendUint64(buf, uint64(id))
		buf = binary.BigEndian.AppendUint32(buf, uint32(p.Position.X))
		buf = binary.BigEndian.AppendUint32(buf, uint32(p.Position.Y))
		buf = binary.BigEndian.AppendUint64(buf, uint64(p.Energy))
		buf = binary.BigEndian.AppendUint32(buf, p.Age)
		buf = binary.BigEndian.AppendUint32(buf, uint32(p.Health))
		buf = binary.BigEndian.AppendUint64(buf, p.Version)
	}

	return sha256.Sum256(buf)
}

// StateHash returns the hash of the committed state without copying it.
func (w *World) StateHash() StateHash {
	w.mu.RLock()
	defer w.mu.RUnlock()

	return w.state.Hash()
}
