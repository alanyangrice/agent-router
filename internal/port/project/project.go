package project

import (
	"context"

	"github.com/google/uuid"

	domainproject "github.com/alanyang/agent-mesh/internal/domain/project"
)

// Repository manages project persistence.
// [DIP] service/project depends on this interface, not on a concrete storage.
type Repository interface {
	Create(ctx context.Context, p domainproject.Project) (domainproject.Project, error)
	GetByID(ctx context.Context, id uuid.UUID) (domainproject.Project, error)
}
