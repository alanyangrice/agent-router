package thread_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/alanyang/agent-mesh/internal/domain/event"
	domainthread "github.com/alanyang/agent-mesh/internal/domain/thread"
	"github.com/alanyang/agent-mesh/internal/mocks"
	threadsvc "github.com/alanyang/agent-mesh/internal/service/thread"
)

func newThreadSvc(t *testing.T) (*threadsvc.Service, *mocks.MockThreadRepository, *mocks.MockEventBus) {
	t.Helper()
	ctrl := gomock.NewController(t)
	repo := mocks.NewMockThreadRepository(ctrl)
	bus := mocks.NewMockEventBus(ctrl)
	return threadsvc.NewService(repo, bus), repo, bus
}

func matchEventType(et event.Type) gomock.Matcher {
	return eventTypeMatcher{et}
}

type eventTypeMatcher struct{ want event.Type }

func (m eventTypeMatcher) Matches(x interface{}) bool {
	e, ok := x.(event.Event)
	return ok && e.Type == m.want
}
func (m eventTypeMatcher) String() string { return "event.Type=" + string(m.want) }

// ── CreateThread ──────────────────────────────────────────────────────────────

func TestCreateThread_Success(t *testing.T) {
	svc, repo, _ := newThreadSvc(t)
	projectID := uuid.New()
	expected := domainthread.Thread{ID: uuid.New(), ProjectID: projectID, Name: "thread-1"}
	repo.EXPECT().CreateThread(gomock.Any(), gomock.Any()).Return(expected, nil)

	got, err := svc.CreateThread(context.Background(), projectID, domainthread.TypeTask, "thread-1", nil)
	require.NoError(t, err)
	assert.Equal(t, expected.ID, got.ID)
}

func TestCreateThread_Error(t *testing.T) {
	svc, repo, _ := newThreadSvc(t)
	repo.EXPECT().CreateThread(gomock.Any(), gomock.Any()).Return(domainthread.Thread{}, errors.New("db error"))

	_, err := svc.CreateThread(context.Background(), uuid.New(), domainthread.TypeTask, "t", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create thread")
}

// ── GetThread ─────────────────────────────────────────────────────────────────

func TestGetThread_Success(t *testing.T) {
	svc, repo, _ := newThreadSvc(t)
	threadID := uuid.New()
	expected := domainthread.Thread{ID: threadID, Name: "t"}
	repo.EXPECT().GetThreadByID(gomock.Any(), threadID).Return(expected, nil)

	got, err := svc.GetThread(context.Background(), threadID)
	require.NoError(t, err)
	assert.Equal(t, threadID, got.ID)
}

func TestGetThread_NotFound(t *testing.T) {
	svc, repo, _ := newThreadSvc(t)
	threadID := uuid.New()
	repo.EXPECT().GetThreadByID(gomock.Any(), threadID).Return(domainthread.Thread{}, errors.New("not found"))

	_, err := svc.GetThread(context.Background(), threadID)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "get thread")
}

// ── ListThreads ───────────────────────────────────────────────────────────────

func TestListThreads_Success(t *testing.T) {
	svc, repo, _ := newThreadSvc(t)
	expected := []domainthread.Thread{{ID: uuid.New()}, {ID: uuid.New()}}
	repo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return(expected, nil)

	got, err := svc.ListThreads(context.Background(), domainthread.ListFilters{})
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestListThreads_Error(t *testing.T) {
	svc, repo, _ := newThreadSvc(t)
	repo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return(nil, errors.New("db error"))

	_, err := svc.ListThreads(context.Background(), domainthread.ListFilters{})
	require.Error(t, err)
}

// ── PostMessage ───────────────────────────────────────────────────────────────

func TestPostMessage_Success(t *testing.T) {
	svc, repo, bus := newThreadSvc(t)
	threadID := uuid.New()
	expected := domainthread.Message{ID: uuid.New(), ThreadID: threadID, Content: "hello"}
	repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(expected, nil)
	bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeThreadMessage)).Return(nil)

	got, err := svc.PostMessage(context.Background(), threadID, nil, domainthread.PostProgress, "hello")
	require.NoError(t, err)
	assert.Equal(t, expected.ID, got.ID)
}

func TestPostMessage_RepoError(t *testing.T) {
	svc, repo, _ := newThreadSvc(t)
	repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(domainthread.Message{}, errors.New("db error"))

	_, err := svc.PostMessage(context.Background(), uuid.New(), nil, domainthread.PostProgress, "content")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "post message")
}

func TestPostMessage_BusError(t *testing.T) {
	// Bus error is non-fatal — message is still returned successfully.
	svc, repo, bus := newThreadSvc(t)
	threadID := uuid.New()
	expected := domainthread.Message{ID: uuid.New(), ThreadID: threadID}
	repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(expected, nil)
	bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(errors.New("bus error"))

	got, err := svc.PostMessage(context.Background(), threadID, nil, domainthread.PostProgress, "hello")
	require.NoError(t, err)
	assert.Equal(t, expected.ID, got.ID)
}

// ── ListMessages ──────────────────────────────────────────────────────────────

func TestListMessages_Success(t *testing.T) {
	svc, repo, _ := newThreadSvc(t)
	threadID := uuid.New()
	expected := []domainthread.Message{{ID: uuid.New()}, {ID: uuid.New()}}
	repo.EXPECT().ListMessages(gomock.Any(), threadID).Return(expected, nil)

	got, err := svc.ListMessages(context.Background(), threadID)
	require.NoError(t, err)
	assert.Len(t, got, 2)
}

func TestListMessages_Error(t *testing.T) {
	svc, repo, _ := newThreadSvc(t)
	repo.EXPECT().ListMessages(gomock.Any(), gomock.Any()).Return(nil, errors.New("db error"))

	_, err := svc.ListMessages(context.Background(), uuid.New())
	require.Error(t, err)
}

func TestListMessages_OrderIsStable(t *testing.T) {
	svc, repo, _ := newThreadSvc(t)
	threadID := uuid.New()
	now := time.Now()
	msgs := []domainthread.Message{
		{ID: uuid.New(), CreatedAt: now.Add(-2 * time.Second)},
		{ID: uuid.New(), CreatedAt: now.Add(-1 * time.Second)},
		{ID: uuid.New(), CreatedAt: now},
	}
	repo.EXPECT().ListMessages(gomock.Any(), threadID).Return(msgs, nil)

	got, err := svc.ListMessages(context.Background(), threadID)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.True(t, got[0].CreatedAt.Before(got[len(got)-1].CreatedAt), "messages must be oldest first")
}
