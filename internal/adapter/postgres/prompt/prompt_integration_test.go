//go:build integration

package prompt_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pgprompt "github.com/alanyang/agent-mesh/internal/adapter/postgres/prompt"
	pgproject "github.com/alanyang/agent-mesh/internal/adapter/postgres/project"
	domainproject "github.com/alanyang/agent-mesh/internal/domain/project"
	domainprompt "github.com/alanyang/agent-mesh/internal/domain/prompt"
	"github.com/alanyang/agent-mesh/internal/testutil"
)

func createTestProject(t *testing.T, repo interface{ Create(context.Context, domainproject.Project) (domainproject.Project, error) }) domainproject.Project {
	t.Helper()
	p := domainproject.Project{ID: uuid.New(), Name: "prompt-test-" + uuid.New().String()[:8], RepoURL: "https://github.com/x", Config: map[string]interface{}{}}
	created, err := repo.Create(context.Background(), p)
	require.NoError(t, err)
	return created
}

func TestPromptRepo_GlobalFallback(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	repo := pgprompt.New(pool)
	projectID := uuid.New()

	// No project-specific prompt set → should return global default.
	got, err := repo.GetForRole(ctx, &projectID, "coder")
	require.NoError(t, err)
	assert.Equal(t, "coder", got.Role)
	assert.NotEmpty(t, got.Content, "global default prompt must not be empty")
}

func TestPromptRepo_ProjectOverride(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	repo := pgprompt.New(pool)
	projRepo := pgproject.New(pool)
	proj := createTestProject(t, projRepo)

	// Set a project-specific prompt.
	err := repo.Set(ctx, domainprompt.RolePrompt{
		ProjectID: &proj.ID,
		Role:      "coder",
		Content:   "custom coder prompt for project",
	})
	require.NoError(t, err)

	// Retrieve → should return project-specific.
	got, err := repo.GetForRole(ctx, &proj.ID, "coder")
	require.NoError(t, err)
	assert.Equal(t, "custom coder prompt for project", got.Content)
}

func TestPromptRepo_List(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	repo := pgprompt.New(pool)
	projRepo := pgproject.New(pool)
	proj := createTestProject(t, projRepo)

	// List returns at least the global defaults merged with any project overrides.
	prompts, err := repo.List(ctx, proj.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, prompts, "list must return at least global defaults")

	// Roles present in result.
	roles := make(map[string]bool)
	for _, p := range prompts {
		roles[p.Role] = true
	}
	assert.True(t, roles["coder"], "coder prompt must be present")
	assert.True(t, roles["qa"], "qa prompt must be present")
	assert.True(t, roles["reviewer"], "reviewer prompt must be present")
}
