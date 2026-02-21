package thread

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/alanyang/agent-mesh/internal/domain/event"
	domainthread "github.com/alanyang/agent-mesh/internal/domain/thread"
	portbus "github.com/alanyang/agent-mesh/internal/port/eventbus"
	portthread "github.com/alanyang/agent-mesh/internal/port/thread"
)

type Service struct {
	repo portthread.Repository
	bus  portbus.EventBus
}

func NewService(repo portthread.Repository, bus portbus.EventBus) *Service {
	return &Service{repo: repo, bus: bus}
}

func (s *Service) CreateThread(ctx context.Context, projectID uuid.UUID, threadType domainthread.ThreadType, name string, taskID *uuid.UUID) (domainthread.Thread, error) {
	t := domainthread.New(projectID, threadType, name, taskID)
	created, err := s.repo.CreateThread(ctx, t)
	if err != nil {
		return domainthread.Thread{}, fmt.Errorf("create thread: %w", err)
	}
	return created, nil
}

func (s *Service) GetThread(ctx context.Context, id uuid.UUID) (domainthread.Thread, error) {
	t, err := s.repo.GetThreadByID(ctx, id)
	if err != nil {
		return domainthread.Thread{}, fmt.Errorf("get thread: %w", err)
	}
	return t, nil
}

func (s *Service) ListThreads(ctx context.Context, filters domainthread.ListFilters) ([]domainthread.Thread, error) {
	threads, err := s.repo.ListThreads(ctx, filters)
	if err != nil {
		return nil, fmt.Errorf("list threads: %w", err)
	}
	return threads, nil
}

func (s *Service) PostMessage(ctx context.Context, threadID uuid.UUID, agentID *uuid.UUID, postType domainthread.PostType, content string) (domainthread.Message, error) {
	m := domainthread.NewMessage(threadID, agentID, postType, content)
	created, err := s.repo.CreateMessage(ctx, m)
	if err != nil {
		return domainthread.Message{}, fmt.Errorf("post message: %w", err)
	}
	if err := s.bus.Publish(ctx, event.New(event.TypeThreadMessage, created.ID)); err != nil {
		slog.ErrorContext(ctx, "failed to publish ThreadMessage event", "message_id", created.ID, "error", err)
	}
	return created, nil
}

func (s *Service) ListMessages(ctx context.Context, threadID uuid.UUID) ([]domainthread.Message, error) {
	msgs, err := s.repo.ListMessages(ctx, threadID)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	return msgs, nil
}
