-- Which toolchain a machine was created with (see images/workspace).
ALTER TABLE instances ADD COLUMN stack TEXT NOT NULL DEFAULT 'base';

-- A message from one agent to another shows in the chat as its own kind of
-- event.
ALTER TABLE chat_events DROP CONSTRAINT chat_events_kind_check;
ALTER TABLE chat_events ADD CONSTRAINT chat_events_kind_check
    CHECK (kind IN ('user', 'assistant', 'tool', 'activity', 'agent', 'message', 'error'));

-- The design of what a chat is building: its components, which machine each
-- is on, and how they connect. One for each chat.
CREATE TABLE architectures (
    chat_id UUID PRIMARY KEY REFERENCES chats (id) ON DELETE CASCADE,
    owner_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- proposed: put to the user, who has not agreed to it yet.
    -- approved: agreed, and what is being built.
    status TEXT NOT NULL CHECK (status IN ('proposed', 'approved')),
    -- The components, connections and decisions. Checked by the application,
    -- which is the only thing that reads it.
    design JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX architectures_owner ON architectures (owner_id);

-- Programs the platform keeps running on a machine: started when the machine
-- starts, restarted if they exit. Agents register what they build here, so
-- that it is still up after the machine has been stopped and started.
CREATE TABLE services (
    instance_id UUID NOT NULL REFERENCES instances (id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    command TEXT NOT NULL,
    -- The folder the command is run in.
    directory TEXT NOT NULL,
    -- The port it listens on, if it listens on one.
    port INT CHECK (port BETWEEN 1 AND 65535),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (instance_id, name)
);

-- Questions one agent has for another that was busy when asked. The other
-- gets them with its next task.
CREATE TABLE agent_messages (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    to_agent_id UUID NOT NULL REFERENCES agents (id) ON DELETE CASCADE,
    -- Who asked: the machine it was sent from, and the agent as it named itself.
    from_machine TEXT NOT NULL,
    from_agent TEXT NOT NULL,
    body TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at TIMESTAMPTZ
);

CREATE INDEX agent_messages_pending ON agent_messages (to_agent_id) WHERE delivered_at IS NULL;
