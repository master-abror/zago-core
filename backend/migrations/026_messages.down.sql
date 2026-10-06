-- 026_messages.down.sql
-- FK last_read dilepas dulu: conversation_members (025) masih ada dan merujuk messages.
ALTER TABLE conversation_members DROP CONSTRAINT fk_conversation_members_last_read;
DROP TABLE messages;
