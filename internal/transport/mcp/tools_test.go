package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
	mcpmcp "github.com/mark3labs/mcp-go/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	domainagent "github.com/alanyang/agent-mesh/internal/domain/agent"
	"github.com/alanyang/agent-mesh/internal/domain/pipeline"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	domainthread "github.com/alanyang/agent-mesh/internal/domain/thread"
	"github.com/alanyang/agent-mesh/internal/mocks"
	agentsvc "github.com/alanyang/agent-mesh/internal/service/agent"
	tasksvc "github.com/alanyang/agent-mesh/internal/service/task"
	threadsvc "github.com/alanyang/agent-mesh/internal/service/thread"
)

// ── helpers ───────────────────────────────────────────────────────────────────

type toolsDeps struct {
	agentRepo     *mocks.MockAgentRepository
	taskRepo      *mocks.MockTaskRepository
	threadRepo    *mocks.MockThreadRepository
	bus           *mocks.MockEventBus
	dist          *mocks.MockDistributor
	agentNotifier *mocks.MockAgentNotifier
	roleNotifier  *mocks.MockRoleNotifier
	locker        *mocks.MockAdvisoryLocker
}

func newToolsDeps(t *testing.T) (*agentsvc.Service, *tasksvc.Service, *threadsvc.Service, toolsDeps) {
	t.Helper()
	ctrl := gomock.NewController(t)
	d := toolsDeps{
		agentRepo:     mocks.NewMockAgentRepository(ctrl),
		taskRepo:      mocks.NewMockTaskRepository(ctrl),
		threadRepo:    mocks.NewMockThreadRepository(ctrl),
		bus:           mocks.NewMockEventBus(ctrl),
		dist:          mocks.NewMockDistributor(ctrl),
		agentNotifier: mocks.NewMockAgentNotifier(ctrl),
		roleNotifier:  mocks.NewMockRoleNotifier(ctrl),
		locker:        mocks.NewMockAdvisoryLocker(ctrl),
	}
	aSvc := agentsvc.NewService(d.agentRepo, d.taskRepo, d.bus)
	tSvc := tasksvc.NewService(d.taskRepo, d.bus, d.dist, d.threadRepo, d.agentNotifier, d.roleNotifier, pipeline.DefaultConfig, d.locker)
	thSvc := threadsvc.NewService(d.threadRepo, d.bus)
	return aSvc, tSvc, thSvc, d
}

// allowSweep permits any sweep goroutine calls silently (no agents found).
func allowSweep(d toolsDeps) {
	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error {
			return fn(ctx)
		}).AnyTimes()
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()
	d.dist.EXPECT().Distribute(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(uuid.Nil, errors.New("no agent")).AnyTimes()
}

// sweepOnce registers a single sweep call that decrements wg when the goroutine runs.
func sweepOnce(d toolsDeps, wg *sync.WaitGroup) {
	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error {
			defer wg.Done()
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()
}

func makeReq(args map[string]any) mcpmcp.CallToolRequest {
	var req mcpmcp.CallToolRequest
	req.Params.Arguments = args
	return req
}

func resultText(r *mcpmcp.CallToolResult) string {
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	b, _ := json.Marshal(r.Content[0])
	var m map[string]interface{}
	json.Unmarshal(b, &m) //nolint:errcheck
	if t, ok := m["text"].(string); ok {
		return t
	}
	return ""
}

func validRolesForTest() map[string]bool {
	roles := make(map[string]bool)
	for _, action := range pipeline.DefaultConfig {
		if action.AssignRole != "" {
			roles[action.AssignRole] = true
		}
	}
	return roles
}

// ── registerAgentHandler ──────────────────────────────────────────────────────

func TestRegisterAgentHandler(t *testing.T) {
	tests := []struct {
		name         string
		args         map[string]any
		setup        func(d toolsDeps, wg *sync.WaitGroup)
		wantContains string
		wantErr      bool
	}{
		{
			name: "new agent registered and ID returned",
			args: map[string]any{
				"project_id": uuid.New().String(),
				"role":       "coder",
				"name":       "bot",
				"model":      "gpt4",
			},
			setup: func(d toolsDeps, wg *sync.WaitGroup) {
				agentID := uuid.New()
				projectID := uuid.New()
				expected := domainagent.Agent{ID: agentID, ProjectID: projectID, Role: "coder", Status: domainagent.StatusIdle}
				d.agentRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(expected, nil)
				d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)
				wg.Add(1)
				sweepOnce(d, wg)
			},
		},
		{
			name: "invalid project_id returns error text",
			args: map[string]any{"project_id": "not-a-uuid", "role": "coder", "name": "bot", "model": "gpt4"},
			setup: func(d toolsDeps, wg *sync.WaitGroup) {},
			wantContains: "error: invalid project_id",
		},
		{
			name: "invalid role returns error text",
			args: map[string]any{"project_id": uuid.New().String(), "role": "admin", "name": "bot", "model": "gpt4"},
			setup: func(d toolsDeps, wg *sync.WaitGroup) {},
			wantContains: "error: invalid role",
		},
		{
			name: "reconnect with existing agent_id reactivates",
			args: map[string]any{
				"project_id": uuid.New().String(),
				"role":       "coder",
				"name":       "bot",
				"model":      "gpt4",
				"agent_id":   uuid.New().String(),
			},
			setup: func(d toolsDeps, wg *sync.WaitGroup) {
				existingID := uuid.New()
				projectID := uuid.New()
				stored := domainagent.Agent{ID: existingID, ProjectID: projectID, Role: "coder", Status: domainagent.StatusIdle}
				d.agentRepo.EXPECT().GetByID(gomock.Any(), gomock.Any()).Return(stored, nil)
				d.agentRepo.EXPECT().UpdateStatus(gomock.Any(), gomock.Any(), domainagent.StatusIdle).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)
				wg.Add(1)
				sweepOnce(d, wg)
			},
		},
		{
			name: "agent_id not found falls through to create new agent",
			args: map[string]any{
				"project_id": uuid.New().String(),
				"role":       "coder",
				"name":       "bot",
				"model":      "gpt4",
				"agent_id":   uuid.New().String(),
			},
			setup: func(d toolsDeps, wg *sync.WaitGroup) {
				newAgentID := uuid.New()
				projectID := uuid.New()
				newAgent := domainagent.Agent{ID: newAgentID, ProjectID: projectID, Role: "coder", Status: domainagent.StatusIdle}
				d.agentRepo.EXPECT().GetByID(gomock.Any(), gomock.Any()).Return(domainagent.Agent{}, errors.New("not found"))
				d.agentRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(newAgent, nil)
				d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)
				allowSweep(d)
			},
		},
		{
			// Handler always passes []string{} for skills — skills are a V2 concern.
			name: "skills field ignored — always passes empty skills",
			args: map[string]any{
				"project_id": uuid.New().String(),
				"role":       "coder",
				"name":       "bot",
				"model":      "gpt4",
			},
			setup: func(d toolsDeps, wg *sync.WaitGroup) {
				d.agentRepo.EXPECT().Create(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, a domainagent.Agent) (domainagent.Agent, error) {
						assert.Empty(t, a.Skills, "handler must pass empty skills")
						return domainagent.Agent{ID: uuid.New(), Role: "coder"}, nil
					})
				d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)
				allowSweep(d)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aSvc, tSvc, _, d := newToolsDeps(t)
			reg := NewSessionRegistry()
			handler := registerAgentHandler(nil, reg, aSvc, tSvc, validRolesForTest())

			// Override args with correct project_id/agent_id UUIDs where needed.
			args := tt.args

			var wg sync.WaitGroup
			tt.setup(d, &wg)

			result, err := handler(context.Background(), makeReq(args))
			wg.Wait()

			require.NoError(t, err)
			if tt.wantContains != "" {
				assert.Contains(t, resultText(result), tt.wantContains)
			}
		})
	}
}

// ── claimTaskHandler ──────────────────────────────────────────────────────────

func TestClaimTaskHandler(t *testing.T) {
	tests := []struct {
		name         string
		agentIDStr   string
		setup        func(d toolsDeps, agentID uuid.UUID, wg *sync.WaitGroup)
		wantContains string
	}{
		{
			name: "task assigned — sets working and returns task",
			setup: func(d toolsDeps, agentID uuid.UUID, wg *sync.WaitGroup) {
				projectID := uuid.New()
				taskID := uuid.New()
				agent := domainagent.Agent{ID: agentID, ProjectID: projectID, Role: "coder"}
				task := domaintask.Task{ID: taskID, ProjectID: projectID, Status: domaintask.StatusInProgress, Labels: []string{}}
				d.agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(agent, nil)
				d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{task}, nil)
				d.agentRepo.EXPECT().SetWorkingStatus(gomock.Any(), agentID, taskID).Return(nil)
			},
		},
		{
			name: "no task — sets idle and returns null",
			setup: func(d toolsDeps, agentID uuid.UUID, wg *sync.WaitGroup) {
				projectID := uuid.New()
				agent := domainagent.Agent{ID: agentID, ProjectID: projectID, Role: "coder"}
				d.agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(agent, nil)
				d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil)
				d.agentRepo.EXPECT().SetIdleStatus(gomock.Any(), agentID).Return(nil)
				wg.Add(1)
				sweepOnce(d, wg)
				d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()
			},
			wantContains: "null",
		},
		{
			name: "merged and backlog tasks skipped — sets idle",
			setup: func(d toolsDeps, agentID uuid.UUID, wg *sync.WaitGroup) {
				projectID := uuid.New()
				agent := domainagent.Agent{ID: agentID, ProjectID: projectID, Role: "coder"}
				tasks := []domaintask.Task{
					{ID: uuid.New(), Status: domaintask.StatusMerged, Labels: []string{}},
					{ID: uuid.New(), Status: domaintask.StatusBacklog, Labels: []string{}},
				}
				d.agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(agent, nil)
				d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return(tasks, nil)
				d.agentRepo.EXPECT().SetIdleStatus(gomock.Any(), agentID).Return(nil)
				allowSweep(d)
			},
			wantContains: "null",
		},
		{
			name:       "invalid agent_id returns error text",
			agentIDStr: "not-a-uuid",
			setup:      func(d toolsDeps, agentID uuid.UUID, wg *sync.WaitGroup) {},
			wantContains: "error: invalid agent_id",
		},
		{
			name: "agent not found returns error text",
			setup: func(d toolsDeps, agentID uuid.UUID, wg *sync.WaitGroup) {
				d.agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(domainagent.Agent{}, errors.New("agent not found"))
			},
			wantContains: "error: agent not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			aSvc, tSvc, _, d := newToolsDeps(t)
			handler := claimTaskHandler(tSvc, aSvc)

			agentID := uuid.New()
			agentIDStr := tt.agentIDStr
			if agentIDStr == "" {
				agentIDStr = agentID.String()
			}

			var wg sync.WaitGroup
			tt.setup(d, agentID, &wg)

			result, err := handler(context.Background(), makeReq(map[string]any{"agent_id": agentIDStr}))
			wg.Wait()

			require.NoError(t, err)
			if tt.wantContains != "" {
				assert.Contains(t, resultText(result), tt.wantContains)
			}
		})
	}
}

// ── updateTaskStatusHandler ───────────────────────────────────────────────────

func TestUpdateTaskStatusHandler(t *testing.T) {
	tests := []struct {
		name         string
		args         map[string]any
		setup        func(d toolsDeps, taskID uuid.UUID)
		wantContains string
	}{
		{
			name: "valid transition returns ok:true",
			setup: func(d toolsDeps, taskID uuid.UUID) {
				projectID := uuid.New()
				task := domaintask.Task{ID: taskID, ProjectID: projectID, Status: domaintask.StatusInReview, Labels: []string{}}
				d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusInQA, domaintask.StatusInReview).Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil).AnyTimes()
				allowSweep(d)
			},
			wantContains: `"ok":true`,
		},
		{
			name:         "invalid transition returns error text",
			setup:        func(d toolsDeps, taskID uuid.UUID) {},
			wantContains: "invalid transition",
			args:         map[string]any{"from": "merged", "to": "ready"},
		},
		{
			name:         "garbage from status returns error",
			setup:        func(d toolsDeps, taskID uuid.UUID) {},
			wantContains: "error:",
			args:         map[string]any{"from": "garbage", "to": "ready"},
		},
		{
			name:         "invalid task_id returns error",
			setup:        func(d toolsDeps, taskID uuid.UUID) {},
			wantContains: "error: invalid task_id",
			args:         map[string]any{"task_id": "not-a-uuid", "from": "backlog", "to": "ready"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, tSvc, _, d := newToolsDeps(t)
			handler := updateTaskStatusHandler(tSvc)

			taskID := uuid.New()
			tt.setup(d, taskID)

			args := tt.args
			if args == nil {
				args = map[string]any{"task_id": taskID.String(), "from": "in_qa", "to": "in_review"}
			} else if _, hasTaskID := args["task_id"]; !hasTaskID {
				args["task_id"] = taskID.String()
			}

			result, err := handler(context.Background(), makeReq(args))
			require.NoError(t, err)
			assert.Contains(t, resultText(result), tt.wantContains)
		})
	}
}

// ── postMessageHandler ────────────────────────────────────────────────────────

func TestPostMessageHandler(t *testing.T) {
	// sharedAgentID is pre-declared so both the setup closure and the args map
	// can reference the same UUID for the "with agent_id" case.
	sharedAgentID := uuid.New()

	tests := []struct {
		name         string
		args         map[string]any
		setup        func(d toolsDeps)
		wantContains string
	}{
		{
			name: "success returns message ID",
			setup: func(d toolsDeps) {
				threadID := uuid.New()
				msgID := uuid.New()
				thread := domainthread.Thread{ID: threadID}
				msg := domainthread.Message{ID: msgID, ThreadID: threadID, Content: "progress!"}
				d.threadRepo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{thread}, nil)
				d.threadRepo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(msg, nil)
				d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)
			},
		},
		{
			name: "no thread returns error text",
			setup: func(d toolsDeps) {
				d.threadRepo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{}, nil)
			},
			wantContains: "thread not found",
		},
		{
			name: "with agent_id sets AgentID on message",
			setup: func(d toolsDeps) {
				threadID := uuid.New()
				thread := domainthread.Thread{ID: threadID}
				msg := domainthread.Message{ID: uuid.New(), AgentID: &sharedAgentID}
				d.threadRepo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{thread}, nil)
				d.threadRepo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).
					DoAndReturn(func(_ context.Context, m domainthread.Message) (domainthread.Message, error) {
						require.NotNil(t, m.AgentID)
						assert.Equal(t, sharedAgentID, *m.AgentID)
						return msg, nil
					})
				d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)
			},
			args: map[string]any{
				"task_id":   uuid.New().String(),
				"content":   "hello",
				"post_type": "progress",
				"agent_id":  sharedAgentID.String(),
			},
		},
		{
			name:         "invalid post_type returns error",
			setup:        func(d toolsDeps) {},
			wantContains: "error: invalid post_type",
			args:         map[string]any{"task_id": uuid.New().String(), "content": "hi", "post_type": "approve"},
		},
		{
			name:         "whitespace-only content returns error",
			setup:        func(d toolsDeps) {},
			wantContains: "error: content must not be empty",
			args:         map[string]any{"task_id": uuid.New().String(), "content": "   ", "post_type": "progress"},
		},
		{
			name:         "invalid task_id returns error",
			setup:        func(d toolsDeps) {},
			wantContains: "error: invalid task_id",
			args:         map[string]any{"task_id": "not-a-uuid", "content": "hi", "post_type": "progress"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, thSvc, d := newToolsDeps(t)
			handler := postMessageHandler(thSvc)

			tt.setup(d)

			args := tt.args
			if args == nil {
				args = map[string]any{
					"task_id":   uuid.New().String(),
					"content":   "progress!",
					"post_type": "progress",
				}
			}

			result, err := handler(context.Background(), makeReq(args))
			require.NoError(t, err)
			if tt.wantContains != "" {
				assert.Contains(t, resultText(result), tt.wantContains)
			}
		})
	}
}

// ── getTaskContextHandler ─────────────────────────────────────────────────────

func TestGetTaskContextHandler(t *testing.T) {
	tests := []struct {
		name         string
		taskIDStr    string
		setup        func(d toolsDeps, taskID uuid.UUID)
		wantContains string
	}{
		{
			name: "success returns task, dependencies, and thread",
			setup: func(d toolsDeps, taskID uuid.UUID) {
				task := domaintask.Task{ID: taskID, Labels: []string{}}
				thread := domainthread.Thread{ID: uuid.New()}
				msg := domainthread.Message{ID: uuid.New(), Content: "update"}
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
				d.taskRepo.EXPECT().GetDependencies(gomock.Any(), taskID).Return([]domaintask.Task{}, nil)
				d.threadRepo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{thread}, nil)
				d.threadRepo.EXPECT().ListMessages(gomock.Any(), thread.ID).Return([]domainthread.Message{msg}, nil)
			},
			wantContains: `"task"`,
		},
		{
			name: "task not found returns error",
			setup: func(d toolsDeps, taskID uuid.UUID) {
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(domaintask.Task{}, errors.New("not found"))
			},
			wantContains: "error:",
		},
		{
			name: "no threads — thread field is null",
			setup: func(d toolsDeps, taskID uuid.UUID) {
				task := domaintask.Task{ID: taskID, Labels: []string{}}
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
				d.taskRepo.EXPECT().GetDependencies(gomock.Any(), taskID).Return([]domaintask.Task{}, nil)
				d.threadRepo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{}, nil)
			},
			wantContains: `"thread":null`,
		},
		{
			name: "ListMessages fails — thread field is null (non-fatal)",
			setup: func(d toolsDeps, taskID uuid.UUID) {
				task := domaintask.Task{ID: taskID, Labels: []string{}}
				thread := domainthread.Thread{ID: uuid.New()}
				d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
				d.taskRepo.EXPECT().GetDependencies(gomock.Any(), taskID).Return([]domaintask.Task{}, nil)
				d.threadRepo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{thread}, nil)
				d.threadRepo.EXPECT().ListMessages(gomock.Any(), thread.ID).Return(nil, errors.New("db error"))
			},
			wantContains: `"thread":null`,
		},
		{
			name:         "invalid task_id returns error",
			taskIDStr:    "not-a-uuid",
			setup:        func(d toolsDeps, taskID uuid.UUID) {},
			wantContains: "error: invalid task_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, tSvc, thSvc, d := newToolsDeps(t)
			handler := getTaskContextHandler(tSvc, thSvc)

			taskID := uuid.New()
			tt.setup(d, taskID)

			taskIDStr := tt.taskIDStr
			if taskIDStr == "" {
				taskIDStr = taskID.String()
			}

			result, err := handler(context.Background(), makeReq(map[string]any{"task_id": taskIDStr}))
			require.NoError(t, err)
			assert.Contains(t, resultText(result), tt.wantContains)
		})
	}
}

// ── setPRUrlHandler ───────────────────────────────────────────────────────────

func TestSetPRUrlHandler(t *testing.T) {
	tests := []struct {
		name         string
		taskIDStr    string
		prURL        string
		setup        func(d toolsDeps, taskID uuid.UUID)
		wantContains string
	}{
		{
			name:  "success returns ok:true",
			prURL: "https://github.com/pr/1",
			setup: func(d toolsDeps, taskID uuid.UUID) {
				d.taskRepo.EXPECT().SetPRUrl(gomock.Any(), taskID, "https://github.com/pr/1").Return(nil)
				d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)
			},
			wantContains: `"ok":true`,
		},
		{
			name:         "empty pr_url returns error",
			prURL:        "",
			setup:        func(d toolsDeps, taskID uuid.UUID) {},
			wantContains: "error: pr_url required",
		},
		{
			name:         "invalid task_id returns error",
			taskIDStr:    "not-a-uuid",
			prURL:        "https://github.com/pr/1",
			setup:        func(d toolsDeps, taskID uuid.UUID) {},
			wantContains: "error: invalid task_id",
		},
		{
			name:  "service error returns error text",
			prURL: "https://github.com/pr/1",
			setup: func(d toolsDeps, taskID uuid.UUID) {
				d.taskRepo.EXPECT().SetPRUrl(gomock.Any(), taskID, gomock.Any()).Return(errors.New("db error"))
			},
			wantContains: "error:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, tSvc, _, d := newToolsDeps(t)
			handler := setPRUrlHandler(tSvc)

			taskID := uuid.New()
			tt.setup(d, taskID)

			taskIDStr := tt.taskIDStr
			if taskIDStr == "" {
				taskIDStr = taskID.String()
			}

			result, err := handler(context.Background(), makeReq(map[string]any{
				"task_id": taskIDStr,
				"pr_url":  tt.prURL,
			}))
			require.NoError(t, err)
			assert.Contains(t, resultText(result), tt.wantContains)
		})
	}
}

// ── listMessagesHandler ───────────────────────────────────────────────────────

func TestListMessagesHandler(t *testing.T) {
	tests := []struct {
		name         string
		taskIDStr    string
		setup        func(d toolsDeps)
		wantContains string
	}{
		{
			name: "success returns messages",
			setup: func(d toolsDeps) {
				threadID := uuid.New()
				thread := domainthread.Thread{ID: threadID}
				msgs := []domainthread.Message{{ID: uuid.New(), Content: "msg1"}}
				d.threadRepo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{thread}, nil)
				d.threadRepo.EXPECT().ListMessages(gomock.Any(), threadID).Return(msgs, nil)
			},
			wantContains: "msg1",
		},
		{
			name: "no thread returns empty array",
			setup: func(d toolsDeps) {
				d.threadRepo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{}, nil)
			},
			wantContains: "[]",
		},
		{
			name: "ListMessages fails returns empty array",
			setup: func(d toolsDeps) {
				threadID := uuid.New()
				thread := domainthread.Thread{ID: threadID}
				d.threadRepo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{thread}, nil)
				d.threadRepo.EXPECT().ListMessages(gomock.Any(), threadID).Return(nil, errors.New("db error"))
			},
			wantContains: "[]",
		},
		{
			name:         "invalid task_id returns error",
			taskIDStr:    "not-a-uuid",
			setup:        func(d toolsDeps) {},
			wantContains: "error: invalid task_id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, thSvc, d := newToolsDeps(t)
			handler := listMessagesHandler(thSvc)

			tt.setup(d)

			taskIDStr := tt.taskIDStr
			if taskIDStr == "" {
				taskIDStr = uuid.New().String()
			}

			result, err := handler(context.Background(), makeReq(map[string]any{"task_id": taskIDStr}))
			require.NoError(t, err)
			assert.Contains(t, resultText(result), tt.wantContains)
		})
	}
}
