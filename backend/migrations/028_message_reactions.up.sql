-- 028_message_reactions.up.sql
CREATE TABLE message_reactions (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    message_id uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users(id)    ON DELETE CASCADE,
    reaction   varchar(50) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_message_reactions UNIQUE (message_id, user_id, reaction)
);
