package git

import (
	"context"
	"fmt"

	portgit "github.com/alanyang/agent-mesh/internal/port/git"
)

type Service struct {
	provider portgit.Provider
}

func NewService(provider portgit.Provider) *Service {
	return &Service{provider: provider}
}

func (s *Service) OpenPR(ctx context.Context, title, body, head, base string) (portgit.PR, error) {
	pr, err := s.provider.OpenPR(ctx, title, body, head, base)
	if err != nil {
		return portgit.PR{}, fmt.Errorf("open PR: %w", err)
	}
	return pr, nil
}

func (s *Service) MergePR(ctx context.Context, prNumber int) error {
	if err := s.provider.MergePR(ctx, prNumber); err != nil {
		return fmt.Errorf("merge PR: %w", err)
	}
	return nil
}

func (s *Service) PostComment(ctx context.Context, prNumber int, comment portgit.ReviewComment) error {
	if err := s.provider.PostComment(ctx, prNumber, comment); err != nil {
		return fmt.Errorf("post comment: %w", err)
	}
	return nil
}

func (s *Service) GetDiff(ctx context.Context, prNumber int) (portgit.Diff, error) {
	diff, err := s.provider.GetDiff(ctx, prNumber)
	if err != nil {
		return portgit.Diff{}, fmt.Errorf("get diff: %w", err)
	}
	return diff, nil
}
