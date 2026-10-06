-- 026_messages.up.sql
CREATE TABLE messages (
    id                  uuid PRIMARY KEY DEFAULT uuidv7(),
    conversation_id     uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    sender_user_id      uuid NOT NULL REFERENCES users(id)         ON DELETE RESTRICT,
    message_type        varchar(30) NOT NULL DEFAULT 'text'
                           CHECK (message_type IN ('text', 'system', 'file', 'image', 'event')),
    body                text,
    metadata            jsonb NOT NULL DEFAULT '{}',
    reply_to_message_id uuid REFERENCES messages(id) ON DELETE SET NULL,
    created_at          timestamptz NOT NULL DEFAULT now(),
    updated_at          timestamptz NOT NULL DEFAULT now(),
    deleted_at          timestamptz
);

CREATE INDEX idx_messages_conversation_created ON messages (conversation_id, created_at DESC, id DESC);
CREATE INDEX idx_messages_sender               ON messages (sender_user_id);
SELECT attach_updated_at('messages');

ALTER TABLE conversation_members
    ADD CONSTRAINT fk_conversation_members_last_read
    FOREIGN KEY (last_read_message_id) REFERENCES messages(id) ON DELETE SET NULL;
