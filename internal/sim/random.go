package sim

type DecisionKind uint64

const (
	DecisionKindMovementIntent     DecisionKind = 1
	DecisionKindFeedingIntent      DecisionKind = 2
	DecisionKindReproductionIntent DecisionKind = 3
	DecisionKindPlantSeeding       DecisionKind = 4
	DecisionKindPlantSprouting     DecisionKind = 5
	DecisionKindInitialPlacement   DecisionKind = 6
)

const pcgMultiplier uint64 = 6364136223846793005

type DecisionRandom struct {
	state uint64
	inc   uint64
}

func NewDecisionRandom(seed uint64, tick Tick, actorID EntityID, kind DecisionKind, ordinal uint64) DecisionRandom {
	splitmixState := uint64(0)
	initialState := uint64(0)

	for _, value := range [...]uint64{seed, uint64(tick), uint64(actorID), uint64(kind), ordinal} {
		splitmixState += value
		splitmixState, initialState = splitmix64Next(splitmixState)
	}

	var stream uint64
	splitmixState, stream = splitmix64Next(splitmixState)

	r := DecisionRandom{
		inc: (stream << 1) | 1,
	}
	r.advance()
	r.state += initialState
	r.advance()

	return r
}

func (r *DecisionRandom) Uint32() uint32 {
	return r.advance()
}

func (r DecisionRandom) State() uint64 {
	return r.state
}

func (r DecisionRandom) Increment() uint64 {
	return r.inc
}

func splitmix64Next(state uint64) (uint64, uint64) {
	state += 0x9e3779b97f4a7c15
	z := state
	z = (z ^ (z >> 30)) * 0xbf58476d1ce4e5b9
	z = (z ^ (z >> 27)) * 0x94d049bb133111eb
	return state, z ^ (z >> 31)
}

func (r *DecisionRandom) advance() uint32 {
	oldState := r.state
	r.state = oldState*pcgMultiplier + r.inc

	xorshifted := uint32(((oldState >> 18) ^ oldState) >> 27)
	rot := uint32(oldState >> 59)
	return (xorshifted >> rot) | (xorshifted << ((-rot) & 31))
}
