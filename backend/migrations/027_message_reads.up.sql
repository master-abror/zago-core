-- 027_message_reads.up.sql
CREATE TABLE message_reads (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    message_id uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users(id)    ON DELETE CASCADE,
    read_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_message_reads UNIQUE (message_id, user_id)
);
