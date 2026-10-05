-- A conversation with the assistant.
CREATE TABLE chats (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    -- idle: nothing in progress. running: the assistant is working.
    -- waiting: the assistant asked the user something and is waiting for it.
    status TEXT NOT NULL CHECK (status IN ('idle', 'running', 'waiting')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX chats_owner_updated ON chats (owner_id, updated_at DESC);

-- Everything that happened in a chat, in order: what the user said, what the
-- assistant said and asked for, what its tools returned, and progress notes.
-- Append-only; the conversation sent to the model is rebuilt from it.
CREATE TABLE chat_events (
    chat_id UUID NOT NULL REFERENCES chats (id) ON DELETE CASCADE,
    seq BIGINT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('user', 'assistant', 'tool', 'activity', 'agent', 'error')),
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chat_id, seq)
);

-- A coding agent: one conversation with the agent program inside a machine,
-- started by a chat and known within it by a name. A machine can have several,
-- working at the same time. This is the table of what is running.
CREATE TABLE agents (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    chat_id UUID NOT NULL REFERENCES chats (id) ON DELETE CASCADE,
    instance_id UUID NOT NULL REFERENCES instances (id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    -- idle: nothing to do (never given a task, or finished its last one).
    -- working: carrying out a task. failed, stopped: how its last task ended.
    status TEXT NOT NULL CHECK (status IN ('idle', 'working', 'failed', 'stopped')),
    -- The agent program's own ID for the conversation, which is how the agent
    -- remembers its earlier tasks. NULL until its first task.
    session_id TEXT,
    -- The task it is working on, or last worked on.
    task TEXT NOT NULL DEFAULT '',
    -- Whether that task was started without anyone waiting for it.
    background BOOLEAN NOT NULL DEFAULT FALSE,
    -- What it is doing right now, while working.
    activity TEXT NOT NULL DEFAULT '',
    -- What came of its last task: its report, or why it failed.
    report TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (chat_id, instance_id, name)
);

CREATE INDEX agents_owner_updated ON agents (owner_id, updated_at DESC);

-- Every task an agent has been given: what was asked, what it did step by
-- step, and how it ended. Together these are what one agent can know about
-- another.
CREATE TABLE agent_tasks (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    agent_id UUID NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    task TEXT NOT NULL,
    background BOOLEAN NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('working', 'done', 'failed', 'stopped')),
    -- What it did, a line for each step, in order.
    log TEXT[] NOT NULL DEFAULT '{}',
    -- Its report, or why it failed.
    report TEXT NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ
);

CREATE INDEX agent_tasks_agent ON agent_tasks (agent_id, id DESC);

-- Things agents have written down for each other: an API's shape, a port, a
-- decision. Shared by all of one user's agents.
CREATE TABLE agent_notes (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- The machine it was written from, by name: the note outlives the machine.
    machine TEXT NOT NULL,
    -- The agent that wrote it, as it named itself.
    author TEXT NOT NULL,
    topic TEXT NOT NULL,
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX agent_notes_owner ON agent_notes (owner_id, id DESC);
