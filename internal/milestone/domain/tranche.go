// Package domain holds the milestone module's pure, framework-free core.
// Advance is the whole tranche state machine — no I/O, exhaustively unit-tested.
package domain

// Milestone lifecycle states, ordered by proximity to the next payout boundary.
const (
	StateQuiet     = "quiet"
	StateWarm      = "warm"
	StateHot       = "hot"
	StateFulfilled = "fulfilled"
)

// Result is the outcome of applying an observed metric value to a milestone.
type Result struct {
	Progress int64   // new stored progress (never decreases)
	State    string  // quiet|warm|hot|fulfilled
	Crossed  []int64 // boundaries newly crossed by this update (each => one payout)
}

// Advance applies newValue to a milestone at oldProgress.
//
// Boundaries are target, target+tranche, target+2*tranche, … A payout fires each time a
// boundary is crossed. maxRepeats caps the number of tranche steps beyond target
// (k in [0, maxRepeats]); maxRepeats < 0 means unlimited (mirrors the source system's Max:-1).
func Advance(oldProgress, newValue, target, tranche, maxRepeats int64) Result {
	if tranche <= 0 {
		tranche = 1 // defensive: clause params should always be > 0
	}

	progress := oldProgress
	if newValue > progress {
		progress = newValue
	}

	// Boundaries newly crossed in (oldProgress, progress].
	var crossed []int64
	for k := int64(0); ; k++ {
		if maxRepeats >= 0 && k > maxRepeats {
			break
		}
		b := target + k*tranche
		if b > progress {
			break
		}
		if b > oldProgress {
			crossed = append(crossed, b)
		}
	}

	// Smallest boundary still ahead of progress.
	next, nextK, hasNext := int64(0), int64(0), false
	for k := int64(0); ; k++ {
		if maxRepeats >= 0 && k > maxRepeats {
			break
		}
		b := target + k*tranche
		if b > progress {
			next, nextK, hasNext = b, k, true
			break
		}
	}

	var state string
	switch {
	case len(crossed) > 0:
		// A tranche just fired.
		state = StateFulfilled
	case !hasNext:
		// Bounded milestone with every boundary already reached.
		state = StateFulfilled
	default:
		prev := int64(0) // reference point for the "how close" ratio
		if nextK > 0 {
			prev = target + (nextK-1)*tranche
		}
		gap := next - prev
		distance := next - progress
		switch {
		case distance <= 2:
			state = StateHot
		case distance*2 <= gap:
			state = StateWarm
		default:
			state = StateQuiet
		}
	}

	return Result{Progress: progress, State: state, Crossed: crossed}
}
