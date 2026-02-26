//go:build integration

package project_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pgproject "github.com/alanyang/agent-mesh/internal/adapter/postgres/project"
	domainproject "github.com/alanyang/agent-mesh/internal/domain/project"
	"github.com/alanyang/agent-mesh/internal/testutil"
)

func TestProjectRepo_Create(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	repo := pgproject.New(pool)

	proj := domainproject.Project{
		ID:      uuid.New(),
		Name:    "test-" + uuid.New().String()[:8],
		RepoURL: "https://github.com/x",
		Config:  map[string]interface{}{},
	}

	created, err := repo.Create(ctx, proj)
	require.NoError(t, err)
	assert.Equal(t, proj.ID, created.ID)
	assert.Equal(t, proj.Name, created.Name)
}

func TestProjectRepo_GetByID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		pool := testutil.SetupTestDB(t)
		ctx := context.Background()
		repo := pgproject.New(pool)

		proj := domainproject.Project{
			ID:      uuid.New(),
			Name:    "test-" + uuid.New().String()[:8],
			RepoURL: "https://github.com/x",
			Config:  map[string]interface{}{},
		}
		_, err := repo.Create(ctx, proj)
		require.NoError(t, err)

		got, err := repo.GetByID(ctx, proj.ID)
		require.NoError(t, err)
		assert.Equal(t, proj.ID, got.ID)
		assert.Equal(t, proj.Name, got.Name)
	})

	t.Run("not found returns error", func(t *testing.T) {
		pool := testutil.SetupTestDB(t)
		ctx := context.Background()
		repo := pgproject.New(pool)

		_, err := repo.GetByID(ctx, uuid.New())
		require.Error(t, err)
	})
}
