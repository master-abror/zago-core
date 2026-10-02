# PostgreSQL ERD & Database Schema Design
## Open Source Modular Application Platform v1.0

**Document:** 03-database-erd.md  
**Status:** Baseline (Rev 1.1 — amendments from 00-errata-and-amendments.md applied)  
**Previous:** 02-domain-model.md  
**Next:** 04-database-schema.md  
**Database:** PostgreSQL 18+  
**ID strategy:** UUIDv7  
**Time strategy:** UTC

---

# 1. Purpose

This document translates the domain model into a relational PostgreSQL design.

It is the primary reference for:

- PostgreSQL migrations
- Go repositories
- authorization queries
- transaction boundaries
- indexes
- data retention
- tenant isolation
- API persistence

The schema favors explicit relationships and strong database constraints over implicit application assumptions.

---

# 2. Database Principles

## 2.1 PostgreSQL is the source of truth

PostgreSQL owns authoritative application state.

Redis is only used for temporary/distributed state.

```text
PostgreSQL
    ↓
Authoritative state

Redis
    ↓
Cache / Session / Realtime / Temporary state
```

Redis data must always be reconstructible or safely invalidated.

---

# 3. Naming Conventions

Tables use plural snake_case.

Examples:

```text
users
organizations
groups
group_memberships
roles
permissions
role_permissions
```

Columns use snake_case.

Primary key:

```text
id uuid
```

Foreign key:

```text
organization_id uuid
user_id uuid
```

Timestamps:

```text
created_at timestamptz
updated_at timestamptz
deleted_at timestamptz
```

All timestamps are stored as UTC.

---

# 4. UUIDv7

All new primary keys should use UUIDv7.

Reasons:

- globally unique
- sortable by creation time
- better index locality than random UUIDv4
- suitable for distributed systems
- safe across multiple application instances

The Go application generates UUIDv7 identifiers (`pkg/id`), because an identifier is needed before the INSERT so that the audit record and outbox event of the same transaction can reference it.

PostgreSQL 18 provides a native `uuidv7()` function. It is used as `DEFAULT uuidv7()` on every `id` column (a safety net for seed and manual SQL) — no extension is required.

UUIDv7 is an identifier, never a secret: it embeds a timestamp and has limited randomness, so it must never be used as a session token, reset token, or any other bearer credential (05 §7.1).

---

# 5. Complete ERD

Conceptual ERD:

```text
                         ┌─────────────────┐
                         │    platforms    │
                         └────────┬────────┘
                                  │
                         ┌────────▼────────┐
                         │ organizations   │
                         └───┬─────────┬───┘
                             │         │
                 ┌───────────┘         └─────────────┐
                 │                                    │
        ┌────────▼─────────┐                 ┌────────▼───────┐
        │ organization_    │                 │     groups     │
        │ memberships      │                 └───────┬────────┘
        └──────────┬────────┘                         │
                   │                                  │
              ┌────▼─────┐                  ┌────────▼────────┐
              │  users   │◄─────────────────┤ group_memberships│
              └────┬─────┘                  └─────────────────┘
                   │
       ┌───────────┼────────────────────────────┐
       │           │                            │
┌──────▼──────┐ ┌──▼──────────────┐      ┌──────▼────────────┐
│ credentials │ │    sessions    │      │ security_events   │
└─────────────┘ └────────────────┘      └───────────────────┘

        AUTHORIZATION

┌──────────────┐       ┌────────────────┐
│    roles     │───────│ role_permissions│
└──────┬───────┘       └───────┬────────┘
       │                       │
       │                ┌──────▼────────┐
       │                │  permissions  │
       │                └──────┬────────┘
       │                       │
       └──────┐          ┌─────▼────────┐
              │          │    modules   │
              │          └──────────────┘
       ┌──────▼──────────┐
       │ role_assignments│
       └─────────────────┘

       ┌──────────────┐
       │   policies   │
       └──────────────┘

        ONBOARDING / EVENTS

       ┌──────────────┐  ┌───────────────────────┐  ┌───────────────┐
       │ invitations  │  │ password_reset_tokens │  │ outbox_events │
       └──────────────┘  └───────────────────────┘  └───────────────┘

        COMMUNICATION

┌────────────────┐
│ conversations  │
└───────┬────────┘
        │
  ┌─────┴─────────────┐
  │                   │
┌─▼────────────────┐ ┌▼──────────────┐
│ conversation_    │ │   messages    │
│ members          │ └──────┬────────┘
└──────────────────┘        │
                      ┌─────▼────────┐
                      │ attachments  │
                      └──────────────┘

       ┌────────────────┐
       │ notifications  │
       └────────────────┘

       ┌────────────────┐
       │  inbox_items   │
       └────────────────┘

       AUDIT / OPERATIONS

       ┌────────────────┐
       │    activities  │
       └────────────────┘

       ┌────────────────┐
       │  system_logs   │
       └────────────────┘

       CONFIGURATION

       ┌────────────────┐
       │    settings    │
       └────────────────┘
```

---

# 6. Platform Tables

## 6.1 platforms

Purpose: represent the installed platform instance.

Columns:

```text
id                  uuid PK
name                varchar(200)
installation_key    varchar(100) UNIQUE
version             varchar(50)
status              varchar(30)
created_at          timestamptz
updated_at          timestamptz
```

Normally a self-hosted deployment will contain one platform record.

---

# 7. Organization Tables

## 7.1 organizations

```text
id                uuid PK
name              varchar(200)
slug              varchar(100)
legal_name        varchar(255) NULL
status            varchar(30)
default_locale    varchar(20)
default_timezone  varchar(100)
created_at        timestamptz
updated_at        timestamptz
deleted_at        timestamptz NULL
```

Constraints:

```text
UNIQUE(slug)
```

Recommended index:

```text
idx_organizations_status
idx_organizations_deleted_at
```

---

# 8. User Tables

## 8.1 users

```text
id                uuid PK
username          varchar(100) NULL
display_name      varchar(200)
first_name        varchar(100) NULL
last_name         varchar(100) NULL
email             varchar(320)
phone             varchar(50) NULL
avatar_url        text NULL
locale            varchar(20)
timezone          varchar(100)
status            varchar(30)
last_login_at     timestamptz NULL
created_at        timestamptz
updated_at        timestamptz
deleted_at        timestamptz NULL
```

Required:

```text
UNIQUE(email)
UNIQUE(username)
CHECK (email = lower(email))
```

The application normalizes email to lowercase before every write; the CHECK constraint makes the database reject anything else. `citext` is not used.

---

# 9. Organization Membership

## 9.1 organization_memberships

```text
id                uuid PK
organization_id   uuid FK organizations(id)
user_id           uuid FK users(id)
status            varchar(30)
joined_at         timestamptz NULL
invited_by        uuid FK users(id) NULL
created_at        timestamptz
updated_at        timestamptz
```

Constraint:

```text
UNIQUE(organization_id, user_id)
```

Indexes:

```text
idx_org_memberships_user
idx_org_memberships_org_status
```

## 9.2 invitations

```text
id                uuid PK
organization_id   uuid FK organizations(id)
email             varchar(320)            -- lowercase
invited_by        uuid FK users(id)
group_id          uuid NULL               -- (group_id, organization_id) FK groups
role_id           uuid FK roles(id) NULL
token_hash        bytea UNIQUE
status            varchar(30)             -- pending | accepted | revoked | expired
expires_at        timestamptz
accepted_at       timestamptz NULL
revoked_at        timestamptz NULL
created_at        timestamptz
updated_at        timestamptz
```

Constraint: at most one `pending` invitation per `(organization_id, email)` (partial unique index).

---

# 10. Groups

## 10.1 groups

```text
id                uuid PK
organization_id   uuid FK organizations(id)
parent_group_id   uuid FK groups(id) NULL
name              varchar(200)
slug              varchar(100)
description       text NULL
status            varchar(30)
created_by        uuid FK users(id)
created_at        timestamptz
updated_at        timestamptz
deleted_at        timestamptz NULL
```

Constraint:

```text
UNIQUE(organization_id, slug)
```

Indexes:

```text
idx_groups_organization
idx_groups_parent
idx_groups_status
```

Additional constraint:

```text
UNIQUE(id, organization_id)      -- target of composite foreign keys
```

Enforced by the database (trigger, see 04 §6):

- a parent group belongs to the same organization;
- `organization_id` is immutable;
- the hierarchy has no cycles and a bounded depth (default 8).

---

# 11. Group Memberships

## 11.1 group_memberships

```text
id                uuid PK
group_id          uuid
organization_id   uuid
user_id           uuid
status            varchar(30)
joined_at         timestamptz NULL
added_by          uuid FK users(id) NULL
created_at        timestamptz
updated_at        timestamptz
```

Constraints:

```text
UNIQUE(group_id, user_id)
FK (group_id, organization_id)  → groups(id, organization_id)                              ON DELETE CASCADE
FK (organization_id, user_id)   → organization_memberships(organization_id, user_id)       ON DELETE RESTRICT
```

The composite foreign keys make "a group member must have an organization membership" a database guarantee.

Indexes:

```text
idx_group_memberships_user
idx_group_memberships_group_status
```

The application still verifies that the organization membership is `active` (the foreign key proves it exists, not that it is active), and deactivates group memberships when the organization membership is removed or suspended — in the same transaction.

---

# 12. Roles

## 12.1 roles

```text
id                uuid PK
organization_id   uuid FK organizations(id) NULL
group_id          uuid NULL               -- (group_id, organization_id) FK groups; group-owned roles
name              varchar(150)
slug              varchar(100)
description       text NULL
role_type         varchar(30)
status            varchar(30)
created_at        timestamptz
updated_at        timestamptz
```

A NULL organization_id represents a platform/global role.

Boundary CHECK:

```text
system       → organization_id NULL and group_id NULL
organization → organization_id NOT NULL and group_id NULL
group        → organization_id NOT NULL and group_id NOT NULL
custom       → organization_id NOT NULL (group_id optional)
```

Uniqueness (three partial unique indexes, because NULLs never collide in a composite UNIQUE):

```text
(slug)                 WHERE organization_id IS NULL
(organization_id, slug) WHERE organization_id IS NOT NULL AND group_id IS NULL
(group_id, slug)       WHERE group_id IS NOT NULL
```

---

# 13. Permissions

## 13.1 permissions

```text
id                uuid PK
module_id         uuid FK modules(id) NULL
resource          varchar(150)
action            varchar(100)
code              varchar(255)
description       text NULL
is_system         boolean
created_at        timestamptz
```

Constraint:

```text
UNIQUE(code)
```

Examples:

```text
user.read
user.create
user.update
user.delete

group.read
group.manage_members

invoice.read
invoice.create
invoice.update
invoice.delete
invoice.approve
invoice.export
```

---

# 14. Role Permissions

## 14.1 role_permissions

```text
id                uuid PK
role_id           uuid FK roles(id)
permission_id     uuid FK permissions(id)
created_by        uuid FK users(id) NULL
created_at        timestamptz
```

Constraint:

```text
UNIQUE(role_id, permission_id)
```

---

# 15. Role Assignments

## 15.1 role_assignments

```text
id                uuid PK
organization_id   uuid FK organizations(id) NULL
role_id           uuid FK roles(id)
subject_type      varchar(30)
subject_id        uuid
scope_type        varchar(30)
scope_id          uuid NULL
assigned_by       uuid FK users(id) NULL
expires_at        timestamptz NULL
created_at        timestamptz
revoked_at        timestamptz NULL
```

Important:

`subject_id` is polymorphic and therefore cannot be a normal foreign key.

Application/domain validation must verify subject existence.

Possible subject types:

```text
user
group
organization
service
```

Possible scopes:

```text
platform
organization
group
resource
own
```

Constraints:

```text
CHECK scope validity:
  platform | own        → scope_id IS NULL
  group | resource      → scope_id IS NOT NULL
  organization          → scope_id NULL or an organization id
CHECK (scope_type = 'platform') = (organization_id IS NULL)
UNIQUE (role_id, subject_type, subject_id, scope_type, COALESCE(scope_id, nil-uuid)) WHERE revoked_at IS NULL
```

Recommended indexes:

```text
idx_role_assignments_subject
idx_role_assignments_org
idx_role_assignments_scope
idx_role_assignments_active
```

---

# 16. Policies

## 16.1 policies

```text
id                uuid PK
organization_id   uuid FK organizations(id) NULL
name              varchar(150)
code              varchar(150)
description       text NULL
effect            varchar(20)
expression        text
status            varchar(30)
created_at        timestamptz
updated_at        timestamptz
```

Constraint:

```text
UNIQUE(organization_id, code)
```

The expression must use a controlled policy language.

Never execute arbitrary Go, SQL or JavaScript from a policy.

---

# 17. Modules

## 17.1 modules

```text
id                uuid PK
code              varchar(100)
name              varchar(200)
version           varchar(50)
description       text NULL
module_type       varchar(50)
status            varchar(30)
manifest          jsonb
installed_at      timestamptz
enabled_at        timestamptz NULL
disabled_at       timestamptz NULL
created_at        timestamptz
updated_at        timestamptz
```

Constraint:

```text
UNIQUE(code)
```

The manifest is stored as JSONB because the module contract will evolve.

---

# 18. Module Dependencies

## 18.1 module_dependencies

```text
id                  uuid PK
module_id           uuid FK modules(id)
dependency_module_id uuid FK modules(id)
version_constraint  varchar(100)
created_at          timestamptz
```

Constraint:

```text
UNIQUE(module_id, dependency_module_id)
```

Circular dependency detection belongs in the module service.

---

# 19. Module Installations

## 19.1 module_installations

```text
id                uuid PK
organization_id   uuid FK organizations(id)
module_id         uuid FK modules(id)
version           varchar(50)
status            varchar(30)
configuration     jsonb
installed_by      uuid FK users(id)
installed_at      timestamptz
enabled_at        timestamptz NULL
disabled_at       timestamptz NULL
updated_at        timestamptz
```

Constraint:

```text
UNIQUE(organization_id, module_id)
```

---

# 20. Settings

## 20.1 settings

```text
id                uuid PK
scope_type        varchar(30)
scope_id          uuid
module_id         uuid FK modules(id) NULL
key               varchar(200)
value             jsonb
value_type        varchar(30)
is_secret         boolean
created_by        uuid FK users(id) NULL
updated_by        uuid FK users(id) NULL
created_at        timestamptz
updated_at        timestamptz
```

Because scope_id is polymorphic, normal FK enforcement is not possible.

Valid scopes:

```text
platform
organization
group
user
module
```

Recommended unique logical key:

```text
(scope_type, scope_id, module_id, key)
```

---

# 21. Credentials

## 21.1 credentials

```text
id                uuid PK
user_id           uuid FK users(id)
credential_type   varchar(30)
secret_hash       text NULL
metadata          jsonb
created_at        timestamptz
updated_at        timestamptz
last_used_at      timestamptz NULL
revoked_at        timestamptz NULL
```

Credential types:

```text
password
totp
recovery_code
webauthn
```

Never store plaintext password or TOTP secrets in logs.

---

# 22. Sessions

## 22.1 sessions

Session payload is stored in Redis.

PostgreSQL may maintain session metadata when the UI needs historical/session-management information.

```text
id                uuid PK                 -- internal id, never a credential
token_hash        bytea UNIQUE            -- SHA-256 of the opaque cookie token
user_id           uuid FK users(id)
organization_id   uuid FK organizations(id) NULL
created_at        timestamptz
last_activity_at  timestamptz
expires_at        timestamptz
ip_address        inet
user_agent        text
auth_level        varchar(30)
revoked_at        timestamptz NULL
```

The session token itself is never stored — only its SHA-256 hash (here and as the Redis key). PostgreSQL is the durable record and the fallback lookup when Redis is unavailable (05 §7).

## 21.2 password_reset_tokens

```text
id                uuid PK
user_id           uuid FK users(id)
token_hash        bytea UNIQUE
expires_at        timestamptz
used_at           timestamptz NULL
ip_address        inet NULL
created_at        timestamptz
```

---

# 23. Security Events

## 23.1 security_events

```text
id                uuid PK
organization_id   uuid NULL               -- soft reference (no FK)
user_id           uuid NULL               -- soft reference (no FK)
session_id        uuid NULL               -- soft reference (no FK)
event_type        varchar(100)
result            varchar(30)
ip_address        inet NULL
user_agent        text NULL
request_id        varchar(100) NULL
metadata          jsonb
created_at        timestamptz
```

Examples:

```text
login.success
login.failed
password.changed
mfa.enabled
mfa.disabled
session.revoked
account.locked
```

Append-only. References are deliberately *soft* (no foreign keys): audit evidence must outlive the rows it points at, and a cascading foreign-key action would mutate rows that are supposed to be immutable.

---

# 24. Notifications

## 24.1 notifications

```text
id                  uuid PK
organization_id     uuid FK organizations(id) NULL
recipient_user_id   uuid FK users(id)
type                varchar(100)
title               varchar(255) NULL       -- English fallback only
body                text NULL               -- English fallback only
data                jsonb                   -- i18n parameters; text is rendered client-side from type + data
priority            varchar(20)
status              varchar(30)
created_at          timestamptz
read_at             timestamptz NULL
expires_at          timestamptz NULL
```

Indexes:

```text
idx_notifications_recipient_status
idx_notifications_created
```

---

# 25. Inbox

## 25.1 inbox_items

```text
id                  uuid PK
organization_id     uuid FK organizations(id)
recipient_user_id   uuid FK users(id)
type                varchar(100)
title               varchar(255)
description         text
resource_type       varchar(150) NULL
resource_id         uuid NULL
action_url          text NULL
status              varchar(30)
priority            varchar(20)
created_at          timestamptz
completed_at        timestamptz NULL
expires_at          timestamptz NULL
```

The resource reference is polymorphic.

---

# 26. Conversations

## 26.1 conversations

```text
id                uuid PK
organization_id   uuid FK organizations(id)
type              varchar(20)
title             varchar(255) NULL
direct_key        text NULL               -- only for type = 'direct'
created_by        uuid FK users(id)
status            varchar(30)
created_at        timestamptz
updated_at        timestamptz
archived_at       timestamptz NULL
```

Constraints:

```text
UNIQUE(id, organization_id)                                   -- target of composite FKs
CHECK ((type = 'direct') = (direct_key IS NOT NULL))
UNIQUE(organization_id, direct_key) WHERE type = 'direct'     -- one direct conversation per pair
```

`direct_key` is `least(user_a, user_b) || ':' || greatest(user_a, user_b)` (computed by the application).

Types:

```text
direct
group
```

---

# 27. Conversation Members

## 27.1 conversation_members

```text
id                    uuid PK
conversation_id       uuid
organization_id       uuid
user_id               uuid
member_role           varchar(30)
status                varchar(30)
joined_at             timestamptz
left_at               timestamptz NULL
last_read_message_id  uuid NULL
muted_until           timestamptz NULL
created_at            timestamptz
updated_at            timestamptz
```

Constraints:

```text
UNIQUE(conversation_id, user_id)
FK (conversation_id, organization_id) → conversations(id, organization_id)                ON DELETE CASCADE
FK (organization_id, user_id)         → organization_memberships(organization_id, user_id) ON DELETE RESTRICT
```

A foreign key for last_read_message_id can reference messages after messages exists.

---

# 28. Messages

## 28.1 messages

```text
id                  uuid PK
conversation_id     uuid FK conversations(id)
sender_user_id      uuid FK users(id)
message_type        varchar(30)
body                text NULL
metadata            jsonb
reply_to_message_id uuid FK messages(id) NULL
created_at          timestamptz
updated_at          timestamptz
deleted_at          timestamptz NULL
```

Indexes:

```text
idx_messages_conversation_created
idx_messages_sender
```

For large deployments, messages may eventually be partitioned by time or organization.

Do not partition the MVP prematurely.

---

# 29. Message Reads

## 29.1 message_reads

Optional detailed read tracking.

```text
id                uuid PK
message_id        uuid FK messages(id)
user_id           uuid FK users(id)
read_at           timestamptz
```

Constraint:

```text
UNIQUE(message_id, user_id)
```

For high-volume chat, prefer the lightweight `last_read_message_id` approach and only use detailed read rows where business requirements justify them.

---

# 30. Message Reactions

## 30.1 message_reactions

```text
id                uuid PK
message_id        uuid FK messages(id)
user_id           uuid FK users(id)
reaction          varchar(50)
created_at        timestamptz
```

Constraint:

```text
UNIQUE(message_id, user_id, reaction)
```

---

# 31. Attachments

## 31.1 attachments

```text
id                uuid PK
organization_id   uuid FK organizations(id)
owner_user_id     uuid FK users(id)
storage_provider  varchar(50)
storage_key       text
original_name     varchar(255)
content_type      varchar(150)
size_bytes        bigint
checksum          varchar(128)
status            varchar(30)
created_at        timestamptz
deleted_at        timestamptz NULL
```

Actual file bytes should be stored in object storage, not PostgreSQL, for normal production deployments.

MVP can support local filesystem storage through the same storage interface.

---

# 32. Activity / Audit

## 32.1 activities

```text
id                uuid PK
organization_id   uuid NULL               -- soft reference (no FK)
actor_user_id     uuid NULL               -- soft reference (no FK)
actor_type        varchar(30)
action            varchar(150)
resource_type     varchar(150) NULL
resource_id       uuid NULL
scope_type        varchar(30) NULL
scope_id          uuid NULL
result            varchar(30)
ip_address        inet NULL
user_agent        text NULL
request_id        varchar(100) NULL
trace_id          varchar(100) NULL
metadata          jsonb
created_at        timestamptz
```

Recommended indexes:

```text
idx_activities_org_created
idx_activities_actor_created
idx_activities_resource
idx_activities_action
```

Activities are append-only (the runtime database role cannot UPDATE or DELETE them — 04 §13). References are soft so that audit evidence survives and so that anonymization (13 §7) is the only sanctioned mutation, performed by a separate maintenance role.

---

# 33. System Logs

## 33.1 system_logs

```text
id                uuid PK
service           varchar(100)
environment       varchar(50)
level             varchar(20)
event_code        varchar(150)
message           text
request_id        varchar(100) NULL
trace_id          varchar(100) NULL
metadata          jsonb
created_at        timestamptz
```

Indexes:

```text
idx_system_logs_created
idx_system_logs_level_created
idx_system_logs_service_created
```

Production deployments may eventually stream logs to Loki, Elasticsearch/OpenSearch or another external logging system.

PostgreSQL storage is acceptable for MVP operational logs with retention limits.

## 33.2 outbox_events

Transactional outbox (see 02 §31.3, 10 §6).

```text
id                uuid PK
organization_id   uuid NULL               -- soft reference
event_name        varchar(200)
aggregate_type    varchar(100) NULL
aggregate_id      uuid NULL
payload           jsonb
request_id        varchar(100) NULL
trace_id          varchar(100) NULL
status            varchar(20)             -- pending | processed | dead
attempts          integer
available_at      timestamptz
last_error        text NULL
created_at        timestamptz
processed_at      timestamptz NULL
```

Index: `(available_at, created_at) WHERE status = 'pending'` — the relay reads with `FOR UPDATE SKIP LOCKED`.

---

# 34. Entity Relationship Details

## User ↔ Organization

```text
users
  1
  │
  │
  N
organization_memberships
  N
  │
  │
  1
organizations
```

Many-to-many relationship.

---

# 35. User ↔ Group

```text
users
  1
  │
  N
group_memberships
  N
  │
  1
groups
```

Many-to-many relationship.

---

# 36. Role ↔ Permission

```text
roles
  1
  │
  N
role_permissions
  N
  │
  1
permissions
```

Many-to-many relationship.

---

# 37. Role ↔ Subject

```text
roles
   │
   N
role_assignments
   N
   │
   └── subject_id
       ├── user
       ├── group
       ├── organization
       └── service
```

This is intentionally polymorphic.

---

# 38. Organization ↔ Modules

```text
organizations
  1
  │
  N
module_installations
  N
  │
  1
modules
```

This allows a module to be enabled independently per organization.

---

# 39. Conversation ↔ User

```text
conversations
  1
  │
  N
conversation_members
  N
  │
  1
users
```

---

# 40. Conversation ↔ Message

```text
conversation
    1
    │
    N
 messages
```

Messages are never globally accessible without conversation authorization.

---

# 41. Tenant Isolation

Every organization-owned resource must have an unambiguous organization context.

Preferred pattern:

```text
resource.organization_id
```

for frequently queried business entities.

For entities where organization ownership is inherited, the relationship must be guaranteed.

Example:

```text
conversation
  → organization

message
  → conversation
  → organization
```

The application must always resolve the organization before querying business data.

Guarantees beyond convention:

- composite foreign keys tie `group_memberships`, `conversation_members`, `roles.group_id` and `invitations.group_id` to the same organization as their parent (04 §6, §8, §11);
- repositories take `organization_id` as a mandatory argument; the rare cross-tenant (platform-level) query is a separately named method that is only callable after a platform-scope authorization decision (10 §13, 12 §6).

---

# 42. PostgreSQL Row-Level Security

RLS is not mandatory for MVP.

Recommended approach:

```text
Application authorization
+
Strong repository scoping
+
Database constraints
```

RLS may be added later for high-assurance multi-tenant deployments.

If RLS is introduced, it must be designed together with connection pooling and transaction-local tenant context.

Do not enable RLS partially without a complete policy strategy.

---

# 43. Index Strategy

Every high-cardinality organization-scoped table should generally have an organization index.

Common patterns:

```text
(organization_id)
(organization_id, status)
(organization_id, created_at DESC)
(user_id, created_at DESC)
(group_id, status)
(conversation_id, created_at DESC)
```

Do not create indexes blindly.

Indexes must correspond to actual query patterns.

---

# 44. Audit Retention

Audit retention must be configurable.

Suggested baseline:

```text
Activities:
long retention

Security events:
long retention

System logs:
shorter configurable retention

Chat:
business-configurable retention
```

A future archival system may move old records to cheaper storage.

---

# 45. Migration Order

The authoritative, numbered list is in 04-database-schema.md §14 (it supersedes the first draft of this section, which omitted `module_installations` and the tables added in Rev 1.1). Summary of the dependency order:

```text
common functions → platforms → organizations → users → organization_memberships → groups
→ group_memberships → modules → module_dependencies → module_installations
→ permissions → roles → role_permissions → role_assignments → policies → settings
→ credentials → sessions → security_events → invitations → password_reset_tokens
→ notifications → inbox_items → conversations → conversation_members → messages
→ message_reads → message_reactions → attachments → activities → system_logs
→ outbox_events → grants
```

Foreign-key-dependent migrations must be ordered after their referenced tables. Migration version numbers are strictly increasing; a migration that has been tagged is never renumbered.

---

# 46. Transaction Boundaries

Examples requiring transactions:

## Create user

```text
users
+
organization_membership
+
initial role assignment
+
audit activity
+
outbox event
```

## Create group

```text
groups
+
initial administrator membership/assignment
+
audit activity
```

## Change role permissions

```text
role_permissions
+
authorization cache invalidation event
+
audit activity
```

## Password change

```text
credentials
+
security event
+
activity
+
outbox event (→ notification, other sessions revocation fan-out)
```

All rows above are written by one `TxManager.WithinTx` call. Anything slow or fallible (email, WebSocket push, cache invalidation in Redis) happens *after* commit, driven by the outbox or by an in-process handler — never inside the transaction. Activities with `result = denied/failed` are written in a separate transaction because the main one rolls back.

---

# 47. Cache Invalidation

Authorization-related changes must invalidate Redis cache.

Examples:

```text
role_permissions changed
role_assignment changed
group_membership changed
user status changed
module enabled/disabled
policy changed
```

Cache keys (final design in 06 §8):

```text
authz:ver:{organization_id}                                   -- version counter, INCR on any authorization change
authz:eff:{organization_id}:{version}:{user_id}:{group|-}     -- effective permission set
module:{organization_id}:{module_code}                        -- module enabled flag
```

Invalidation is an O(1) version bump, not a key scan. If Redis is unavailable, the effective set is computed from PostgreSQL (fail-safe).

---

# 48. Concurrency

Important records should support optimistic concurrency where appropriate.

For example:

```text
version bigint
```

may be added to:

- settings
- module installations
- conversations
- selected business records

The API rejects stale updates with:

```text
409 Conflict   (error code: version_conflict)
```

This is preferable to silently overwriting another user's changes.

---

# 49. Soft Delete

Soft-deleted records should normally be excluded by repository methods.

Example conceptual query:

```sql
WHERE deleted_at IS NULL
```

Repositories should make active-record queries the default.

Administrative recovery may be implemented later.

---

# 50. Data Integrity Rules

The application must never rely exclusively on frontend validation.

PostgreSQL should enforce:

- foreign keys
- uniqueness
- non-null requirements
- valid status values
- positive numeric limits where relevant
- relationship integrity

Go services enforce:

- authorization
- cross-entity invariants
- workflow rules
- policy evaluation
- business constraints

---

# 51. Sensitive Data

Do not store sensitive secrets directly in ordinary JSON metadata.

Never store:

```text
password
access token
refresh token
session secret
MFA recovery secret
private key
encryption key
```

as ordinary audit metadata.

Sensitive values should be:

- hashed when verification is required
- encrypted when recovery is required
- stored in a dedicated secret manager where appropriate

---

# 52. Future Scaling

The schema is designed to support:

```text
Load Balancer
      ↓
Go API × N
      ↓
PostgreSQL
      +
Redis
      +
Worker × N
```

Later:

```text
PostgreSQL primary
       ├── read replicas
       └── archive

Redis cluster

Object Storage

Message Broker

Search Engine
```

The initial schema should not prematurely introduce distributed complexity.

---

# 53. ERD-to-Go Mapping

Recommended mapping:

```text
users                  → internal/identity
organizations          → internal/organization
groups                 → internal/group
memberships            → internal/membership
roles                  → internal/role
permissions            → internal/permission
policies               → internal/policy
modules                → internal/module
notifications          → internal/notification
inbox_items            → internal/inbox
conversations/messages → internal/chat
activities             → internal/audit
system_logs            → internal/systemlog
settings               → internal/settings
sessions               → internal/auth
invitations            → internal/invitation
outbox_events          → internal/kernel (outbox)
```

A Go package owns its repositories and domain rules.

---

# 54. Next Document

04-database-schema.md contains the implementation-level PostgreSQL schema: exact SQL types, CREATE TABLE statements, CHECK strategy, indexes, partial unique indexes, foreign keys, triggers, `updated_at` strategy, UUIDv7 handling, database roles and grants, migration conventions and rollback strategy, seed and bootstrap rules.
