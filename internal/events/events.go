// Package events defines the payloads that flow over the bus between the api (producer)
// and worker (consumer). Kept dependency-free so both sides can import it.
package events

// Event type names carried on MilestoneChanged.Type and used as WS message types.
const (
	TypePerformanceObserved = "PerformanceObserved"
	TypeMilestoneAdvanced   = "MilestoneAdvanced"
	TypeMilestoneFulfilled  = "MilestoneFulfilled"
)

// PerformanceObserved is emitted by ingest when a signed stat webhook is accepted.
type PerformanceObserved struct {
	AthleteID     string `json:"athlete_id"`
	Metric        string `json:"metric"`
	Value         int64  `json:"value"`
	SourceEventID string `json:"source_event_id"`
}

// MilestoneChanged is emitted by the worker and fanned out to the player's WebSocket.
type MilestoneChanged struct {
	Type        string `json:"type"` // MilestoneAdvanced | MilestoneFulfilled
	AthleteID   string `json:"athlete_id"`
	MilestoneID string `json:"milestone_id"`
	Metric      string `json:"metric"`
	Progress    int64  `json:"progress"`
	Target      int64  `json:"target"`
	State       string `json:"state"`
	Boundary    int64  `json:"boundary,omitempty"`
	Amount      int64  `json:"amount,omitempty"`
	Currency    string `json:"currency,omitempty"`
}
