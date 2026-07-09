package contract

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"

	"github.com/jetlum/playerboard/internal/contract/domain"
)

// Service maps DB rows to the framework-free domain view and writes audit trail entries.
type Service struct {
	repo *Repo
}

func NewService(repo *Repo) *Service {
	return &Service{repo: repo}
}

func (s *Service) Contracts(ctx context.Context, athleteID uuid.UUID) ([]domain.Contract, error) {
	rows, err := s.repo.ListContracts(ctx, athleteID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Audit(ctx, athleteID, "contracts.read", ""); err != nil {
		slog.Warn("audit write failed", "err", err)
	}
	out := make([]domain.Contract, 0, len(rows))
	for _, c := range rows {
		out = append(out, domain.Contract{
			ID:          c.ID.String(),
			ClubFrom:    c.ClubFrom,
			ClubTo:      c.ClubTo,
			Currency:    c.Currency,
			FixedAmount: c.FixedAmount,
			Salary:      c.Salary,
			Status:      c.Status,
		})
	}
	return out, nil
}

func (s *Service) Clauses(ctx context.Context, contractID, athleteID uuid.UUID) ([]domain.Clause, error) {
	rows, err := s.repo.ListClauses(ctx, contractID, athleteID)
	if err != nil {
		return nil, err
	}
	if err := s.repo.Audit(ctx, athleteID, "clauses.read", contractID.String()); err != nil {
		slog.Warn("audit write failed", "err", err)
	}
	out := make([]domain.Clause, 0, len(rows))
	for _, c := range rows {
		params := map[string]any{}
		if len(c.Params) > 0 {
			_ = json.Unmarshal(c.Params, &params)
		}
		out = append(out, domain.Clause{
			ID:     c.ID.String(),
			Kind:   c.Kind,
			Params: params,
		})
	}
	return out, nil
}
