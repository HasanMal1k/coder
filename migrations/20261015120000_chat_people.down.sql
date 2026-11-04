ALTER TABLE agent_messages DROP COLUMN IF EXISTS chat_seq;
ALTER TABLE agent_messages DROP COLUMN IF EXISTS attachments;
ALTER TABLE agent_messages DROP COLUMN IF EXISTS from_owner;

DROP TABLE IF EXISTS attachments;

DELETE FROM chat_events WHERE kind = 'post';
ALTER TABLE chat_events DROP CONSTRAINT chat_events_kind_check;
ALTER TABLE chat_events ADD CONSTRAINT chat_events_kind_check
    CHECK (kind IN ('user', 'assistant', 'tool', 'activity', 'agent', 'message', 'error'));

DROP INDEX IF EXISTS agents_owner_handle;
ALTER TABLE agents DROP COLUMN IF EXISTS handle;
