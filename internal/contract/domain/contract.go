// Package domain holds the contract module's framework-free types.
package domain

// Contract is the player-facing view of a club contract. Money is in minor units.
type Contract struct {
	ID          string `json:"id"`
	ClubFrom    string `json:"club_from"`
	ClubTo      string `json:"club_to"`
	Currency    string `json:"currency"`
	FixedAmount int64  `json:"fixed_amount"`
	Salary      int64  `json:"salary"`
	Status      string `json:"status"`
}

// Clause is a single provision of a contract; Params is the decoded jsonb blob.
type Clause struct {
	ID     string         `json:"id"`
	Kind   string         `json:"kind"`
	Params map[string]any `json:"params"`
}
