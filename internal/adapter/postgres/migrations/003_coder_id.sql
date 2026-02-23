-- migration 003: add coder_id to tasks for bounce-back preferred routing
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS coder_id UUID REFERENCES agents(id);
