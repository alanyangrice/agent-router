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

// ── helpers ───────────────────────────────────────────────────────────────────

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

func TestCreateThread(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(repo *mocks.MockThreadRepository) domainthread.Thread
		wantErr bool
		wantMsg string
	}{
		{
			name: "success",
			setup: func(repo *mocks.MockThreadRepository) domainthread.Thread {
				projectID := uuid.New()
				expected := domainthread.Thread{ID: uuid.New(), ProjectID: projectID, Name: "thread-1"}
				repo.EXPECT().CreateThread(gomock.Any(), gomock.Any()).Return(expected, nil)
				return expected
			},
		},
		{
			name: "repo error",
			setup: func(repo *mocks.MockThreadRepository) domainthread.Thread {
				repo.EXPECT().CreateThread(gomock.Any(), gomock.Any()).
					Return(domainthread.Thread{}, errors.New("db error"))
				return domainthread.Thread{}
			},
			wantErr: true,
			wantMsg: "create thread",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _ := newThreadSvc(t)
			expected := tt.setup(repo)

			got, err := svc.CreateThread(context.Background(), uuid.New(), domainthread.TypeTask, "thread-1", nil)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, expected.ID, got.ID)
		})
	}
}

// ── GetThread ─────────────────────────────────────────────────────────────────

func TestGetThread(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(repo *mocks.MockThreadRepository, id uuid.UUID)
		wantErr bool
		wantMsg string
	}{
		{
			name: "success",
			setup: func(repo *mocks.MockThreadRepository, id uuid.UUID) {
				repo.EXPECT().GetThreadByID(gomock.Any(), id).
					Return(domainthread.Thread{ID: id, Name: "t"}, nil)
			},
		},
		{
			name: "not found",
			setup: func(repo *mocks.MockThreadRepository, id uuid.UUID) {
				repo.EXPECT().GetThreadByID(gomock.Any(), id).
					Return(domainthread.Thread{}, errors.New("not found"))
			},
			wantErr: true,
			wantMsg: "get thread",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _ := newThreadSvc(t)
			threadID := uuid.New()
			tt.setup(repo, threadID)

			got, err := svc.GetThread(context.Background(), threadID)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, threadID, got.ID)
		})
	}
}

// ── ListThreads ───────────────────────────────────────────────────────────────

func TestListThreads(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(repo *mocks.MockThreadRepository)
		wantLen int
		wantErr bool
	}{
		{
			name: "success returns all threads",
			setup: func(repo *mocks.MockThreadRepository) {
				repo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).
					Return([]domainthread.Thread{{ID: uuid.New()}, {ID: uuid.New()}}, nil)
			},
			wantLen: 2,
		},
		{
			name: "repo error",
			setup: func(repo *mocks.MockThreadRepository) {
				repo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return(nil, errors.New("db error"))
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _ := newThreadSvc(t)
			tt.setup(repo)

			got, err := svc.ListThreads(context.Background(), domainthread.ListFilters{})
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tt.wantLen)
		})
	}
}

// ── PostMessage ───────────────────────────────────────────────────────────────

func TestPostMessage(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(repo *mocks.MockThreadRepository, bus *mocks.MockEventBus, threadID uuid.UUID) domainthread.Message
		wantErr bool
		wantMsg string
	}{
		{
			name: "success",
			setup: func(repo *mocks.MockThreadRepository, bus *mocks.MockEventBus, threadID uuid.UUID) domainthread.Message {
				expected := domainthread.Message{ID: uuid.New(), ThreadID: threadID, Content: "hello"}
				repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(expected, nil)
				bus.EXPECT().Publish(gomock.Any(), matchEventType(event.TypeThreadMessage)).Return(nil)
				return expected
			},
		},
		{
			name: "repo error",
			setup: func(repo *mocks.MockThreadRepository, bus *mocks.MockEventBus, threadID uuid.UUID) domainthread.Message {
				repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).
					Return(domainthread.Message{}, errors.New("db error"))
				return domainthread.Message{}
			},
			wantErr: true,
			wantMsg: "post message",
		},
		{
			// Bus error is non-fatal — message is still returned successfully.
			name: "bus error is non-fatal",
			setup: func(repo *mocks.MockThreadRepository, bus *mocks.MockEventBus, threadID uuid.UUID) domainthread.Message {
				expected := domainthread.Message{ID: uuid.New(), ThreadID: threadID}
				repo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(expected, nil)
				bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(errors.New("bus error"))
				return expected
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, bus := newThreadSvc(t)
			threadID := uuid.New()
			expected := tt.setup(repo, bus, threadID)

			got, err := svc.PostMessage(context.Background(), threadID, nil, domainthread.PostProgress, "hello")
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantMsg)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, expected.ID, got.ID)
		})
	}
}

// ── ListMessages ──────────────────────────────────────────────────────────────

func TestListMessages(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name    string
		setup   func(repo *mocks.MockThreadRepository, threadID uuid.UUID)
		wantLen int
		wantErr bool
		checkOrder bool
	}{
		{
			name: "success returns messages",
			setup: func(repo *mocks.MockThreadRepository, threadID uuid.UUID) {
				repo.EXPECT().ListMessages(gomock.Any(), threadID).
					Return([]domainthread.Message{{ID: uuid.New()}, {ID: uuid.New()}}, nil)
			},
			wantLen: 2,
		},
		{
			name: "repo error",
			setup: func(repo *mocks.MockThreadRepository, threadID uuid.UUID) {
				repo.EXPECT().ListMessages(gomock.Any(), gomock.Any()).Return(nil, errors.New("db error"))
			},
			wantErr: true,
		},
		{
			name: "order is stable — oldest first",
			setup: func(repo *mocks.MockThreadRepository, threadID uuid.UUID) {
				msgs := []domainthread.Message{
					{ID: uuid.New(), CreatedAt: now.Add(-2 * time.Second)},
					{ID: uuid.New(), CreatedAt: now.Add(-1 * time.Second)},
					{ID: uuid.New(), CreatedAt: now},
				}
				repo.EXPECT().ListMessages(gomock.Any(), threadID).Return(msgs, nil)
			},
			wantLen:    3,
			checkOrder: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _ := newThreadSvc(t)
			threadID := uuid.New()
			tt.setup(repo, threadID)

			got, err := svc.ListMessages(context.Background(), threadID)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Len(t, got, tt.wantLen)
			if tt.checkOrder {
				assert.True(t, got[0].CreatedAt.Before(got[len(got)-1].CreatedAt), "messages must be oldest first")
			}
		})
	}
}
