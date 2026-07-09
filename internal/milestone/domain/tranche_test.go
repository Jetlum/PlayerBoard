package domain

import (
	"reflect"
	"testing"
)

func TestAdvance(t *testing.T) {
	// Standard demo clause: target 20, tranche 10, unlimited repeats.
	const target, tranche, unlimited = 20, 10, -1

	tests := []struct {
		name        string
		old, newVal int64
		target      int64
		tranche     int64
		max         int64
		wantProg    int64
		wantState   string
		wantCrossed []int64
	}{
		{"quiet far away", 0, 5, target, tranche, unlimited, 5, StateQuiet, nil},
		{"warm at half", 0, 10, target, tranche, unlimited, 10, StateWarm, nil},
		{"hot near target", 18, 18, target, tranche, unlimited, 18, StateHot, nil},
		{"advance 18 to 19 stays hot", 18, 19, target, tranche, unlimited, 19, StateHot, nil},
		{"fulfil at target", 18, 20, target, tranche, unlimited, 20, StateFulfilled, []int64{20}},
		{"fulfil 19 to 20", 19, 20, target, tranche, unlimited, 20, StateFulfilled, []int64{20}},
		{"second tranche 20 to 30", 20, 30, target, tranche, unlimited, 30, StateFulfilled, []int64{30}},
		{"between tranches is warm", 20, 25, target, tranche, unlimited, 25, StateWarm, nil},
		{"near second boundary is hot", 20, 29, target, tranche, unlimited, 29, StateHot, nil},
		{"multi-cross in one jump", 18, 45, target, tranche, unlimited, 45, StateFulfilled, []int64{20, 30, 40}},
		{"regression keeps progress", 20, 18, target, tranche, unlimited, 20, StateQuiet, nil},
		{"replay same value no cross", 20, 20, target, tranche, unlimited, 20, StateQuiet, nil},

		// Bounded: max=0 => only the target boundary ever pays.
		{"max0 single payout", 18, 35, target, tranche, 0, 35, StateFulfilled, []int64{20}},
		{"max0 already done", 25, 40, target, tranche, 0, 40, StateFulfilled, nil},
		// Bounded: max=1 => target and target+tranche.
		{"max1 two payouts in one jump", 18, 45, target, tranche, 1, 45, StateFulfilled, []int64{20, 30}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Advance(tc.old, tc.newVal, tc.target, tc.tranche, tc.max)
			if got.Progress != tc.wantProg {
				t.Errorf("progress = %d, want %d", got.Progress, tc.wantProg)
			}
			if got.State != tc.wantState {
				t.Errorf("state = %q, want %q", got.State, tc.wantState)
			}
			if !reflect.DeepEqual(got.Crossed, tc.wantCrossed) {
				t.Errorf("crossed = %v, want %v", got.Crossed, tc.wantCrossed)
			}
		})
	}
}

// Advancing across the same boundary twice must only pay once (engine idempotency depends on this).
func TestAdvanceIdempotentBoundary(t *testing.T) {
	first := Advance(19, 20, 20, 10, -1)
	if len(first.Crossed) != 1 {
		t.Fatalf("first cross = %v, want one boundary", first.Crossed)
	}
	second := Advance(first.Progress, 20, 20, 10, -1)
	if len(second.Crossed) != 0 {
		t.Errorf("replay crossed = %v, want none", second.Crossed)
	}
	if second.Progress != 20 {
		t.Errorf("replay progress = %d, want 20", second.Progress)
	}
}
