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

	"github.com/alanyang/agent-mesh/internal/domain/pipeline"
	domaintask "github.com/alanyang/agent-mesh/internal/domain/task"
	domainthread "github.com/alanyang/agent-mesh/internal/domain/thread"
	"github.com/alanyang/agent-mesh/internal/mocks"
	agentsvc "github.com/alanyang/agent-mesh/internal/service/agent"
	tasksvc "github.com/alanyang/agent-mesh/internal/service/task"
	threadsvc "github.com/alanyang/agent-mesh/internal/service/thread"
	domainagent "github.com/alanyang/agent-mesh/internal/domain/agent"
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

func TestRegisterAgent_NewAgent(t *testing.T) {
	aSvc, tSvc, _, d := newToolsDeps(t)
	reg := NewSessionRegistry()
	handler := registerAgentHandler(nil, reg, aSvc, tSvc, validRolesForTest())

	projectID := uuid.New()
	agentID := uuid.New()
	expected := domainagent.Agent{ID: agentID, ProjectID: projectID, Role: "coder", Status: domainagent.StatusIdle}

	d.agentRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(expected, nil)
	d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)
	// Sweep goroutine.
	var wg sync.WaitGroup
	wg.Add(1)
	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error {
			defer wg.Done()
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()

	result, err := handler(context.Background(), makeReq(map[string]any{
		"project_id": projectID.String(),
		"role":       "coder",
		"name":       "bot",
		"model":      "gpt4",
	}))
	wg.Wait()
	require.NoError(t, err)
	text := resultText(result)
	assert.Contains(t, text, agentID.String())
}

func TestRegisterAgent_InvalidProjectID(t *testing.T) {
	aSvc, tSvc, _, _ := newToolsDeps(t)
	handler := registerAgentHandler(nil, NewSessionRegistry(), aSvc, tSvc, validRolesForTest())

	result, err := handler(context.Background(), makeReq(map[string]any{
		"project_id": "not-a-uuid",
		"role":       "coder",
		"name":       "bot",
		"model":      "gpt4",
	}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), "error: invalid project_id")
}

func TestRegisterAgent_InvalidRole(t *testing.T) {
	aSvc, tSvc, _, _ := newToolsDeps(t)
	handler := registerAgentHandler(nil, NewSessionRegistry(), aSvc, tSvc, validRolesForTest())

	result, err := handler(context.Background(), makeReq(map[string]any{
		"project_id": uuid.New().String(),
		"role":       "admin", // invalid role
		"name":       "bot",
		"model":      "gpt4",
	}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), "error: invalid role")
}

func TestRegisterAgent_Reconnect_Success(t *testing.T) {
	aSvc, tSvc, _, d := newToolsDeps(t)
	handler := registerAgentHandler(nil, NewSessionRegistry(), aSvc, tSvc, validRolesForTest())

	existingID := uuid.New()
	projectID := uuid.New()
	stored := domainagent.Agent{ID: existingID, ProjectID: projectID, Role: "coder", Status: domainagent.StatusIdle}

	d.agentRepo.EXPECT().GetByID(gomock.Any(), existingID).Return(stored, nil)
	d.agentRepo.EXPECT().UpdateStatus(gomock.Any(), existingID, domainagent.StatusIdle).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)
	// Sweep goroutine.
	var wg sync.WaitGroup
	wg.Add(1)
	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error {
			defer wg.Done()
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()

	result, err := handler(context.Background(), makeReq(map[string]any{
		"project_id": projectID.String(),
		"role":       "coder",
		"name":       "bot",
		"model":      "gpt4",
		"agent_id":   existingID.String(),
	}))
	wg.Wait()
	require.NoError(t, err)
	assert.Contains(t, resultText(result), existingID.String())
}

func TestRegisterAgent_Reconnect_NotFound_FallsThrough(t *testing.T) {
	// agent_id provided but not found → falls through to create new agent.
	aSvc, tSvc, _, d := newToolsDeps(t)
	handler := registerAgentHandler(nil, NewSessionRegistry(), aSvc, tSvc, validRolesForTest())

	existingID := uuid.New()
	newAgentID := uuid.New()
	projectID := uuid.New()
	newAgent := domainagent.Agent{ID: newAgentID, ProjectID: projectID, Role: "coder", Status: domainagent.StatusIdle}

	d.agentRepo.EXPECT().GetByID(gomock.Any(), existingID).Return(domainagent.Agent{}, errors.New("not found"))
	d.agentRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(newAgent, nil)
	d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)
	allowSweep(d)

	result, err := handler(context.Background(), makeReq(map[string]any{
		"project_id": projectID.String(),
		"role":       "coder",
		"name":       "bot",
		"model":      "gpt4",
		"agent_id":   existingID.String(),
	}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), newAgentID.String())
}

func TestRegisterAgent_SkillsNotSupported(t *testing.T) {
	// Handler always passes []string{} for skills regardless of input.
	aSvc, tSvc, _, d := newToolsDeps(t)
	handler := registerAgentHandler(nil, NewSessionRegistry(), aSvc, tSvc, validRolesForTest())
	projectID := uuid.New()
	agentID := uuid.New()

	d.agentRepo.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, a domainagent.Agent) (domainagent.Agent, error) {
			assert.Empty(t, a.Skills, "handler must pass empty skills — skills are a V2 concern")
			return domainagent.Agent{ID: agentID, ProjectID: projectID, Role: "coder"}, nil
		})
	d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)
	allowSweep(d)

	_, err := handler(context.Background(), makeReq(map[string]any{
		"project_id": projectID.String(),
		"role":       "coder",
		"name":       "bot",
		"model":      "gpt4",
	}))
	require.NoError(t, err)
}

// ── claimTaskHandler ──────────────────────────────────────────────────────────

func TestClaimTask_TaskAssigned_SetsWorking(t *testing.T) {
	aSvc, tSvc, _, d := newToolsDeps(t)
	handler := claimTaskHandler(tSvc, aSvc)

	agentID := uuid.New()
	projectID := uuid.New()
	taskID := uuid.New()
	agent := domainagent.Agent{ID: agentID, ProjectID: projectID, Role: "coder"}
	task := domaintask.Task{ID: taskID, ProjectID: projectID, Status: domaintask.StatusInProgress, Labels: []string{}}

	d.agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(agent, nil)
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{task}, nil)
	d.agentRepo.EXPECT().SetWorkingStatus(gomock.Any(), agentID, taskID).Return(nil)

	result, err := handler(context.Background(), makeReq(map[string]any{"agent_id": agentID.String()}))
	require.NoError(t, err)
	text := resultText(result)
	assert.Contains(t, text, taskID.String())
}

func TestClaimTask_NoTask_SetsIdle(t *testing.T) {
	aSvc, tSvc, _, d := newToolsDeps(t)
	handler := claimTaskHandler(tSvc, aSvc)

	agentID := uuid.New()
	projectID := uuid.New()
	agent := domainagent.Agent{ID: agentID, ProjectID: projectID, Role: "coder"}

	d.agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(agent, nil)
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil)
	d.agentRepo.EXPECT().SetIdleStatus(gomock.Any(), agentID).Return(nil)
	// Sweep goroutine.
	var wg sync.WaitGroup
	wg.Add(1)
	d.locker.EXPECT().WithLock(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ int64, fn func(context.Context) error) error {
			defer wg.Done()
			return fn(ctx)
		})
	d.taskRepo.EXPECT().List(gomock.Any(), gomock.Any()).Return([]domaintask.Task{}, nil).AnyTimes()

	result, err := handler(context.Background(), makeReq(map[string]any{"agent_id": agentID.String()}))
	wg.Wait()
	require.NoError(t, err)
	assert.Equal(t, "null", resultText(result))
}

func TestClaimTask_SkipsTerminalTasks(t *testing.T) {
	// merged and backlog are both skipped.
	aSvc, tSvc, _, d := newToolsDeps(t)
	handler := claimTaskHandler(tSvc, aSvc)

	agentID := uuid.New()
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

	result, err := handler(context.Background(), makeReq(map[string]any{"agent_id": agentID.String()}))
	require.NoError(t, err)
	assert.Equal(t, "null", resultText(result))
}

func TestClaimTask_InvalidAgentID(t *testing.T) {
	aSvc, tSvc, _, _ := newToolsDeps(t)
	handler := claimTaskHandler(tSvc, aSvc)

	result, err := handler(context.Background(), makeReq(map[string]any{"agent_id": "not-a-uuid"}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), "error: invalid agent_id")
}

func TestClaimTask_AgentNotFound(t *testing.T) {
	aSvc, tSvc, _, d := newToolsDeps(t)
	handler := claimTaskHandler(tSvc, aSvc)

	agentID := uuid.New()
	d.agentRepo.EXPECT().GetByID(gomock.Any(), agentID).Return(domainagent.Agent{}, errors.New("agent not found"))

	result, err := handler(context.Background(), makeReq(map[string]any{"agent_id": agentID.String()}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), "error: agent not found")
}

// ── updateTaskStatusHandler ───────────────────────────────────────────────────

func TestUpdateTaskStatus_ValidTransition(t *testing.T) {
	_, tSvc, _, d := newToolsDeps(t)
	handler := updateTaskStatusHandler(tSvc)

	taskID := uuid.New()
	projectID := uuid.New()
	// Use in_qa→in_review: reviewer gets distributed (or not), sweep for "qa" fires.
	task := domaintask.Task{ID: taskID, ProjectID: projectID, Status: domaintask.StatusInReview, Labels: []string{}}

	d.taskRepo.EXPECT().UpdateStatus(gomock.Any(), taskID, domaintask.StatusInQA, domaintask.StatusInReview).Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil).AnyTimes()
	allowSweep(d)

	result, err := handler(context.Background(), makeReq(map[string]any{
		"task_id": taskID.String(),
		"from":    "in_qa",
		"to":      "in_review",
	}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), `"ok":true`)
}

func TestUpdateTaskStatus_InvalidTransition(t *testing.T) {
	_, tSvc, _, _ := newToolsDeps(t)
	handler := updateTaskStatusHandler(tSvc)

	result, err := handler(context.Background(), makeReq(map[string]any{
		"task_id": uuid.New().String(),
		"from":    "merged",
		"to":      "ready",
	}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), "error:")
	assert.Contains(t, resultText(result), "invalid transition")
}

func TestUpdateTaskStatus_GarbageFromStatus(t *testing.T) {
	_, tSvc, _, _ := newToolsDeps(t)
	handler := updateTaskStatusHandler(tSvc)

	result, err := handler(context.Background(), makeReq(map[string]any{
		"task_id": uuid.New().String(),
		"from":    "garbage",
		"to":      "ready",
	}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), "error:")
}

func TestUpdateTaskStatus_InvalidTaskID(t *testing.T) {
	_, tSvc, _, _ := newToolsDeps(t)
	handler := updateTaskStatusHandler(tSvc)

	result, err := handler(context.Background(), makeReq(map[string]any{
		"task_id": "not-a-uuid",
		"from":    "backlog",
		"to":      "ready",
	}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), "error: invalid task_id")
}

// ── postMessageHandler ────────────────────────────────────────────────────────

func TestPostMessage_Success(t *testing.T) {
	_, _, thSvc, d := newToolsDeps(t)
	handler := postMessageHandler(thSvc)

	taskID := uuid.New()
	threadID := uuid.New()
	msgID := uuid.New()
	thread := domainthread.Thread{ID: threadID}
	msg := domainthread.Message{ID: msgID, ThreadID: threadID, Content: "progress!"}

	d.threadRepo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{thread}, nil)
	d.threadRepo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).Return(msg, nil)
	d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)

	result, err := handler(context.Background(), makeReq(map[string]any{
		"task_id":   taskID.String(),
		"content":   "progress!",
		"post_type": "progress",
	}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), msgID.String())
}

func TestPostMessage_NoThread(t *testing.T) {
	_, _, thSvc, d := newToolsDeps(t)
	handler := postMessageHandler(thSvc)

	d.threadRepo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{}, nil)

	result, err := handler(context.Background(), makeReq(map[string]any{
		"task_id":   uuid.New().String(),
		"content":   "hello",
		"post_type": "progress",
	}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), "error:")
	assert.Contains(t, resultText(result), "thread not found")
}

func TestPostMessage_WithAgentID(t *testing.T) {
	_, _, thSvc, d := newToolsDeps(t)
	handler := postMessageHandler(thSvc)

	taskID := uuid.New()
	agentID := uuid.New()
	threadID := uuid.New()
	thread := domainthread.Thread{ID: threadID}
	msg := domainthread.Message{ID: uuid.New(), AgentID: &agentID}

	d.threadRepo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{thread}, nil)
	d.threadRepo.EXPECT().CreateMessage(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, m domainthread.Message) (domainthread.Message, error) {
			require.NotNil(t, m.AgentID)
			assert.Equal(t, agentID, *m.AgentID)
			return msg, nil
		})
	d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)

	_, err := handler(context.Background(), makeReq(map[string]any{
		"task_id":   taskID.String(),
		"content":   "hello",
		"post_type": "progress",
		"agent_id":  agentID.String(),
	}))
	require.NoError(t, err)
}

func TestPostMessage_InvalidPostType(t *testing.T) {
	_, _, thSvc, _ := newToolsDeps(t)
	handler := postMessageHandler(thSvc)

	result, err := handler(context.Background(), makeReq(map[string]any{
		"task_id":   uuid.New().String(),
		"content":   "hi",
		"post_type": "approve", // invalid
	}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), "error: invalid post_type")
}

func TestPostMessage_EmptyContent(t *testing.T) {
	_, _, thSvc, _ := newToolsDeps(t)
	handler := postMessageHandler(thSvc)

	result, err := handler(context.Background(), makeReq(map[string]any{
		"task_id":   uuid.New().String(),
		"content":   "   ", // whitespace only
		"post_type": "progress",
	}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), "error: content must not be empty")
}

func TestPostMessage_InvalidTaskID(t *testing.T) {
	_, _, thSvc, _ := newToolsDeps(t)
	handler := postMessageHandler(thSvc)

	result, err := handler(context.Background(), makeReq(map[string]any{
		"task_id":   "not-a-uuid",
		"content":   "hi",
		"post_type": "progress",
	}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), "error: invalid task_id")
}

// ── getTaskContextHandler ─────────────────────────────────────────────────────

func TestGetTaskContext_Success(t *testing.T) {
	_, tSvc, thSvc, d := newToolsDeps(t)
	handler := getTaskContextHandler(tSvc, thSvc)

	taskID := uuid.New()
	task := domaintask.Task{ID: taskID, Labels: []string{}}
	thread := domainthread.Thread{ID: uuid.New()}
	msg := domainthread.Message{ID: uuid.New(), Content: "update"}

	d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
	d.taskRepo.EXPECT().GetDependencies(gomock.Any(), taskID).Return([]domaintask.Task{}, nil)
	d.threadRepo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{thread}, nil)
	d.threadRepo.EXPECT().ListMessages(gomock.Any(), thread.ID).Return([]domainthread.Message{msg}, nil)

	result, err := handler(context.Background(), makeReq(map[string]any{"task_id": taskID.String()}))
	require.NoError(t, err)
	text := resultText(result)
	assert.Contains(t, text, `"task"`)
	assert.Contains(t, text, `"dependencies"`)
	assert.Contains(t, text, `"thread"`)
}

func TestGetTaskContext_TaskNotFound(t *testing.T) {
	_, tSvc, thSvc, d := newToolsDeps(t)
	handler := getTaskContextHandler(tSvc, thSvc)

	taskID := uuid.New()
	d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(domaintask.Task{}, errors.New("not found"))

	result, err := handler(context.Background(), makeReq(map[string]any{"task_id": taskID.String()}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), "error:")
}

func TestGetTaskContext_NoThreads(t *testing.T) {
	// No threads → thread field is null in response.
	_, tSvc, thSvc, d := newToolsDeps(t)
	handler := getTaskContextHandler(tSvc, thSvc)

	taskID := uuid.New()
	task := domaintask.Task{ID: taskID, Labels: []string{}}

	d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
	d.taskRepo.EXPECT().GetDependencies(gomock.Any(), taskID).Return([]domaintask.Task{}, nil)
	d.threadRepo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{}, nil)

	result, err := handler(context.Background(), makeReq(map[string]any{"task_id": taskID.String()}))
	require.NoError(t, err)
	text := resultText(result)
	// messages stays nil → serialized as null.
	assert.Contains(t, text, `"thread":null`)
}

func TestGetTaskContext_ListMessagesFails(t *testing.T) {
	// ListMessages fails → thread field is null (non-fatal).
	_, tSvc, thSvc, d := newToolsDeps(t)
	handler := getTaskContextHandler(tSvc, thSvc)

	taskID := uuid.New()
	task := domaintask.Task{ID: taskID, Labels: []string{}}
	thread := domainthread.Thread{ID: uuid.New()}

	d.taskRepo.EXPECT().GetByID(gomock.Any(), taskID).Return(task, nil)
	d.taskRepo.EXPECT().GetDependencies(gomock.Any(), taskID).Return([]domaintask.Task{}, nil)
	d.threadRepo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{thread}, nil)
	d.threadRepo.EXPECT().ListMessages(gomock.Any(), thread.ID).Return(nil, errors.New("db error"))

	result, err := handler(context.Background(), makeReq(map[string]any{"task_id": taskID.String()}))
	require.NoError(t, err)
	text := resultText(result)
	assert.Contains(t, text, `"thread":null`)
}

func TestGetTaskContext_InvalidTaskID(t *testing.T) {
	_, tSvc, thSvc, _ := newToolsDeps(t)
	handler := getTaskContextHandler(tSvc, thSvc)

	result, err := handler(context.Background(), makeReq(map[string]any{"task_id": "not-a-uuid"}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), "error: invalid task_id")
}

// ── setPRUrlHandler ───────────────────────────────────────────────────────────

func TestSetPRUrl_Success(t *testing.T) {
	_, tSvc, _, d := newToolsDeps(t)
	handler := setPRUrlHandler(tSvc)

	taskID := uuid.New()
	d.taskRepo.EXPECT().SetPRUrl(gomock.Any(), taskID, "https://github.com/pr/1").Return(nil)
	d.bus.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil)

	result, err := handler(context.Background(), makeReq(map[string]any{
		"task_id": taskID.String(),
		"pr_url":  "https://github.com/pr/1",
	}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), `"ok":true`)
}

func TestSetPRUrl_EmptyURL(t *testing.T) {
	_, tSvc, _, _ := newToolsDeps(t)
	handler := setPRUrlHandler(tSvc)

	result, err := handler(context.Background(), makeReq(map[string]any{
		"task_id": uuid.New().String(),
		"pr_url":  "",
	}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), "error: pr_url required")
}

func TestSetPRUrl_InvalidTaskID(t *testing.T) {
	_, tSvc, _, _ := newToolsDeps(t)
	handler := setPRUrlHandler(tSvc)

	result, err := handler(context.Background(), makeReq(map[string]any{
		"task_id": "not-a-uuid",
		"pr_url":  "https://github.com/pr/1",
	}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), "error: invalid task_id")
}

func TestSetPRUrl_ServiceError(t *testing.T) {
	_, tSvc, _, d := newToolsDeps(t)
	handler := setPRUrlHandler(tSvc)

	taskID := uuid.New()
	d.taskRepo.EXPECT().SetPRUrl(gomock.Any(), taskID, gomock.Any()).Return(errors.New("db error"))

	result, err := handler(context.Background(), makeReq(map[string]any{
		"task_id": taskID.String(),
		"pr_url":  "https://github.com/pr/1",
	}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), "error:")
}

// ── listMessagesHandler ───────────────────────────────────────────────────────

func TestListMessages_Success(t *testing.T) {
	_, _, thSvc, d := newToolsDeps(t)
	handler := listMessagesHandler(thSvc)

	taskID := uuid.New()
	threadID := uuid.New()
	thread := domainthread.Thread{ID: threadID}
	msgs := []domainthread.Message{{ID: uuid.New(), Content: "msg1"}}

	d.threadRepo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{thread}, nil)
	d.threadRepo.EXPECT().ListMessages(gomock.Any(), threadID).Return(msgs, nil)

	result, err := handler(context.Background(), makeReq(map[string]any{"task_id": taskID.String()}))
	require.NoError(t, err)
	text := resultText(result)
	assert.Contains(t, text, "msg1")
}

func TestListMessages_NoThread(t *testing.T) {
	_, _, thSvc, d := newToolsDeps(t)
	handler := listMessagesHandler(thSvc)

	d.threadRepo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{}, nil)

	result, err := handler(context.Background(), makeReq(map[string]any{"task_id": uuid.New().String()}))
	require.NoError(t, err)
	assert.Equal(t, "[]", resultText(result))
}

func TestListMessages_ListMessagesFails(t *testing.T) {
	_, _, thSvc, d := newToolsDeps(t)
	handler := listMessagesHandler(thSvc)

	threadID := uuid.New()
	thread := domainthread.Thread{ID: threadID}

	d.threadRepo.EXPECT().ListThreads(gomock.Any(), gomock.Any()).Return([]domainthread.Thread{thread}, nil)
	d.threadRepo.EXPECT().ListMessages(gomock.Any(), threadID).Return(nil, errors.New("db error"))

	result, err := handler(context.Background(), makeReq(map[string]any{"task_id": uuid.New().String()}))
	require.NoError(t, err)
	assert.Equal(t, "[]", resultText(result))
}

func TestListMessages_InvalidTaskID(t *testing.T) {
	_, _, thSvc, _ := newToolsDeps(t)
	handler := listMessagesHandler(thSvc)

	result, err := handler(context.Background(), makeReq(map[string]any{"task_id": "not-a-uuid"}))
	require.NoError(t, err)
	assert.Contains(t, resultText(result), "error: invalid task_id")
}
