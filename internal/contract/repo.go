package contract

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jetlum/playerboard/internal/query"
)

// Repo is the DB adapter for the contract module. Every read is scoped by athlete_id.
type Repo struct {
	q *query.Queries
}

func NewRepo(pool *pgxpool.Pool) *Repo {
	return &Repo{q: query.New(pool)}
}

func (r *Repo) ListContracts(ctx context.Context, athleteID uuid.UUID) ([]query.Contract, error) {
	return r.q.ListContractsByAthlete(ctx, athleteID)
}

func (r *Repo) ListClauses(ctx context.Context, contractID, athleteID uuid.UUID) ([]query.Clause, error) {
	return r.q.ListClausesForAthleteContract(ctx, query.ListClausesForAthleteContractParams{
		ContractID: contractID,
		AthleteID:  athleteID,
	})
}

// Audit appends an append-only audit row for a read/write.
func (r *Repo) Audit(ctx context.Context, athleteID uuid.UUID, action, detail string) error {
	return r.q.InsertAudit(ctx, query.InsertAuditParams{
		AthleteID: pgtype.UUID{Bytes: athleteID, Valid: true},
		Action:    action,
		Detail:    detail,
	})
}
