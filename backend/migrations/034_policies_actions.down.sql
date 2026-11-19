-- 034_policies_actions.down.sql
DROP INDEX idx_policies_actions;
ALTER TABLE policies
    DROP CONSTRAINT ck_policies_actions,
    DROP COLUMN actions;
DROP FUNCTION policy_actions_valid(text[]);
