# Domain Model Specification
## Open Source Modular Application Platform v1.0

**Document:** 02-domain-model.md  
**Status:** Baseline (Rev 1.1 — amendments from 00-errata-and-amendments.md applied)  
**Previous document:** 01-product-requirements.md  
**Next document:** 03-database-erd.md

---

# 1. Purpose

This document defines the business entities, relationships, ownership boundaries, lifecycle rules, and invariants of the platform.

The domain model is the foundation for:

- PostgreSQL tables
- Go domain packages
- repository interfaces
- application services
- REST API resources
- Svelte routes and stores
- authorization decisions
- audit events
- module contracts

The domain model must be approved before implementing the complete database schema.

---

# 2. Domain Design Principles

## 2.1 Explicit ownership

Every entity must have a clear owner.

Examples:

- A user belongs to the Identity domain.
- A group belongs to an Organization.
- A membership connects a User to an Organization or Group.
- A role belongs to an authorization boundary.
- A conversation owns its messages.
- A module owns its business tables.
- An activity belongs to the Audit domain.

## 2.2 No implicit authorization

A relationship does not automatically grant permission.

For example:

- Being a group member does not automatically allow reading every group resource.
- Being an Admin does not automatically allow granting every permission.
- Knowing a conversation ID does not grant access to the conversation.

## 2.3 Organization-aware data

Business data must be associated with an organization unless it is explicitly platform-global.

## 2.4 Soft deletion where appropriate

Entities such as users, groups, organizations, modules and conversations should generally use a status or deleted timestamp rather than immediate physical deletion.

Audit records must not be deleted through normal application operations.

## 2.5 Immutable historical records

The following should be treated as append-only or historically preserved:

- audit activities
- security events
- login history
- system log records
- financial or legally relevant business records where required by a module

---

# 3. Context Map

The platform is divided into bounded contexts.

```text
Platform
├── Identity
├── Authentication
├── Organization
├── Membership
├── Authorization
├── Module
├── Communication
│   ├── Notification
│   ├── Inbox
│   └── Chat
├── Audit
├── System Operations
├── Settings
└── Business Modules
```

## 3.1 Identity

Responsible for:

- users
- profiles
- user status
- identity attributes
- locale and timezone preferences

## 3.2 Authentication

Responsible for:

- login
- password credentials
- sessions
- MFA
- recovery codes
- login history
- credential changes

## 3.3 Organization

Responsible for:

- organizations
- organization settings
- organization status
- organization-level configuration

## 3.4 Membership

Responsible for:

- organization membership
- group membership
- membership status
- membership roles and scopes

## 3.5 Authorization

Responsible for:

- roles
- permissions
- policies
- role assignments
- permission evaluation
- field-level access

## 3.6 Module

Responsible for:

- module registration
- module metadata
- module versions
- dependencies
- lifecycle
- module configuration

## 3.7 Communication

Responsible for:

- notifications
- inbox items
- conversations
- messages
- read state
- presence and realtime events

## 3.8 Audit

Responsible for:

- user activities
- security activities
- data changes
- access events
- authorization decisions when configured

## 3.9 System Operations

Responsible for:

- system logs
- service health
- worker failures
- infrastructure events
- operational diagnostics

---

# 4. Core Entity Overview

```text
Platform
  └── Organization
       ├── Organization Membership
       │    └── User
       │
       ├── Group
       │    └── Group Membership
       │         └── User
       │
       ├── Role
       │    └── Role Permission
       │         └── Permission
       │
       ├── Policy
       ├── Module Installation
       ├── Settings
       ├── Conversations
       │    ├── Conversation Member
       │    └── Messages
       ├── Notifications
       ├── Inbox Items
       └── Activities
```

---

# 5. Platform

The Platform represents the installed application instance.

## Attributes

- id
- name
- installation identifier
- version
- status
- created_at
- updated_at

## Responsibilities

- global configuration
- platform-level administrators
- global modules
- platform security policies
- global operational settings

A platform may contain multiple organizations.

---

# 6. Organization

An Organization is a tenant or isolated business workspace.

Examples:

- ACME Corporation
- Jakarta Education Office
- A school
- A finance company
- A project team

## Attributes

- id
- name
- slug
- legal_name, optional
- status
- default_locale
- default_timezone
- created_at
- updated_at
- deleted_at

## Organization statuses

```text
active
suspended
archived
```

A suspended organization cannot normally authenticate or use business modules, except for authorized platform operators.

---

# 7. User

A User represents a person or service identity that can authenticate or interact with the platform.

## Attributes

- id
- username, optional
- display_name
- first_name, optional
- last_name, optional
- email
- phone, optional
- avatar_url, optional
- locale
- timezone
- status
- last_login_at, optional
- created_at
- updated_at
- deleted_at

## User statuses

```text
pending
active
suspended
locked
deactivated
deleted
```

## Rules

1. Email uniqueness must be defined according to deployment policy.
2. A suspended user cannot create normal sessions.
3. A deactivated user retains historical audit references.
4. A user may belong to multiple organizations.
5. A user may belong to multiple groups within one organization.

---

# 8. User Profile

The profile is the user-facing representation of identity information.

Profile data may include:

- display name
- avatar
- contact details
- locale
- timezone
- language
- notification preferences

Profile data is not the same as authentication credentials.

---

# 9. Organization Membership

Organization Membership connects a User to an Organization.

```text
User ───< Organization Membership >─── Organization
```

## Attributes

- id
- organization_id
- user_id
- status
- joined_at
- invited_by
- created_at
- updated_at

## Statuses

```text
invited
active
suspended
removed
```

## Rules

1. A user may have only one membership record per organization.
2. Removing a membership must not delete the user.
3. A user may be active in one organization and suspended in another.
4. Organization membership is required before accessing organization-scoped resources.

---

# 10. Group

A Group is a logical subdivision inside an organization.

Examples:

- Finance
- Human Resources
- Engineering
- Operations
- Event A
- Event B

## Attributes

- id
- organization_id
- parent_group_id, optional
- name
- slug
- description
- status
- created_by
- created_at
- updated_at
- deleted_at

## Rules

1. A group belongs to exactly one organization.
2. A group cannot reference a parent group from another organization.
3. Hierarchical groups are optional but should be supported.
4. Deleting a group must define behavior for memberships and role assignments.
5. A group administrator cannot manage groups outside their authorized scope.
6. `organization_id` of a group is immutable after creation.
7. The group hierarchy is acyclic and has a bounded depth (default maximum 8 levels). Both are enforced by the database (04 §6), not only by the application.

---

# 11. Group Membership

Group Membership connects a User to a Group.

```text
User ───< Group Membership >─── Group
```

## Attributes

- id
- group_id
- organization_id (denormalized from the group; guarantees invariant 2 through a composite foreign key)
- user_id
- status
- joined_at
- added_by
- created_at
- updated_at

## Statuses

```text
invited
active
suspended
removed
```

## Rules

1. The user must have active organization membership.
2. A user cannot be active in a group belonging to another organization.
3. Group membership alone does not grant business permissions.
4. A group membership may be used as a scope input during authorization.
5. The database guarantees that (group, organization) is a real pair and that (organization, user) is a real organization membership (composite foreign keys). Whether that organization membership is still `active` is checked by the application in the same transaction.
6. When an organization membership becomes `removed` or `suspended`, the application deactivates the user's group memberships in that organization in the same transaction.

---

# 12. Role

A Role is a named collection of permissions.

Examples:

- Super Admin
- Organization Admin
- Finance Manager
- Finance Staff
- Viewer
- Auditor

## Attributes

- id
- organization_id, nullable for platform-global roles
- group_id, optional (only for group-scoped roles; must belong to `organization_id`)
- name
- slug
- description
- role_type
- status
- created_at
- updated_at

## Role types

```text
system
organization
group
custom
```

## Rules

1. System roles cannot be deleted through normal UI.
2. Custom roles may be created by authorized administrators.
3. A role must belong to a defined authorization boundary:
   - `system` → `organization_id` and `group_id` are both NULL (platform-global)
   - `organization` → `organization_id` set, `group_id` NULL
   - `group` → both set; the role is owned by that group (a Group Admin manages it)
   - `custom` → `organization_id` set, `group_id` optional
4. A role cannot grant permissions beyond the assigning administrator's authority.
5. Slug is unique inside its boundary (global, per organization, or per group).

---

# 13. Permission

A Permission represents an allowed operation on a resource.

Permission naming convention:

```text
resource.action
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

## Attributes

- id
- module_id, optional
- resource
- action
- code
- description
- is_system
- created_at

## Rules

1. Permission codes must be globally unique.
2. Permissions should be registered by modules.
3. A permission is not sufficient by itself; scope and policy may still deny access.
4. Deactivated module permissions must not be granted to new assignments.

---

# 14. Role-Permission Assignment

This relationship connects roles to permissions.

```text
Role ───< Role Permission >─── Permission
```

## Attributes

- id
- role_id
- permission_id
- created_at
- created_by

## Rules

1. A role cannot contain duplicate permission assignments.
2. Removing a permission from a role must invalidate related authorization caches.
3. Changes must generate an audit activity.
4. Permission changes may require elevated authentication.

---

# 15. Role Assignment

A Role Assignment connects a role to a subject.

A subject may be:

- a user
- a group
- an organization
- a service identity

## Attributes

- id
- organization_id, optional
- role_id
- subject_type
- subject_id
- scope_type
- scope_id, optional
- assigned_by
- created_at
- expires_at, optional
- revoked_at, optional

## Subject types

```text
user
group
organization
service
```

## Scope types

```text
platform
organization
group
resource
own
```

## Rules

1. A role assignment must have a valid subject.
2. A group-scoped role must reference a group in the same organization.
3. An administrator cannot assign a role with greater authority than the administrator possesses.
4. Temporary assignments may expire automatically.
5. Revoked assignments must not remain effective in cache.
6. Scope validity is a database rule: `platform` and `own` have no `scope_id`; `group` and `resource` require one; `organization` may leave it NULL (meaning "the assignment's own organization"). A platform-scoped assignment has no `organization_id`; every other scope has one.
7. There is at most one active (non-revoked) assignment per (role, subject, scope).
8. A `group` subject means every active member of that group inherits the role; the `scope_type`/`scope_id` still decides where the role applies.

---

# 16. Policy

A Policy adds conditional logic to authorization.

Examples:

```text
invoice.status != "closed"
user belongs to finance group
resource.owner_id == current_user.id
amount <= approval_limit
```

## Attributes

- id
- organization_id, optional
- name
- code
- description
- effect
- expression
- status
- created_at
- updated_at

## Effects

```text
allow
deny
```

## Rules

1. Policies must be evaluated server-side.
2. Policies must use a controlled expression language.
3. Arbitrary code execution is forbidden.
4. Deny policies should take precedence where explicitly configured.
5. Policy evaluation must be auditable for sensitive operations.

---

# 17. Field Permission

Field permissions define which attributes a user may read or modify.

Examples:

```text
invoice.internal_cost.read
user.phone.read
employee.salary.read
```

## Rules

1. Field permissions must be enforced before serialization.
2. A hidden field must not be sent to the client.
3. Write permissions must be checked independently from read permissions.
4. Field-level restrictions must apply to REST, WebSocket, exports and background jobs.

---

# 18. Module

A Module is a deployable or installable business capability.

Examples:

- Finance
- HR
- Inventory
- CRM
- Education
- Project Management

## Attributes

- id
- code
- name
- version
- description
- module_type
- status
- manifest
- installed_at
- enabled_at
- disabled_at
- created_at
- updated_at

## Module statuses

```text
registered
installed
enabled
disabled
failed
uninstalled
```

## Rules

1. Module codes must be unique.
2. A module must declare dependencies.
3. Circular dependencies are invalid.
4. A module cannot be enabled if required migrations fail.
5. Disabling a module should preserve its data unless explicitly uninstalled.
6. Uninstallation requires an explicit destructive-operation confirmation.

---

# 19. Module Installation

Module Installation represents a module installed for a specific organization.

```text
Organization ───< Module Installation >─── Module
```

## Attributes

- id
- organization_id
- module_id
- version
- status
- configuration
- installed_by
- installed_at
- enabled_at
- disabled_at
- updated_at

## Rules

1. A module may be globally installed but disabled for an organization.
2. Organization-specific configuration must be isolated.
3. A user can access a module only if the module is enabled and the user has permission.
4. Module configuration changes must be audited.

---

# 20. Settings

Settings are key-value configuration records.

Settings may exist at:

```text
platform
organization
group
user
module
```

## Attributes

- id
- scope_type
- scope_id
- module_id, optional
- key
- value
- value_type
- is_secret
- updated_by
- created_at
- updated_at

## Rules

1. Secret settings must be encrypted or stored through a secret manager.
2. Secrets must never be returned in normal API responses.
3. Settings must have schema validation.
4. User settings cannot override security-critical platform settings.
5. Settings changes must be audited.

---

# 21. Notification

A Notification is a system-generated message directed to one or more users.

Examples:

- password changed
- new approval request
- role changed
- module enabled
- security alert

## Attributes

- id
- organization_id, optional
- recipient_user_id
- type
- title, optional (English fallback only)
- body, optional (English fallback only)
- data (parameters for client-side rendering; see 08 §8)
- priority
- status
- created_at
- read_at, optional
- expires_at, optional

## Statuses

```text
unread
read
archived
expired
```

Notifications are distinct from chat messages. The user-facing text is rendered by the client from `type` + `data` in the user's locale; `title`/`body` exist only as a fallback for clients that do not localize.

---

# 22. Inbox Item

An Inbox Item is an actionable task or work item.

Examples:

- approve invoice
- review registration
- complete profile
- respond to an administrative request

## Attributes

- id
- organization_id
- recipient_user_id
- type
- title
- description
- resource_type
- resource_id
- action_url
- status
- priority
- created_at
- completed_at, optional
- expires_at, optional

## Statuses

```text
pending
in_progress
completed
dismissed
expired
```

Inbox items may generate notifications but are not identical to notifications.

---

# 23. Conversation

A Conversation is a communication container.

Types:

```text
direct
group
```

## Attributes

- id
- organization_id
- type
- title, optional
- direct_key, only for `direct` (a canonical key of the two participants; makes "one direct conversation per pair" a database guarantee)
- created_by
- status
- created_at
- updated_at
- archived_at, optional

## Rules

1. A direct conversation must have exactly two members.
2. A group conversation must have at least two members.
3. A conversation belongs to one organization unless explicitly designed as platform-global.
4. Archived conversations remain readable according to policy.
5. Membership is required for access.
6. `direct_key` is unique per organization for direct conversations, so two concurrent requests cannot create duplicates.

---

# 24. Conversation Member

Conversation Member connects a user to a conversation.

## Attributes

- id
- conversation_id
- organization_id (denormalized from the conversation; guarantees invariant 5 through composite foreign keys)
- user_id
- member_role
- status
- joined_at
- left_at, optional
- last_read_message_id, optional
- muted_until, optional

## Member roles

```text
member
moderator
owner
```

## Rules

1. Only active members may read or send messages.
2. A user cannot add another user without the required permission.
3. Leaving a conversation must preserve historical messages according to retention policy.
4. Conversation membership checks are mandatory for every message operation.

---

# 25. Message

A Message is a unit of content inside a conversation.

## Attributes

- id
- conversation_id
- sender_user_id
- message_type
- body
- metadata
- reply_to_message_id, optional
- created_at
- updated_at
- deleted_at, optional

## Message types

```text
text
system
file
image
event
```

## Rules

1. Sender must be an active conversation member.
2. Deleted messages should normally be represented as tombstones.
3. Message editing must respect time and permission policies.
4. Message content must be validated and size-limited.
5. Attachments must be stored outside PostgreSQL when large.

---

# 26. Message Read State

Message read state tracks the last message read by a member.

Possible model:

```text
conversation_members.last_read_message_id
```

For detailed auditing, an additional message_reads table may be used.

Read events may be rate-limited or aggregated to avoid excessive writes.

---

# 27. Attachment

An Attachment represents a file associated with a message or business resource.

## Attributes

- id
- organization_id
- owner_user_id
- storage_provider
- storage_key
- original_name
- content_type
- size_bytes
- checksum
- status
- created_at
- deleted_at

## Rules

1. File access must be authorization-checked.
2. Content type must not be trusted solely from the client.
3. File size limits are mandatory.
4. Malware scanning may be added through the worker.
5. Storage keys must not expose sensitive information.

---

# 28. Activity

An Activity is a user-visible or security-relevant record of an action.

## Attributes

- id
- organization_id, optional
- actor_user_id, optional
- actor_type
- action
- resource_type
- resource_id
- scope_type
- scope_id, optional
- result
- ip_address
- user_agent
- request_id
- trace_id, optional
- metadata
- created_at

## Actions

Examples:

```text
user.login
user.logout
user.password_changed
user.profile_updated
role.assigned
permission.changed
invoice.created
invoice.updated
invoice.deleted
message.read
conversation.created
```

## Rules

1. Activities are append-only.
2. Activity visibility is permission-controlled.
3. Sensitive metadata must be redacted.
4. Activity records should not contain raw credentials or tokens.
5. High-volume read events may be sampled or aggregated according to policy.

---

# 29. System Log

A System Log records operational events generated by the platform.

## Attributes

- id
- service
- environment
- level
- event_code
- message
- request_id, optional
- trace_id, optional
- metadata
- created_at

## Levels

```text
debug
info
warn
error
fatal
```

System logs are operational records and should not be exposed to normal users.

---

# 30. Session

A Session represents an authenticated login context.

The session payload should primarily be stored in Redis.

## Metadata

- id (internal identifier; never sent to the client as a credential)
- token_hash (SHA-256 of the opaque random token carried by the cookie)
- user_id
- organization_id, optional
- created_at
- last_activity_at
- expires_at
- ip_address
- user_agent
- auth_level
- revoked_at, optional

## Rules

1. Sessions must be revocable.
2. Password changes may revoke other sessions.
3. MFA changes may revoke sessions.
4. Suspended users must have sessions invalidated.
5. The session token is 32 bytes from a cryptographically secure generator. It is never derived from time or from any identifier, and only its hash is stored (Redis key and `sessions.token_hash`). UUIDv7 must not be used as a token.
6. Revoking a session or suspending a user or organization must also terminate that user's live WebSocket connections.

---

# 31. Credential

Credentials represent authentication factors.

Initial credential types:

```text
password
totp
recovery_code
```

Future types:

```text
webauthn
passkey
external_oidc
```

Credential records must never store plaintext secrets.

---

# 31.1 Invitation

An Invitation lets an administrator bring a person (existing or new) into an organization, optionally with a preset group and role.

## Attributes

- id, organization_id, email (lowercase)
- invited_by
- group_id, optional; role_id, optional
- token_hash (the token itself is only ever sent by email)
- status: `pending | accepted | revoked | expired`
- expires_at, accepted_at, revoked_at
- created_at, updated_at

## Rules

1. The token is random, single-use, short-lived, and stored only as a hash.
2. At most one pending invitation per (organization, email).
3. Accepting creates or activates the user, the organization membership, and (if preset) the group membership and role assignment in one transaction.
4. The preset role can never exceed the inviter's authority (authority ceiling, 06 §5.3).

---

# 31.2 Password Reset Token

Single-use, short-lived token for the forgot-password flow.

## Attributes

- id, user_id, token_hash, expires_at, used_at, ip_address, created_at

## Rules

1. Stored only as a hash. Using a token invalidates all other outstanding tokens of that user.
2. A successful reset revokes all of the user's sessions.
3. Expired and used tokens may be hard-deleted by the maintenance job.

---

# 31.3 Outbox Event

A durable record of a domain event written in the same transaction as the state change that caused it (transactional outbox).

## Attributes

- id, organization_id (optional), event_name, aggregate_type, aggregate_id
- payload, request_id, trace_id
- status: `pending | processed | dead`
- attempts, available_at, last_error, created_at, processed_at

## Rules

1. An event exists if and only if its transaction committed.
2. Delivery to the worker is at-least-once; every handler must be idempotent (deduplicate by event id).
3. Events that keep failing move to `dead` and are surfaced in `system_logs`.
4. Processed rows are purged by the maintenance job after a retention window.

---

# 32. Security Event

A Security Event records important authentication and security changes.

Examples:

- failed login
- successful login
- password changed
- MFA enabled
- MFA disabled
- session revoked
- suspicious login
- account locked

Security events should be linked to the user and optionally to a session, IP address, request ID and organization.

---

# 33. Entity Relationship Summary

```text
Platform
  ├── Organizations
  ├── Global Roles
  ├── Global Permissions
  ├── Global Modules
  └── Global Settings

Organization
  ├── Organization Memberships ─── User
  ├── Groups
  ├── Organization Roles
  ├── Policies
  ├── Module Installations ─── Module
  ├── Settings
  ├── Notifications
  ├── Inbox Items
  ├── Conversations
  ├── Activities
  └── Business Module Data

Group
  ├── Parent Group
  ├── Group Memberships ─── User
  └── Scoped Role Assignments

Role
  ├── Role Permissions ─── Permission
  └── Role Assignments

Conversation
  ├── Conversation Members ─── User
  └── Messages
       ├── Sender ─── User
       ├── Replies
       └── Attachments
```

---

# 34. Critical Invariants

The following invariants must be enforced in the database and application layer.

1. A group cannot belong to two organizations.
2. A group member must have organization membership.
3. A role assignment cannot reference an invalid scope.
4. A user cannot access an organization without active membership.
5. A conversation member must belong to the conversation's organization.
6. A message sender must be an active conversation member.
7. A module permission code must be unique.
8. A disabled module cannot expose active routes.
9. Audit records cannot be updated through normal application APIs.
10. System logs cannot be accessed by normal users.
11. Field restrictions must be enforced before serialization.
12. Revoked sessions must not authenticate requests.
13. An administrator cannot delegate authority greater than their own.
14. Redis failure must not destroy authoritative business data.
15. All organization-scoped records must include organization context directly or through a guaranteed ownership chain.
16. A group hierarchy is acyclic, bounded in depth, and stays inside one organization; a group's organization never changes.
17. A role's boundary (`system` / `organization` / `group` / `custom`) matches its `organization_id` / `group_id` columns.
18. A state change, its audit record, and its outbox event are committed atomically or not at all.

## 34.1 Where each invariant is enforced

| # | Database | Application |
|---|---|---|
| 1, 2, 5, 16 | composite foreign keys, triggers (04 §6, §11) | active-status checks |
| 3, 17 | CHECK constraints (04 §8) | authority ceiling |
| 6 | – | conversation membership check on every message operation |
| 7 | UNIQUE(permissions.code) | catalog sync |
| 8 | – | route gating per organization (07 §3) |
| 9 | REVOKE UPDATE/DELETE for the runtime role (04 §13) | no update code path |
| 12 | – | session lookup + revocation (05) |
| 13 | – | `grant.go` (06 §5.3) |
| 14 | – | PostgreSQL is authoritative; Redis is rebuildable |
| 15 | `organization_id` columns + FKs | mandatory scoping in repositories (10 §13) |
| 18 | outbox table | `TxManager.WithinTx` (10 §6) |

---

# 35. Recommended Database Constraints

The PostgreSQL design should include:

- primary keys using UUIDv7 (generated by the application; `DEFAULT uuidv7()` on PostgreSQL 18 as a safety net)
- foreign keys
- unique constraints
- check constraints for statuses and types
- partial indexes for active records
- indexes on organization_id
- indexes on user_id
- indexes on created_at
- indexes on status
- composite indexes for common authorization queries
- optimistic locking where concurrent updates are likely

Examples:

```text
unique(users.email)
unique(organizations.slug)
unique(groups.organization_id, groups.slug)
unique(organization_memberships.organization_id, organization_memberships.user_id)
unique(group_memberships.group_id, group_memberships.user_id)
unique(permissions.code)
unique(module_installations.organization_id, module_installations.module_id)
unique(conversations.organization_id, conversations.direct_key) where type = 'direct'
check(users.email = lower(users.email))
```

---

# 36. Deletion Policy

## Hard deletion may be allowed for

- temporary sessions
- expired recovery codes
- cache records
- temporary upload metadata
- transient worker jobs
- expired invitations and used/expired password reset tokens
- processed outbox events after the retention window

## Soft deletion or archival is preferred for

- users
- organizations
- groups
- roles
- modules
- conversations
- messages
- business records
- settings with historical relevance

## Never normally delete

- audit activities
- security events
- legally required records
- immutable operational evidence

---

# 37. Next Document

03-database-erd.md translates this domain model into the PostgreSQL ERD, and 04-database-schema.md into exact SQL. The ERD must be completed before writing production Go repositories and services.
