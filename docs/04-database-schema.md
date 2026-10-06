# Database Schema
## Open Source Modular Application Platform v1.0

**Document:** 04-database-schema.md
**Status:** Baseline (Rev 1.1)
**Previous:** 03-database-erd.md
**Next:** 05-authentication-design.md
**Database:** PostgreSQL 18+

---

# 1. Purpose

03-database-erd.md defines *what* each table means. This document defines *exactly* what to run: `CREATE TABLE` statements, constraint syntax, trigger functions, database roles and grants, bootstrap rules, and migration numbering. Where the ERD left a decision open, this document closes it.

Rev 1.1 changes (see 00-errata-and-amendments.md): native `uuidv7()` default, composite foreign keys for tenant integrity, group hierarchy triggers, `roles.group_id`, `role_assignments` constraints, `sessions.token_hash`, new tables (`invitations`, `password_reset_tokens`, `outbox_events`), `direct_key`, three database roles, soft references on append-only tables.

---

# 2. Decisions the ERD Left Open

## 2.1 UUIDv7 generation

PostgreSQL 18 has a native `uuidv7()` function. The rule is:

- The Go application generates the UUIDv7 (`pkg/id`) and supplies it on `INSERT`: the id is needed *before* the insert so the audit record and outbox event of the same transaction can reference it.
- Every `id` column also has `DEFAULT uuidv7()` — a safety net for seed scripts, tests and manual SQL. No extension (`pgcrypto`) is needed.
- A UUIDv7 is an identifier, never a secret (it embeds a timestamp and has limited randomness). It is never used as a session token, reset token or invitation token — those are 32 random bytes (05 §7.1).

## 2.2 Email case-insensitivity

The application lowercases `email` before every write **and** the database enforces it: `CHECK (email = lower(email))`. `users.email` is a plain `UNIQUE` column. `citext` is not used.

## 2.3 Status fields: `CHECK`, not native `ENUM`

Every status/type column is `varchar(n)` with a `CHECK (... IN (...))` constraint. Adding a value is `ALTER TABLE ... DROP CONSTRAINT ... ADD CONSTRAINT ...` inside a normal migration; extending a native `ENUM` (`ALTER TYPE ... ADD VALUE`) is a recurring source of migration-tooling pain.

## 2.4 Tenant integrity through composite foreign keys

Invariants that a plain foreign key cannot express ("a group member belongs to the group's organization") are enforced with composite foreign keys against `UNIQUE (id, organization_id)` targets. See `groups`, `group_memberships`, `roles`, `invitations`, `conversations`, `conversation_members`.

Note on `MATCH SIMPLE` (the PostgreSQL default): a composite foreign key is **not checked when any of its columns is NULL**. Tables where a column may be NULL therefore carry a `CHECK` that forbids the half-NULL combination (see `roles`).

## 2.5 Append-only tables have soft references

`activities`, `security_events`, `system_logs` and `outbox_events` reference organizations/users/sessions by plain `uuid` columns **without foreign keys**. Audit evidence must outlive the rows it points at, and a cascading foreign-key action would mutate rows that are supposed to be immutable.

## 2.6 `module_installations` and migration numbering

03-database-erd.md §45 (first draft) omitted `module_installations`. The authoritative, numbered order is §14 below. Version numbers are strictly increasing because golang-migrate only applies versions greater than the current one; a tagged migration is never renumbered — new changes get new numbers.

---

# 3. Extensions and Common Functions

PostgreSQL 18 needs no extension for this schema.

```sql
-- 001_common_functions.up.sql
CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS trigger AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Attaches the updated_at trigger to a table. Called explicitly by every migration
-- that creates a table with an updated_at column (see §4).
CREATE OR REPLACE FUNCTION attach_updated_at(target regclass)
RETURNS void AS $$
DECLARE
    tname text := (SELECT relname FROM pg_class WHERE oid = target);
BEGIN
    EXECUTE format(
        'CREATE TRIGGER %I BEFORE UPDATE ON %s FOR EACH ROW EXECUTE FUNCTION set_updated_at()',
        'trg_' || tname || '_updated_at', target);
END;
$$ LANGUAGE plpgsql;
```

```sql
-- 001_common_functions.down.sql
DROP FUNCTION IF EXISTS attach_updated_at(regclass);
DROP FUNCTION IF EXISTS set_updated_at();
```

---

# 4. The `updated_at` Convention

Every table with an `updated_at` column calls, at the end of its own migration:

```sql
SELECT attach_updated_at('<table>');
```

It is written out in each migration below. "Assume it exists" is not acceptable: a **catalog-scanning test** (§17) fails the build if any table with an `updated_at` column lacks the trigger — this applies to module tables too (17, 18).

---

# 5. Platform & Organization

```sql
-- 002_platforms.up.sql
CREATE TABLE platforms (
    id               uuid PRIMARY KEY DEFAULT uuidv7(),
    name             varchar(200) NOT NULL,
    installation_key varchar(100) NOT NULL,
    version          varchar(50)  NOT NULL,
    status           varchar(30)  NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active', 'maintenance')),
    created_at       timestamptz  NOT NULL DEFAULT now(),
    updated_at       timestamptz  NOT NULL DEFAULT now(),

    CONSTRAINT uq_platforms_installation_key UNIQUE (installation_key)
);
SELECT attach_updated_at('platforms');
```

```sql
-- 003_organizations.up.sql
CREATE TABLE organizations (
    id                uuid PRIMARY KEY DEFAULT uuidv7(),
    name              varchar(200) NOT NULL,
    slug              varchar(100) NOT NULL,
    legal_name        varchar(255),
    status            varchar(30)  NOT NULL DEFAULT 'active'
                         CHECK (status IN ('active', 'suspended', 'archived')),
    default_locale    varchar(20)  NOT NULL DEFAULT 'en-US',
    default_timezone  varchar(100) NOT NULL DEFAULT 'UTC',
    created_at        timestamptz  NOT NULL DEFAULT now(),
    updated_at        timestamptz  NOT NULL DEFAULT now(),
    deleted_at        timestamptz,

    CONSTRAINT uq_organizations_slug UNIQUE (slug)
);

CREATE INDEX idx_organizations_status     ON organizations (status) WHERE deleted_at IS NULL;
CREATE INDEX idx_organizations_deleted_at ON organizations (deleted_at);
SELECT attach_updated_at('organizations');
```

---

# 6. Identity & Membership

```sql
-- 004_users.up.sql
CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    username      varchar(100),
    display_name  varchar(200) NOT NULL,
    first_name    varchar(100),
    last_name     varchar(100),
    email         varchar(320) NOT NULL,
    phone         varchar(50),
    avatar_url    text,
    locale        varchar(20)  NOT NULL DEFAULT 'en-US',
    timezone      varchar(100) NOT NULL DEFAULT 'UTC',
    status        varchar(30)  NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending', 'active', 'suspended', 'locked', 'deactivated', 'deleted')),
    last_login_at timestamptz,
    created_at    timestamptz  NOT NULL DEFAULT now(),
    updated_at    timestamptz  NOT NULL DEFAULT now(),
    deleted_at    timestamptz,

    CONSTRAINT uq_users_email      UNIQUE (email),
    CONSTRAINT uq_users_username   UNIQUE (username),
    CONSTRAINT ck_users_email_lower CHECK (email = lower(email))
);

CREATE INDEX idx_users_status ON users (status) WHERE deleted_at IS NULL;
SELECT attach_updated_at('users');
```

`status = 'locked'` is an *administrative* lock. The temporary lock after failed logins lives in Redis with a TTL (05 §12.3), not in this column.

```sql
-- 005_organization_memberships.up.sql
CREATE TABLE organization_memberships (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    user_id         uuid NOT NULL REFERENCES users(id)         ON DELETE RESTRICT,
    status          varchar(30) NOT NULL DEFAULT 'invited'
                       CHECK (status IN ('invited', 'active', 'suspended', 'removed')),
    joined_at       timestamptz,
    invited_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_org_memberships UNIQUE (organization_id, user_id)
);

CREATE INDEX idx_org_memberships_user       ON organization_memberships (user_id);
CREATE INDEX idx_org_memberships_org_status ON organization_memberships (organization_id, status);
SELECT attach_updated_at('organization_memberships');
```

`uq_org_memberships` is also the target of the composite foreign keys from `group_memberships` and `conversation_members`.

```sql
-- 006_groups.up.sql
CREATE TABLE groups (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE RESTRICT,
    parent_group_id uuid REFERENCES groups(id) ON DELETE RESTRICT,
    name            varchar(200) NOT NULL,
    slug            varchar(100) NOT NULL,
    description     text,
    status          varchar(30) NOT NULL DEFAULT 'active'
                       CHECK (status IN ('active', 'archived')),
    created_by      uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    deleted_at      timestamptz,

    CONSTRAINT uq_groups_org_slug UNIQUE (organization_id, slug),
    CONSTRAINT uq_groups_id_org   UNIQUE (id, organization_id)   -- target of composite FKs
);

CREATE INDEX idx_groups_organization ON groups (organization_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_groups_parent       ON groups (parent_group_id);
CREATE INDEX idx_groups_status       ON groups (status);
SELECT attach_updated_at('groups');

-- Hierarchy rules a plain FK cannot express:
--   * organization_id is immutable
--   * a parent belongs to the same organization
--   * no cycles, bounded depth (8 levels including the group itself)
CREATE OR REPLACE FUNCTION check_group_hierarchy()
RETURNS trigger AS $$
DECLARE
    max_depth constant int := 8;
    ancestors int;
    has_cycle boolean;
BEGIN
    IF TG_OP = 'UPDATE' AND NEW.organization_id IS DISTINCT FROM OLD.organization_id THEN
        RAISE EXCEPTION 'groups.organization_id is immutable';
    END IF;

    IF NEW.parent_group_id IS NOT NULL THEN
        IF NEW.parent_group_id = NEW.id THEN
            RAISE EXCEPTION 'a group cannot be its own parent';
        END IF;

        IF NOT EXISTS (
            SELECT 1 FROM groups
            WHERE id = NEW.parent_group_id AND organization_id = NEW.organization_id
        ) THEN
            RAISE EXCEPTION 'parent_group_id must belong to the same organization_id';
        END IF;

        -- walk up the ancestor chain (bounded, so a pre-existing bad chain cannot loop forever)
        WITH RECURSIVE anc(id, parent_group_id, depth) AS (
            SELECT g.id, g.parent_group_id, 1 FROM groups g WHERE g.id = NEW.parent_group_id
            UNION ALL
            SELECT g.id, g.parent_group_id, a.depth + 1
            FROM groups g JOIN anc a ON g.id = a.parent_group_id
            WHERE a.depth < 64
        )
        SELECT count(*), COALESCE(bool_or(id = NEW.id), false)
          INTO ancestors, has_cycle
          FROM anc;

        IF has_cycle THEN
            RAISE EXCEPTION 'group hierarchy cycle detected';
        END IF;

        IF ancestors + 1 > max_depth THEN
            RAISE EXCEPTION 'group hierarchy deeper than % levels', max_depth;
        END IF;
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_groups_hierarchy
    BEFORE INSERT OR UPDATE OF parent_group_id, organization_id ON groups
    FOR EACH ROW EXECUTE FUNCTION check_group_hierarchy();
```

Moving a group that already has descendants can push a descendant past the depth limit; that case is checked by the application's "move group" use case (the trigger only sees the row being written).

```sql
-- 007_group_memberships.up.sql
CREATE TABLE group_memberships (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    group_id        uuid NOT NULL,
    organization_id uuid NOT NULL,
    user_id         uuid NOT NULL,
    status          varchar(30) NOT NULL DEFAULT 'invited'
                       CHECK (status IN ('invited', 'active', 'suspended', 'removed')),
    joined_at       timestamptz,
    added_by        uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_group_memberships UNIQUE (group_id, user_id),
    -- the group really belongs to this organization
    CONSTRAINT fk_group_memberships_group
        FOREIGN KEY (group_id, organization_id) REFERENCES groups (id, organization_id) ON DELETE CASCADE,
    -- the user really has a membership in this organization (domain invariant 2)
    CONSTRAINT fk_group_memberships_org_member
        FOREIGN KEY (organization_id, user_id) REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT
);

CREATE INDEX idx_group_memberships_user         ON group_memberships (user_id);
CREATE INDEX idx_group_memberships_group_status ON group_memberships (group_id, status);
SELECT attach_updated_at('group_memberships');
```

---

# 7. Modules

```sql
-- 008_modules.up.sql
CREATE TABLE modules (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    code         varchar(100) NOT NULL,
    name         varchar(200) NOT NULL,
    version      varchar(50)  NOT NULL,
    description  text,
    module_type  varchar(50)  NOT NULL DEFAULT 'business'
                    CHECK (module_type IN ('core', 'business')),
    status       varchar(30)  NOT NULL DEFAULT 'registered'
                    CHECK (status IN ('registered', 'installed', 'enabled', 'disabled', 'failed', 'uninstalled')),
    manifest     jsonb NOT NULL,
    installed_at timestamptz,
    enabled_at   timestamptz,
    disabled_at  timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_modules_code UNIQUE (code),
    CONSTRAINT ck_modules_code_format CHECK (code ~ '^[a-z][a-z0-9_]{1,49}$')
);
SELECT attach_updated_at('modules');
```

`code` is restricted to lowercase letters, digits and underscore because it is used as a **table-name prefix** (07 §6) and as the name of the module's migration version table (§15.2).

```sql
-- 009_module_dependencies.up.sql
CREATE TABLE module_dependencies (
    id                   uuid PRIMARY KEY DEFAULT uuidv7(),
    module_id            uuid NOT NULL REFERENCES modules(id) ON DELETE CASCADE,
    dependency_module_id uuid NOT NULL REFERENCES modules(id) ON DELETE RESTRICT,
    version_constraint   varchar(100) NOT NULL,
    created_at           timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_module_dependencies      UNIQUE (module_id, dependency_module_id),
    CONSTRAINT ck_module_dependencies_self CHECK (module_id <> dependency_module_id)
);
```

```sql
-- 010_module_installations.up.sql
CREATE TABLE module_installations (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    module_id       uuid NOT NULL REFERENCES modules(id)       ON DELETE RESTRICT,
    version         varchar(50) NOT NULL,
    status          varchar(30) NOT NULL DEFAULT 'installed'
                       CHECK (status IN ('installed', 'enabled', 'disabled', 'failed')),
    configuration   jsonb NOT NULL DEFAULT '{}',
    row_version     bigint NOT NULL DEFAULT 1,    -- optimistic locking (03 §48)
    installed_by    uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    installed_at    timestamptz NOT NULL DEFAULT now(),
    enabled_at      timestamptz,
    disabled_at     timestamptz,
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_module_installations UNIQUE (organization_id, module_id)
);
SELECT attach_updated_at('module_installations');
```

---

# 8. Authorization

```sql
-- 011_permissions.up.sql
CREATE TABLE permissions (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    module_id   uuid REFERENCES modules(id) ON DELETE RESTRICT,
    resource    varchar(150) NOT NULL,
    action      varchar(100) NOT NULL,
    code        varchar(255) NOT NULL,
    description text,
    is_system   boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_permissions_code UNIQUE (code)
);

CREATE INDEX idx_permissions_module ON permissions (module_id);
```

```sql
-- 012_roles.up.sql
CREATE TABLE roles (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid REFERENCES organizations(id) ON DELETE CASCADE,
    group_id        uuid,
    name            varchar(150) NOT NULL,
    slug            varchar(100) NOT NULL,
    description     text,
    role_type       varchar(30) NOT NULL
                       CHECK (role_type IN ('system', 'organization', 'group', 'custom')),
    status          varchar(30) NOT NULL DEFAULT 'active'
                       CHECK (status IN ('active', 'archived')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    -- a group-owned role must belong to a group of the same organization
    CONSTRAINT fk_roles_group
        FOREIGN KEY (group_id, organization_id) REFERENCES groups (id, organization_id) ON DELETE RESTRICT,

    -- the boundary matches the columns (also blocks the half-NULL case that MATCH SIMPLE would skip)
    CONSTRAINT ck_roles_boundary CHECK (
           (role_type = 'system'       AND organization_id IS NULL     AND group_id IS NULL)
        OR (role_type = 'organization' AND organization_id IS NOT NULL AND group_id IS NULL)
        OR (role_type = 'group'        AND organization_id IS NOT NULL AND group_id IS NOT NULL)
        OR (role_type = 'custom'       AND organization_id IS NOT NULL)
    )
);

-- Slug uniqueness per boundary. A composite UNIQUE would not work: NULL <> NULL.
CREATE UNIQUE INDEX uq_roles_global_slug ON roles (slug)
    WHERE organization_id IS NULL;
CREATE UNIQUE INDEX uq_roles_org_slug    ON roles (organization_id, slug)
    WHERE organization_id IS NOT NULL AND group_id IS NULL;
CREATE UNIQUE INDEX uq_roles_group_slug  ON roles (group_id, slug)
    WHERE group_id IS NOT NULL;

CREATE INDEX idx_roles_organization ON roles (organization_id);
SELECT attach_updated_at('roles');
```

```sql
-- 013_role_permissions.up.sql
CREATE TABLE role_permissions (
    id            uuid PRIMARY KEY DEFAULT uuidv7(),
    role_id       uuid NOT NULL REFERENCES roles(id)       ON DELETE CASCADE,
    permission_id uuid NOT NULL REFERENCES permissions(id) ON DELETE RESTRICT,
    created_by    uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_role_permissions UNIQUE (role_id, permission_id)
);
```

```sql
-- 014_role_assignments.up.sql
CREATE TABLE role_assignments (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid REFERENCES organizations(id) ON DELETE CASCADE,
    role_id         uuid NOT NULL REFERENCES roles(id) ON DELETE RESTRICT,
    subject_type    varchar(30) NOT NULL CHECK (subject_type IN ('user', 'group', 'organization', 'service')),
    subject_id      uuid NOT NULL, -- polymorphic; not an FK, see note below
    scope_type      varchar(30) NOT NULL CHECK (scope_type IN ('platform', 'organization', 'group', 'resource', 'own')),
    scope_id        uuid,
    assigned_by     uuid REFERENCES users(id) ON DELETE SET NULL,
    expires_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    revoked_at      timestamptz,

    -- domain invariant 3: an assignment cannot reference an invalid scope
    CONSTRAINT ck_role_assignments_scope CHECK (
           (scope_type IN ('platform', 'own') AND scope_id IS NULL)
        OR  scope_type = 'organization'
        OR (scope_type IN ('group', 'resource') AND scope_id IS NOT NULL)
    ),
    -- platform-scoped assignments have no organization; every other scope has one
    CONSTRAINT ck_role_assignments_org CHECK ((scope_type = 'platform') = (organization_id IS NULL))
);

CREATE INDEX idx_role_assignments_subject ON role_assignments (subject_type, subject_id);
CREATE INDEX idx_role_assignments_org     ON role_assignments (organization_id);
CREATE INDEX idx_role_assignments_scope   ON role_assignments (scope_type, scope_id);
CREATE INDEX idx_role_assignments_active  ON role_assignments (subject_id) WHERE revoked_at IS NULL;

-- at most one active assignment per (role, subject, scope)
CREATE UNIQUE INDEX uq_role_assignments_active ON role_assignments
    (role_id, subject_type, subject_id, scope_type,
     COALESCE(scope_id, '00000000-0000-0000-0000-000000000000'::uuid))
    WHERE revoked_at IS NULL;
```

`subject_id` is intentionally not a foreign key — it can point at `users`, `groups`, or `organizations` depending on `subject_type` (03 §15). Existence and same-organization are verified in the application's `role_assignment` service inside the granting transaction. This is the one deliberate exception to "the database enforces referential integrity", confined to this single column.

```sql
-- 015_policies.up.sql
CREATE TABLE policies (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid REFERENCES organizations(id) ON DELETE CASCADE,
    name            varchar(150) NOT NULL,
    code            varchar(150) NOT NULL,
    description     text,
    effect          varchar(20) NOT NULL CHECK (effect IN ('allow', 'deny')),
    expression      text NOT NULL,
    status          varchar(30) NOT NULL DEFAULT 'active'
                       CHECK (status IN ('active', 'disabled')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_policies_org_code UNIQUE (organization_id, code)
);

CREATE UNIQUE INDEX uq_policies_global_code ON policies (code) WHERE organization_id IS NULL;
SELECT attach_updated_at('policies');
```

---

# 9. Settings

```sql
-- 016_settings.up.sql
CREATE TABLE settings (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    scope_type  varchar(30) NOT NULL CHECK (scope_type IN ('platform', 'organization', 'group', 'user', 'module')),
    scope_id    uuid NOT NULL,
    module_id   uuid REFERENCES modules(id) ON DELETE CASCADE,
    key         varchar(200) NOT NULL,
    value       jsonb NOT NULL,
    value_type  varchar(30) NOT NULL,
    is_secret   boolean NOT NULL DEFAULT false,
    row_version bigint NOT NULL DEFAULT 1,    -- optimistic locking (03 §48)
    created_by  uuid REFERENCES users(id) ON DELETE SET NULL,
    updated_by  uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- A single UNIQUE(scope_type, scope_id, module_id, key) would NOT catch two
-- non-module settings with the same key, because module_id IS NULL and
-- NULL <> NULL. Two partial indexes cover both cases:
CREATE UNIQUE INDEX uq_settings_key_with_module ON settings (scope_type, scope_id, module_id, key)
    WHERE module_id IS NOT NULL;
CREATE UNIQUE INDEX uq_settings_key_no_module ON settings (scope_type, scope_id, key)
    WHERE module_id IS NULL;
SELECT attach_updated_at('settings');
```

`scope_id` for `platform` scope is the `platforms.id` row. Secret values (`is_secret = true`) are stored encrypted by the application; they are never returned by read endpoints (07 §10).

---

# 10. Authentication

```sql
-- 017_credentials.up.sql
CREATE TABLE credentials (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_type varchar(30) NOT NULL CHECK (credential_type IN ('password', 'totp', 'recovery_code', 'webauthn')),
    secret_hash     text,  -- password / recovery code: PHC-format hash. TOTP: encrypted secret (not a hash)
    metadata        jsonb NOT NULL DEFAULT '{}',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    last_used_at    timestamptz,
    revoked_at      timestamptz
);

CREATE INDEX idx_credentials_user_type ON credentials (user_id, credential_type) WHERE revoked_at IS NULL;
-- one active password per user
CREATE UNIQUE INDEX uq_credentials_active_password ON credentials (user_id)
    WHERE credential_type = 'password' AND revoked_at IS NULL;
SELECT attach_updated_at('credentials');
```

```sql
-- 018_sessions.up.sql
CREATE TABLE sessions (
    id               uuid PRIMARY KEY DEFAULT uuidv7(),       -- internal id, never a credential
    token_hash       bytea NOT NULL,                          -- SHA-256 of the opaque cookie token
    user_id          uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    organization_id  uuid REFERENCES organizations(id) ON DELETE CASCADE,
    created_at       timestamptz NOT NULL DEFAULT now(),
    last_activity_at timestamptz NOT NULL DEFAULT now(),
    expires_at       timestamptz NOT NULL,
    ip_address       inet,
    user_agent       text,
    auth_level       varchar(30) NOT NULL DEFAULT 'password'
                        CHECK (auth_level IN ('password', 'mfa')),
    revoked_at       timestamptz,

    CONSTRAINT uq_sessions_token_hash UNIQUE (token_hash)
);

CREATE INDEX idx_sessions_user_active ON sessions (user_id) WHERE revoked_at IS NULL;
```

The live session lookup goes through Redis keyed by the token hash (05 §7). This table is the durable record (history, the "Active Sessions" UI, revoke-all-others) **and the fallback lookup when Redis is unavailable**.

```sql
-- 019_security_events.up.sql
CREATE TABLE security_events (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid,   -- soft reference (§2.5)
    user_id         uuid,   -- soft reference
    session_id      uuid,   -- soft reference
    event_type      varchar(100) NOT NULL,
    result          varchar(30) NOT NULL CHECK (result IN ('success', 'failure')),
    ip_address      inet,
    user_agent      text,
    request_id      varchar(100),
    metadata        jsonb NOT NULL DEFAULT '{}',
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_security_events_user_created ON security_events (user_id, created_at DESC);
CREATE INDEX idx_security_events_org_created  ON security_events (organization_id, created_at DESC);
```

```sql
-- 020_invitations.up.sql
CREATE TABLE invitations (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    email           varchar(320) NOT NULL,
    invited_by      uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    group_id        uuid,
    role_id         uuid REFERENCES roles(id) ON DELETE RESTRICT,
    token_hash      bytea NOT NULL,
    status          varchar(30) NOT NULL DEFAULT 'pending'
                       CHECK (status IN ('pending', 'accepted', 'revoked', 'expired')),
    expires_at      timestamptz NOT NULL,
    accepted_at     timestamptz,
    revoked_at      timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_invitations_token_hash UNIQUE (token_hash),
    CONSTRAINT ck_invitations_email_lower CHECK (email = lower(email)),
    CONSTRAINT fk_invitations_group
        FOREIGN KEY (group_id, organization_id) REFERENCES groups (id, organization_id) ON DELETE CASCADE
);

CREATE UNIQUE INDEX uq_invitations_pending ON invitations (organization_id, email)
    WHERE status = 'pending';
CREATE INDEX idx_invitations_org_status ON invitations (organization_id, status);
SELECT attach_updated_at('invitations');
```

```sql
-- 021_password_reset_tokens.up.sql
CREATE TABLE password_reset_tokens (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    user_id     uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash  bytea NOT NULL,
    expires_at  timestamptz NOT NULL,
    used_at     timestamptz,
    ip_address  inet,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_password_reset_token_hash UNIQUE (token_hash)
);

CREATE INDEX idx_password_reset_user_open ON password_reset_tokens (user_id) WHERE used_at IS NULL;
```

---

# 11. Communication

```sql
-- 022_notifications.up.sql
CREATE TABLE notifications (
    id                uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id   uuid REFERENCES organizations(id) ON DELETE CASCADE,
    recipient_user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type              varchar(100) NOT NULL,
    title             varchar(255),   -- English fallback only (08 §8)
    body              text,           -- English fallback only
    data              jsonb NOT NULL DEFAULT '{}',   -- i18n parameters
    priority          varchar(20) NOT NULL DEFAULT 'normal'
                         CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    status            varchar(30) NOT NULL DEFAULT 'unread'
                         CHECK (status IN ('unread', 'read', 'archived', 'expired')),
    created_at        timestamptz NOT NULL DEFAULT now(),
    read_at           timestamptz,
    expires_at        timestamptz
);

CREATE INDEX idx_notifications_recipient_status ON notifications (recipient_user_id, status);
CREATE INDEX idx_notifications_created          ON notifications (created_at DESC);
```

```sql
-- 023_inbox_items.up.sql
CREATE TABLE inbox_items (
    id                uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id   uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    recipient_user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    type              varchar(100) NOT NULL,
    title             varchar(255),
    description       text,
    data              jsonb NOT NULL DEFAULT '{}',
    resource_type     varchar(150),
    resource_id       uuid,
    action_url        text,
    status            varchar(30) NOT NULL DEFAULT 'pending'
                         CHECK (status IN ('pending', 'in_progress', 'completed', 'dismissed', 'expired')),
    priority          varchar(20) NOT NULL DEFAULT 'normal'
                         CHECK (priority IN ('low', 'normal', 'high', 'urgent')),
    created_at        timestamptz NOT NULL DEFAULT now(),
    completed_at      timestamptz,
    expires_at        timestamptz
);

CREATE INDEX idx_inbox_items_recipient_status ON inbox_items (recipient_user_id, status);
CREATE INDEX idx_inbox_items_resource         ON inbox_items (resource_type, resource_id);
```

```sql
-- 024_conversations.up.sql
CREATE TABLE conversations (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    type            varchar(20) NOT NULL CHECK (type IN ('direct', 'group')),
    title           varchar(255),
    direct_key      text,   -- least(user_a,user_b) || ':' || greatest(...), only for direct
    created_by      uuid NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    status          varchar(30) NOT NULL DEFAULT 'active'
                       CHECK (status IN ('active', 'archived')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    archived_at     timestamptz,

    CONSTRAINT uq_conversations_id_org UNIQUE (id, organization_id),
    CONSTRAINT ck_conversations_direct_key CHECK ((type = 'direct') = (direct_key IS NOT NULL))
);

-- one direct conversation per pair, even under concurrent creation
CREATE UNIQUE INDEX uq_conversations_direct ON conversations (organization_id, direct_key)
    WHERE type = 'direct';
CREATE INDEX idx_conversations_org ON conversations (organization_id, status);
SELECT attach_updated_at('conversations');
```

```sql
-- 025_conversation_members.up.sql
CREATE TABLE conversation_members (
    id                   uuid PRIMARY KEY DEFAULT uuidv7(),
    conversation_id      uuid NOT NULL,
    organization_id      uuid NOT NULL,
    user_id              uuid NOT NULL,
    member_role          varchar(30) NOT NULL DEFAULT 'member'
                            CHECK (member_role IN ('member', 'moderator', 'owner')),
    status               varchar(30) NOT NULL DEFAULT 'active'
                            CHECK (status IN ('active', 'left')),
    joined_at            timestamptz NOT NULL DEFAULT now(),
    left_at              timestamptz,
    last_read_message_id uuid, -- FK added in 026, after `messages` exists
    muted_until          timestamptz,
    created_at           timestamptz NOT NULL DEFAULT now(),
    updated_at           timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_conversation_members UNIQUE (conversation_id, user_id),
    -- domain invariant 5: a member belongs to the conversation's organization
    CONSTRAINT fk_conversation_members_conversation
        FOREIGN KEY (conversation_id, organization_id) REFERENCES conversations (id, organization_id) ON DELETE CASCADE,
    CONSTRAINT fk_conversation_members_org_member
        FOREIGN KEY (organization_id, user_id) REFERENCES organization_memberships (organization_id, user_id) ON DELETE RESTRICT
);

CREATE INDEX idx_conversation_members_user ON conversation_members (user_id, status);
SELECT attach_updated_at('conversation_members');
```

```sql
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
```

"Sender is an active conversation member" (domain invariant 6) depends on changing state, so it is checked by the application on every write, not by a constraint.

```sql
-- 027_message_reads.up.sql
CREATE TABLE message_reads (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    message_id uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users(id)    ON DELETE CASCADE,
    read_at    timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_message_reads UNIQUE (message_id, user_id)
);
```

```sql
-- 028_message_reactions.up.sql
CREATE TABLE message_reactions (
    id         uuid PRIMARY KEY DEFAULT uuidv7(),
    message_id uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    user_id    uuid NOT NULL REFERENCES users(id)    ON DELETE CASCADE,
    reaction   varchar(50) NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT uq_message_reactions UNIQUE (message_id, user_id, reaction)
);
```

```sql
-- 029_attachments.up.sql
CREATE TABLE attachments (
    id               uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id  uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    owner_user_id    uuid NOT NULL REFERENCES users(id)         ON DELETE RESTRICT,
    message_id       uuid REFERENCES messages(id) ON DELETE SET NULL,
    storage_provider varchar(50) NOT NULL,
    storage_key      text NOT NULL,
    original_name    varchar(255) NOT NULL,
    content_type     varchar(150) NOT NULL,
    size_bytes       bigint NOT NULL CHECK (size_bytes >= 0),
    checksum         varchar(128),
    status           varchar(30) NOT NULL DEFAULT 'active'
                        CHECK (status IN ('active', 'quarantined', 'deleted')),
    created_at       timestamptz NOT NULL DEFAULT now(),
    deleted_at       timestamptz
);

CREATE INDEX idx_attachments_message ON attachments (message_id);
CREATE INDEX idx_attachments_org     ON attachments (organization_id, created_at DESC);
```

---

# 12. Audit & Operations

```sql
-- 030_activities.up.sql
CREATE TABLE activities (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid,   -- soft reference (§2.5)
    actor_user_id   uuid,   -- soft reference
    actor_type      varchar(30) NOT NULL DEFAULT 'user'
                       CHECK (actor_type IN ('user', 'service', 'system')),
    action          varchar(150) NOT NULL,
    resource_type   varchar(150),
    resource_id     uuid,
    scope_type      varchar(30),
    scope_id        uuid,
    result          varchar(30) NOT NULL CHECK (result IN ('success', 'denied', 'failed')),
    ip_address      inet,
    user_agent      text,
    request_id      varchar(100),
    trace_id        varchar(100),
    metadata        jsonb NOT NULL DEFAULT '{}',
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_activities_org_created   ON activities (organization_id, created_at DESC, id DESC);
CREATE INDEX idx_activities_actor_created ON activities (actor_user_id, created_at DESC, id DESC);
CREATE INDEX idx_activities_scope_created ON activities (scope_type, scope_id, created_at DESC);
CREATE INDEX idx_activities_resource      ON activities (resource_type, resource_id);
CREATE INDEX idx_activities_action        ON activities (action);
```

```sql
-- 031_system_logs.up.sql
CREATE TABLE system_logs (
    id          uuid PRIMARY KEY DEFAULT uuidv7(),
    service     varchar(100) NOT NULL,
    environment varchar(50)  NOT NULL,
    level       varchar(20)  NOT NULL CHECK (level IN ('debug', 'info', 'warn', 'error', 'fatal')),
    event_code  varchar(150) NOT NULL,
    message     text NOT NULL,
    request_id  varchar(100),
    trace_id    varchar(100),
    metadata    jsonb NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_system_logs_created         ON system_logs (created_at DESC);
CREATE INDEX idx_system_logs_level_created   ON system_logs (level, created_at DESC);
CREATE INDEX idx_system_logs_service_created ON system_logs (service, created_at DESC);
```

```sql
-- 032_outbox_events.up.sql   (transactional outbox, 10 §6)
CREATE TABLE outbox_events (
    id              uuid PRIMARY KEY DEFAULT uuidv7(),
    organization_id uuid,   -- soft reference
    event_name      varchar(200) NOT NULL,
    aggregate_type  varchar(100),
    aggregate_id    uuid,
    payload         jsonb NOT NULL DEFAULT '{}',
    request_id      varchar(100),
    trace_id        varchar(100),
    status          varchar(20) NOT NULL DEFAULT 'pending'
                       CHECK (status IN ('pending', 'processed', 'dead')),
    attempts        integer NOT NULL DEFAULT 0,
    available_at    timestamptz NOT NULL DEFAULT now(),
    last_error      text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    processed_at    timestamptz
);

-- the relay reads: WHERE status = 'pending' AND available_at <= now()
--                  ORDER BY available_at, created_at LIMIT n FOR UPDATE SKIP LOCKED
CREATE INDEX idx_outbox_pending ON outbox_events (available_at, created_at) WHERE status = 'pending';
CREATE INDEX idx_outbox_processed ON outbox_events (processed_at) WHERE status <> 'pending';
```

---

# 13. Database Roles & Grants

## 13.1 Three roles, created by infrastructure — not by a migration

A migration cannot reliably create roles: `CREATE ROLE` needs `CREATEROLE` and roles are cluster-level objects. Roles are created by the infrastructure init script (`deploy/db/init/00-roles.sql` for Docker; the operator's provisioning for production). Passwords come from the secret manager — never from a committed file (the dev values in the compose file are for local use only).

| Role | Used by | Rights |
|---|---|---|
| `app_migrator` | `cmd/migrate` and the module lifecycle (install/uninstall, 07 §3) | Owns the schema and every object; DDL |
| `app_user` | API and worker at runtime | DML on everything; **no UPDATE/DELETE** on `activities`, `security_events`, `system_logs` |
| `app_maintenance` | Only the worker jobs for retention and `user.anonymize` (13 §6–7) | `SELECT`/`DELETE` on the log tables and processed outbox rows; `UPDATE` on a small, named column list |

```sql
-- deploy/db/init/00-roles.sql  (run once by the infrastructure, as a superuser)
CREATE ROLE app_migrator    WITH LOGIN PASSWORD :'migrator_password';
CREATE ROLE app_user        WITH LOGIN PASSWORD :'app_password';
CREATE ROLE app_maintenance WITH LOGIN PASSWORD :'maintenance_password';

ALTER DATABASE platform OWNER TO app_migrator;
ALTER SCHEMA public OWNER TO app_migrator;
REVOKE ALL ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO app_user, app_maintenance;
```

## 13.2 Grants migration

Runs as `app_migrator` (so the default privileges below apply to every table that role creates later — including module tables).

```sql
-- 033_grants.up.sql
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO app_user;

-- tables created later by app_migrator (module tables) get the same grants automatically
ALTER DEFAULT PRIVILEGES IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO app_user;

-- append-only: the runtime role can create and read, never modify or erase
REVOKE UPDATE, DELETE ON activities, security_events, system_logs FROM app_user;

-- maintenance role: the minimum needed for retention and anonymization
GRANT SELECT, DELETE ON activities, security_events, system_logs, outbox_events TO app_maintenance;
GRANT UPDATE (actor_user_id, metadata)                            ON activities      TO app_maintenance;
GRANT UPDATE (user_id, ip_address, user_agent, metadata)          ON security_events TO app_maintenance;

-- ADR-0006: golang-migrate's version table is not application data. The runtime role may read it
-- (schema version check) but never write it. Guarded: the table exists once golang-migrate has opened it.
DO $$
BEGIN
    IF to_regclass('public.schema_migrations') IS NOT NULL THEN
        REVOKE INSERT, UPDATE, DELETE ON public.schema_migrations FROM app_user;
    END IF;
END
$$;
```

The last block (ADR-0006) exists because golang-migrate creates its version table (`schema_migrations`) *before* migration 001, so `GRANT … ON ALL TABLES` would otherwise hand the runtime role write access to the schema version. `app_user` may read it, never write it.

A bug that somehow tried to `UPDATE activities` as `app_user` fails in the database, not just in code review. The only sanctioned mutation — anonymization — is a separate code path on a separate connection pool (`MAINTENANCE_DATABASE_URL`).

Because module tables are created by `app_migrator`, the default privileges give `app_user` DML on them without further work. A module that wants an append-only table adds its own `REVOKE` in its migration (17 §5). The same applies to the module's version table `schema_migrations_<module_code>` (§15.2): it is created after 033, so it receives the default DML grant — the module migration revokes `INSERT, UPDATE, DELETE` on it from `app_user` (ADR-0006; enforced when the module system lands, M07).

---

# 14. Migration Order

Supersedes 03-database-erd.md's first-draft §45:

```text
001_common_functions
002_platforms
003_organizations
004_users
005_organization_memberships
006_groups
007_group_memberships
008_modules
009_module_dependencies
010_module_installations
011_permissions
012_roles
013_role_permissions
014_role_assignments
015_policies
016_settings
017_credentials
018_sessions
019_security_events
020_invitations
021_password_reset_tokens
022_notifications
023_inbox_items
024_conversations
025_conversation_members
026_messages
027_message_reads
028_message_reactions
029_attachments
030_activities
031_system_logs
032_outbox_events
033_grants
```

Each numbered step has a paired `.up.sql` / `.down.sql`. A module's own tables (e.g. `announcements_posts`) are **never** part of core's sequence: they use the module's own migration sequence and version table (§15.2), run when that module is installed.

---

# 15. Migration Tooling

**golang-migrate used as a library** by `cmd/migrate` (the same binary runs locally, in CI and in Docker), with the pgx v5 driver. The connection URL scheme for that driver is `pgx5://…`. Plain `.sql` files (embedded into the binary with `go:embed`, `backend/migrations/embed.go`), `up`/`down N`/`version`/`roundtrip`/`seed` subcommands, no ORM DSL — consistent with "the database is the source of truth".

```text
backend/migrations/
├── 001_common_functions.up.sql
├── 001_common_functions.down.sql
├── ...
└── 033_grants.down.sql
```

`cmd/migrate` runs with `MIGRATION_DATABASE_URL` (role `app_migrator`). The API and worker never hold DDL rights.

## 15.1 Rollback strategy

Every `.down.sql` reverses its `.up.sql` in opposite dependency order — e.g. `010_module_installations.down.sql` is `DROP TABLE module_installations;`, and a migration that added a constraint via `ALTER TABLE` (like `026`'s `last_read_message_id` wiring) drops that constraint before the referenced table is dropped further down the chain. `033_grants.down.sql` revokes what it granted. The CI job `make migrate-roundtrip` (up → down → up) runs against a throwaway database inside a throwaway PostgreSQL container (ADR-0005) on every migration change (19 §5), and fails if any table or function is left in `public` after the full down (the version table excepted). `down` at version 0 is a successful no-op.

## 15.2 Module migrations

Each module has its **own migration sequence, starting at 001**, and its **own version table** `schema_migrations_<module_code>`, so module numbers never collide with core's or with another module's. The core module lifecycle runs them through the `MigrationRunner` handed to `Module.Migrate` (07 §5) using `app_migrator`. The down migrations run on uninstall only after the Super Admin confirms the data-handling choice (07 §3).

---

# 16. Seed, System Roles, and Bootstrap

**No permissions and no roles are seeded by raw SQL.** Permissions and system roles are declared in code and synchronized idempotently at startup (06 §5.4); SQL seeds drift from code. What exists at install time:

1. **Platform row** — inserted by the Go seeder, idempotent on `installation_key`:

```sql
INSERT INTO platforms (id, name, installation_key, version, status)
VALUES ($1, 'Local Installation', $2, $3, 'active')
ON CONFLICT (installation_key) DO NOTHING;
```

`$1` (UUIDv7) and `$2` are generated once by the seeding tool and reused on re-runs.

2. **Permission catalog** — `internal/permission/catalog.go` declares every core permission (06 §5.4). On startup the catalog is upserted by `code` (`is_system = true`). The sync never deletes.

3. **System roles** — Super Admin, Organization Admin, Group Admin, Member (06 §5.5), upserted by `slug` with their default permission sets. Super Admin receives every `is_system` permission on each sync.

4. **First Super Admin** — `make bootstrap-admin EMAIL=you@example.org`: creates the user, the password credential, and a platform-scoped `role_assignment` for Super Admin, idempotently, in one transaction. The password is read interactively (or from stdin) or generated and printed once — never from a committed file.

---

# 17. Testing Requirements

```text
Constraints
  duplicate organizations.slug rejected
  users.email with uppercase letters rejected (ck_users_email_lower)
  roles: duplicate global slug rejected; duplicate org-level slug rejected; duplicate slug
    inside one group rejected; same slug allowed across two organizations
  roles: every boundary combination in ck_roles_boundary accepted, every other rejected
    (including a group_id with a NULL organization_id)
  role_assignments: invalid scope/scope_id combinations rejected; a second active identical
    assignment rejected; the same assignment after revocation accepted
  settings: same (scope, key) rejected whether or not module_id is set
  module_dependencies: a module cannot depend on itself
  modules.code rejects uppercase, dashes, and names that do not start with a letter
  conversations: a second direct conversation for the same pair in the same organization
    rejected; direct_key NULL for type=group and NOT NULL for type=direct enforced
  invitations: a second pending invitation for the same (organization, email) rejected

Tenant integrity (composite foreign keys)
  group_membership whose user has no organization_membership in the group's organization rejected
  group_membership with an organization_id different from the group's rejected
  conversation_member from another organization rejected
  a role with group_id in another organization rejected

Triggers
  updated_at changes on every UPDATE, unchanged on rows nobody touched
  EVERY table with an updated_at column has the updated_at trigger (catalog-scanning test)
  a group's parent in a different organization rejected
  a group cycle (A→B→A, and A→A) rejected
  a hierarchy deeper than 8 levels rejected
  groups.organization_id cannot be changed

Cascades
  deleting an organization cascades to its roles, role_assignments, module_installations,
    conversations, invitations — never to the `users` rows themselves (ON DELETE RESTRICT on
    users everywhere); it is BLOCKED (RESTRICT) while groups or organization_memberships still
    exist (soft delete is the normal path, 02 §36); activities/security_events/outbox rows are unaffected
  deleting a group cascades to group_memberships and to invitations bound to it, and is blocked
    (RESTRICT) while roles.group_id or a child group still reference it; role_assignments with
    scope_type='group' are NOT blocked by the database (polymorphic scope_id) — the application
    revokes them (ADR-0008)

Grants
  app_user cannot UPDATE or DELETE a row in activities, security_events, or system_logs
    (expect a permission-denied error from PostgreSQL itself)
  app_user CAN INSERT and SELECT them
  a table created afterwards by app_migrator is automatically usable by app_user
  app_maintenance can DELETE old log rows and UPDATE only the granted columns

Migrations
  every .down.sql successfully reverses its .up.sql on a throwaway database
    (make migrate-roundtrip: up → down → up; nothing left in `public` after the full down)
  app_user can read but not write the golang-migrate version table (schema_migrations)
  docs/erd.md is generated from the migrated schema and must match it (UPDATE_ERD=1 make db-test)
  the bootstrap/seed path is idempotent — running it twice produces no duplicate rows
```
