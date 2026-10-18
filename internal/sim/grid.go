package sim

import "slices"

// neighborOffsets lists the eight surrounding cells in a fixed order, so
// candidate lists, and therefore random picks, are deterministic.
var neighborOffsets = [8][2]int32{
	{-1, -1}, {0, -1}, {1, -1},
	{-1, 0}, {1, 0},
	{-1, 1}, {0, 1}, {1, 1},
}

// pick selects one candidate using a random draw.
func pick(candidates []Position, draw uint32) (Position, bool) {
	if len(candidates) == 0 {
		return Position{}, false
	}
	return candidates[int(draw%uint32(len(candidates)))], true
}

func sortedIDs(ids []EntityID) []EntityID {
	sorted := slices.Clone(ids)
	slices.Sort(sorted)
	return sorted
}
