# Product Requirements
## Open Source Modular Application Platform v1.0

**Document:** 01-product-requirements.md
**Status:** Baseline (Rev 1.1)
**Next:** 02-domain-model.md

---

*A note on ordering: this is document 01 in the numbered series, but the last one originally written. The architecture was designed first and this requirements document was assembled from what it already encodes — so a future reader starting from 01 reads intent before implementation, the way the numbering always intended, even though the actual writing happened in the other direction.*

*Rev 1.1 note: after a full review, the series was corrected and extended (see 00-errata-and-amendments.md for the change record), and a build plan was added (21-milestone-plan.md, 22-handover-protocol.md, 23-prompt-library.md).*

---

# 1. Problem Statement

Almost every internal tool, admin panel, or B2B SaaS product needs the same foundation before any of its actual business logic can start: who can log in, how they're organized, what they're allowed to do, and a record of what they did. Teams rebuild this foundation — often imperfectly, without proper audit trails, with permissions bolted on as an afterthought — for every new project.

This platform exists so that foundation is built once, correctly, to a genuinely production-grade standard, and made available as open source — so building a new application on top of it means writing a **module** (07-module-system.md) instead of rebuilding authentication and user management from scratch.

---

# 2. Personas

| Persona | Cares about | Primarily uses |
|---|---|---|
| **Super Admin** | Owns the whole deployment: organizations, platform-wide settings, module installation, system health | Dashboard, Modules, System Log, all Activities |
| **Organization/Group Admin** | Manages users and roles within their own scope; doesn't need to think about the platform underneath | Users, Groups, Roles, Permissions, Invitations (scoped to their organization or group) |
| **End User** | Uses whatever modules are enabled for their role; wants their work to be fast and their notifications to make sense | Whichever business modules are enabled, Notifications, Inbox, Chat, Profile |
| **Module Developer** | Builds a business capability (Finance, HR, CRM, ...) without needing to understand core's internals | 07-module-system.md, 17-module-developer-guide.md |
| **Platform Contributor** | Works on core itself: authentication, authorization, the module system, the realtime layer | 10-backend-architecture.md, 11-frontend-architecture.md, 18-testing-strategy.md |

---

# 3. Functional Requirements

The feature list, mapped to where each is actually specified:

| Feature | Requirement | Specified in |
|---|---|---|
| Dashboard | An overview surface; core provides the shell, modules contribute widgets | 11-frontend-architecture.md §2 |
| Users | Platform-wide user directory; an Admin can create, update, deactivate | 04-database-schema.md §6, 08-api-specification.md §10.8 |
| Invitations | An Admin invites a person by email (optionally with a preset group and role); the invitee sets a password and joins; passwords can be reset by email | 05-authentication-design.md §13, 08-api-specification.md §10.6 |
| Groups | Organization subdivisions (Finance, HRD, ...); a Group Admin adds and activates members | 02-domain-model.md, 06-authorization-engine.md §4.2, 08-api-specification.md §10.7 |
| Roles | Named permission sets, scoped platform-wide, per-organization, or per-group | 06-authorization-engine.md §5 |
| Permissions | CRUD-per-module plus field-level read/write, fully customizable by Super Admin (global) or Group Admin (own scope) | 06-authorization-engine.md, 06 §5.3 (authority ceiling), 06 §5.4 (catalog) |
| Modules | Installable, per-organization-enable/disable business capabilities | 07-module-system.md |
| Log Activities | Who did/changed/deleted/opened what, with agent/IP/timestamp; scoped visibility (Super Admin: all, Organization/Group Admin: own scope + own actions separately, User: own only) | 13-audit-logging.md §2, §4 |
| Log System | Infrastructure/operational events, Super Admin only, never user activity | 13-audit-logging.md §5 |
| Settings | Platform, organization, group, user, and module-scoped configuration | 07-module-system.md §10, 04-database-schema.md §9 |
| Notifications | Fire-and-forget, informational, realtime via WebSocket with durable fallback | 09-websocket-protocol.md §6 |
| Inbox | Actionable items expecting a response, distinct from Notifications | 09-websocket-protocol.md §7 |
| Chat | Direct and group conversations between users, with attachments | 09-websocket-protocol.md §8 |

---

# 4. Non-Functional Requirements

## 4.1 Security

Default-deny authorization, no plaintext secrets anywhere, append-only audit trail, defense against the standard threat categories (05-authentication-design.md, 06-authorization-engine.md, 12-security-threat-model.md). Security is treated as a first-class document (12), not a checklist appended to a feature doc.

## 4.2 Performance & Scale ("berskala international")

Stateless API instances (authoritative state in PostgreSQL, shared and rebuildable state in Redis, never in-process memory) so horizontal scaling is a deployment decision, not a rewrite (10-backend-architecture.md §5). WebSocket delivery scales across instances via Redis Pub/Sub (09-websocket-protocol.md §4), and asynchronous work (notifications, email, exports) flows through a transactional outbox to separately-scalable workers (10 §6). Cursor-based pagination for any collection that can grow unbounded (08-api-specification.md §4). Exact latency/throughput targets are deliberately not fixed in this document — they belong wherever a specific deployment's SLA is defined, once real traffic exists to size against (18-testing-strategy.md §9).

## 4.3 Reliability

Graceful shutdown draining in-flight work (10-backend-architecture.md §12), health/readiness endpoints reflecting real dependency state (10 §11, 14-observability.md §7), defined degraded behavior when Redis is unavailable (10 §15), a state change committed atomically with its audit record and event (10 §6), migration rollback verified in CI (19-ci-cd.md §5), flaky tests treated as bugs rather than tolerated noise (18-testing-strategy.md §8).

## 4.4 Internationalization

UTC storage with per-user locale and timezone (04-database-schema.md §6), translation-key-driven UI and API error/notification codes rather than hardcoded strings (08-api-specification.md §8, 11-frontend-architecture.md §10) — a deployment isn't limited to the language it was originally built in.

## 4.5 Extensibility

The entire module system (07, 17) exists to serve this requirement specifically: a developer builds a business capability against `module-sdk`'s stable interfaces without modifying core, and without being able to — Go's own package visibility rules make the boundary structural, not just a convention (10-backend-architecture.md §4).

## 4.6 Maintainability

One `internal/` package per bounded context, consistent domain/application/repository/transport layering everywhere (10-backend-architecture.md §3, §6), one Makefile interface shared by contributors and CI alike (16-makefile-specification.md), a documentation series that stays authoritative because deferred work is tracked in one place rather than scattered (20-roadmap.md) and because development proceeds in milestones whose handovers are written into the repository (21–23).

## 4.7 Technology Baseline

Go (chi, pgx), PostgreSQL 18, Redis 8, Svelte 5 + Vite + TypeScript as a single-page application served same-origin with the API, Docker. The authoritative list and exact versions are in 10 §1.1, 11 §1.1 and `docs/adr/0001-stack.md`.

---

# 5. Success Criteria for v1.0

A deployment satisfies this document's intent when all of the following hold:

```text
[ ] A fresh install can bootstrap its first Super Admin, create its first Organization,
    and invite a Group Admin by email (the invitation arrives, is accepted, and the
    Group Admin can log in)
[ ] That Group Admin can create a Group, add and activate Users, and assign
    Roles with a custom permission set — without Super Admin involvement
    for any of it
[ ] A User's session survives a restart of the API process (session state
    isn't lost because it was never in-process to begin with)
[ ] Every create/update/delete is visible in the Activity Log, correctly
    scoped per viewer's role
[ ] A Super Admin can see System Log entries a Group Admin cannot
[ ] A notification fired by one module reaches a connected user in under a
    second, and is still visible via REST if that user was offline
[ ] A developer unfamiliar with core can follow 17-module-developer-guide.md
    and ship a working module — permissions, a page, a notification —
    without editing anything under backend/internal/
[ ] make setup, make dev, make test, make verify, and make docker-up all work on a clean
    checkout with nothing but Docker, Go, and Node pre-installed
```

---

# 6. Out of Scope for v1.0

The full list, with reasoning, is 20-roadmap.md. In one line each: TOTP MFA and recovery codes, WebAuthn/passkeys, enterprise SSO, a module marketplace, sandboxed modules, S3-compatible storage, and multi-region deployment are all deliberately deferred — not gaps discovered late, but boundaries drawn on purpose so v1.0 has a real, shippable edge. (Password reset and invitations are **in** v1.0.)

---

# 7. Glossary

```text
Platform          One installation/deployment of this software.
Organization       A tenant within a Platform — one company or team. Multi-tenant-
                   ready from the schema up, even where a given deployment only
                   ever has one.
Group               A subdivision within an Organization (Finance, HR, ...).
                   Has its own Group Admin, members, and role assignments.
Role                 A named set of Permissions, assignable at platform,
                   organization, group, resource, or "own" scope.
Permission            A single `<resource>.<action>` grant, including field-level
                   `<resource>.field.<name>.<read|write>` variants.
Scope                  The boundary a Role Assignment or Permission applies within
                   — resolved per-request from session (Organization) and the
                   X-Active-Group-ID header (Group).
Policy                   A conditional allow/deny rule layered on top of a raw
                   permission grant, evaluated server-side against a small,
                   deliberately non-Turing-complete expression grammar.
Module                    An installable business capability. Declares its own
                   permissions, routes, migrations, notifications and events via a
                   manifest; depends only on module-sdk, never on core internals.
Identity                   The output of authentication: which user, which
                   organization, which session, what auth level — nothing about
                   what that identity is allowed to do.
Activity                     A record of a business/user action, scoped and
                   visible per the viewer's role.
System Log                     A record of infrastructure/operational events,
                   visible to the Super Admin only, containing no user activity.
Outbox                          A table written in the same transaction as a state change,
                   from which a worker delivers events at-least-once.
Invitation                       A single-use, emailed token that lets a person join an
                   Organization (optionally with a preset Group and Role).
Milestone                         A unit of the build plan (21) sized to one development
                   session; ends with a handover written into the repository (22).
```

---

# 8. Document Map

```text
00  Errata & Amendments        — the Rev 1.1 change record (everything in it is applied below)
01  Product Requirements       — this document: why, for whom, what "done" means
02  Domain Model                — the entities and their relationships
03  Database ERD                 — how those entities map to tables
04  Database Schema               — the exact SQL, roles and grants
05  Authentication Design          — "who is this?"
06  Authorization Engine            — "what can they do?"
07  Module System                    — how the platform is extended
08  API Specification                 — the REST contract, shared conventions
09  WebSocket / Realtime                — notifications, inbox, chat
10  Backend Architecture                 — Go package structure, transactions, outbox, conventions
11  Frontend Architecture                 — Svelte SPA structure and conventions
12  Security Threat Model                  — who's attacking what, and how it's handled
13  Audit Logging                            — activities, security events, system logs
14  Observability                             — logs, metrics, traces
15  Docker & Native Development                — running it locally
16  Makefile Specification                       — the one interface everyone uses
17  Module Developer Guide                        — building your first module, step by step
18  Testing Strategy                                — how, and how much, to test
19  CI/CD                                            — what has to pass before merge
20  Roadmap                                            — what's deliberately not here yet
21  Milestone Plan                                      — the order in which v1.0 is built
22  Handover Protocol                                    — how one session hands over to the next
23  Prompt Library                                        — the prompts that drive each session
```
