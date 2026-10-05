DROP TABLE IF EXISTS agent_messages;
DROP TABLE IF EXISTS services;
DROP TABLE IF EXISTS architectures;

DELETE FROM chat_events WHERE kind = 'message';
ALTER TABLE chat_events DROP CONSTRAINT chat_events_kind_check;
ALTER TABLE chat_events ADD CONSTRAINT chat_events_kind_check
    CHECK (kind IN ('user', 'assistant', 'tool', 'activity', 'agent', 'error'));

ALTER TABLE instances DROP COLUMN IF EXISTS stack;
