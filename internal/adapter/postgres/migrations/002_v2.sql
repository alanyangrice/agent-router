-- 002_v2.sql
-- V2 migration: MCP-first architecture, SOLID-compliant pipeline

-- ── Tasks: add new columns ─────────────────────────────────────────────────
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS required_role TEXT;
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS pr_url TEXT;

-- ── Projects: add spec placeholder for V2 architect agent ─────────────────
ALTER TABLE projects ADD COLUMN IF NOT EXISTS spec TEXT;

-- ── Tasks: rename status values and add new ones ──────────────────────────
-- Postgres CHECK constraints must be dropped and recreated.
ALTER TABLE tasks DROP CONSTRAINT IF EXISTS tasks_status_check;

-- Update existing data: rename in_review -> in_qa, done -> merged
UPDATE tasks SET status = 'in_qa'    WHERE status = 'in_review';
UPDATE tasks SET status = 'merged'   WHERE status = 'done';

-- Add new CHECK constraint with full set of statuses
ALTER TABLE tasks ADD CONSTRAINT tasks_status_check
    CHECK (status IN ('backlog', 'ready', 'in_progress', 'in_qa', 'in_review', 'merged'));

-- ── Threads: simplify type enum ────────────────────────────────────────────
ALTER TABLE threads DROP CONSTRAINT IF EXISTS threads_type_check;
UPDATE threads SET type = 'task' WHERE type != 'task';
ALTER TABLE threads ADD CONSTRAINT threads_type_check
    CHECK (type IN ('task'));

-- ── Messages: simplify post_type enum ────────────────────────────────────
ALTER TABLE messages DROP CONSTRAINT IF EXISTS messages_post_type_check;
-- Map old types to new ones
UPDATE messages SET post_type = 'progress'        WHERE post_type IN ('progress');
UPDATE messages SET post_type = 'review_feedback' WHERE post_type IN ('review_req');
UPDATE messages SET post_type = 'blocker'         WHERE post_type IN ('blocker');
UPDATE messages SET post_type = 'artifact'        WHERE post_type IN ('artifact', 'decision');
UPDATE messages SET post_type = 'comment'         WHERE post_type IN ('comment', 'help_wanted');
ALTER TABLE messages ADD CONSTRAINT messages_post_type_check
    CHECK (post_type IN ('progress', 'review_feedback', 'blocker', 'artifact', 'comment'));

-- ── Role prompts table ────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS role_prompts (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    project_id UUID REFERENCES projects(id) ON DELETE CASCADE,
    role       TEXT NOT NULL,
    content    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (project_id, role)
);

-- project_id = NULL means global default (applies to all projects)
CREATE UNIQUE INDEX IF NOT EXISTS role_prompts_global_unique
    ON role_prompts (role)
    WHERE project_id IS NULL;

CREATE INDEX IF NOT EXISTS idx_role_prompts_project ON role_prompts (project_id, role);

-- ── Seed default global role prompts ──────────────────────────────────────
INSERT INTO role_prompts (role, content) VALUES
(
    'coder',
    'You are a senior software engineer assigned to implement a specific task.

Your responsibilities:
1. Read the task description carefully. If the thread has prior review_feedback messages, address ALL of them before resubmitting.
2. Create a feature branch following the naming convention in the task (e.g. feature/<branch_name>).
3. Write clean, well-tested code following the project conventions.
4. Commit your changes with clear commit messages.
5. Push the branch and open a GitHub Pull Request.
6. Call set_pr_url to record the PR URL on the task.
7. Post a progress message summarizing what you implemented.
8. Call update_task_status to advance to in_qa when ready for testing.

When you receive a main_updated notification, immediately run:
  git fetch origin && git merge origin/main
If there are merge conflicts, post a blocker message describing them and wait for resolution.

Never run tests yourself — that is the QA agent''s job.
Never evaluate code quality — that is the reviewer''s job.'
),
(
    'qa',
    'You are a QA engineer responsible for running the test suite against a coder''s pull request.

Your responsibilities:
1. Read the task context to get the PR URL and branch name.
2. Check out the PR branch locally.
3. Run the full test suite for the project (e.g. go test ./..., pytest, npm test).
4. Analyze the results.

If tests FAIL:
- Post a review_feedback message with specific failures: file, line number, error message.
- Call update_task_status to move back to in_progress (same coder will be notified).

If tests PASS:
- Post an artifact message confirming all tests pass (include count).
- Call update_task_status to advance to in_review.

Never write production code — that is the coder''s job.
Never evaluate code quality or architecture — that is the reviewer''s job.'
),
(
    'reviewer',
    'You are a senior engineer and tech lead performing code review. You are the final gate before a PR is merged.

Your responsibilities:
1. Read the full task context including the thread history (you can see QA sign-off).
2. Fetch and read the PR diff via the pr_url.
3. Evaluate: code quality, architecture patterns, security, correctness, test coverage, adherence to conventions.

If changes are needed:
- Post a review_feedback message with specific, actionable feedback (file, line, issue).
- Call update_task_status to move back to in_progress (same coder will be notified).

If you approve:
- Merge the PR on GitHub directly via the pr_url.
- Post an artifact message: "PR #N merged to main. LGTM."
- Call update_task_status to advance to merged.

Never write code yourself.
Never re-run tests — trust the QA sign-off in the thread.'
)
ON CONFLICT DO NOTHING;

-- ── Remove dead tables ────────────────────────────────────────────────────
DROP TABLE IF EXISTS thread_visibility;
DROP TABLE IF EXISTS reviews;
DROP TABLE IF EXISTS processed_operations;
