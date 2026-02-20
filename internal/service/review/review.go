package review

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	domainreview "github.com/alanyang/agent-mesh/internal/domain/review"
)

type Repository interface {
	Create(ctx context.Context, r domainreview.Review) (domainreview.Review, error)
	GetByTaskID(ctx context.Context, taskID uuid.UUID) ([]domainreview.Review, error)
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) *Service {
	return &Service{repo: repo}
}

func (s *Service) Create(ctx context.Context, taskID, reviewerAgentID uuid.UUID, prURL string, verdict domainreview.Verdict, comments []domainreview.Comment) (domainreview.Review, error) {
	r := domainreview.New(taskID, reviewerAgentID, prURL, verdict, comments)

	created, err := s.repo.Create(ctx, r)
	if err != nil {
		return domainreview.Review{}, fmt.Errorf("create review: %w", err)
	}
	return created, nil
}

func (s *Service) GetByTaskID(ctx context.Context, taskID uuid.UUID) ([]domainreview.Review, error) {
	reviews, err := s.repo.GetByTaskID(ctx, taskID)
	if err != nil {
		return nil, fmt.Errorf("get reviews by task: %w", err)
	}
	return reviews, nil
}
