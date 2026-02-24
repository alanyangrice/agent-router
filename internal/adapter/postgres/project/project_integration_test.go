//go:build integration

package project_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	domainproject "github.com/alanyang/agent-mesh/internal/domain/project"
	pgproject "github.com/alanyang/agent-mesh/internal/adapter/postgres/project"
	"github.com/alanyang/agent-mesh/internal/testutil"
)

func TestProjectRepo_CreateGetByID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	repo := pgproject.New(pool)

	p := domainproject.Project{
		ID:      uuid.New(),
		Name:    "test-project",
		RepoURL: "https://github.com/test/repo",
		Config:  map[string]interface{}{},
	}
	created, err := repo.Create(ctx, p)
	require.NoError(t, err)
	assert.Equal(t, p.ID, created.ID)
	assert.Equal(t, "test-project", created.Name)

	got, err := repo.GetByID(ctx, p.ID)
	require.NoError(t, err)
	assert.Equal(t, p.ID, got.ID)
	assert.Equal(t, "test-project", got.Name)
}

func TestProjectRepo_GetByID_NotFound(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	repo := pgproject.New(pool)

	_, err := repo.GetByID(context.Background(), uuid.New())
	require.Error(t, err)
}
