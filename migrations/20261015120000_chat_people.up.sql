-- Each agent has a name of its own, like a person's, by which the owner
-- addresses it in a chat ("@tom"). The platform chooses it; no two of an
-- owner's agents share one, in any chat. `name` stays what it was: the
-- agent's job ("frontend"), chosen by the assistant.
ALTER TABLE agents ADD COLUMN handle TEXT;

-- Agents from before get names from the start of the same list the platform
-- uses (internal/assistant/handles.go), in the order they were created.
WITH list AS (
    SELECT ARRAY['tom', 'ava', 'max', 'zoe', 'sam', 'mia', 'leo', 'ivy', 'ben', 'eva',
                 'jay', 'kai', 'noa', 'rex', 'uma', 'ted', 'liv', 'finn', 'nia', 'ray']::TEXT[] AS names
), numbered AS (
    SELECT id, row_number() OVER (PARTITION BY owner_id ORDER BY created_at, id) - 1 AS n
    FROM agents
)
UPDATE agents a
SET handle = list.names[1 + numbered.n % cardinality(list.names)]
    || CASE WHEN numbered.n >= cardinality(list.names)
            THEN (1 + numbered.n / cardinality(list.names))::TEXT ELSE '' END
FROM numbered, list
WHERE a.id = numbered.id;

ALTER TABLE agents ALTER COLUMN handle SET NOT NULL;
CREATE UNIQUE INDEX agents_owner_handle ON agents (owner_id, handle);

-- An agent writing in a chat, on its own or in answer to the owner, is an
-- event of its own kind.
ALTER TABLE chat_events DROP CONSTRAINT chat_events_kind_check;
ALTER TABLE chat_events ADD CONSTRAINT chat_events_kind_check
    CHECK (kind IN ('user', 'assistant', 'tool', 'activity', 'agent', 'message', 'post', 'error'));

-- Files people and agents put in a chat: images, documents, data. Kept here,
-- whole, and limited in size by the application (10 MB each).
CREATE TABLE attachments (
    id UUID PRIMARY KEY,
    owner_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    chat_id UUID NOT NULL REFERENCES chats (id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size INT NOT NULL CHECK (size >= 0),
    data BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX attachments_chat ON attachments (chat_id);

-- A message the owner wrote to an agent that was busy waits here like a
-- question from another agent, with the files that came with it and the
-- chat event it was, so that the answer can be shown as a reply to it.
ALTER TABLE agent_messages ADD COLUMN from_owner BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE agent_messages ADD COLUMN attachments UUID[] NOT NULL DEFAULT '{}';
ALTER TABLE agent_messages ADD COLUMN chat_seq BIGINT;
