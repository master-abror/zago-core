# ZaGo Core

**Build the business, not the boilerplate.**

An open-source, multi-tenant application platform that gives you authentication, organizations, fine-grained permissions, audit trails and realtime messaging out of the box — so a new product starts as a *module*, not as another rewrite of login and user management.

![Go](https://img.shields.io/badge/Go-backend-00ADD8?logo=go&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-18-4169E1?logo=postgresql&logoColor=white)
![Redis](https://img.shields.io/badge/Redis-8-DC382D?logo=redis&logoColor=white)
![Svelte](https://img.shields.io/badge/Svelte-5-FF3E00?logo=svelte&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-ready-2496ED?logo=docker&logoColor=white)

> ZaGo Core is under active development toward v1.0. The design documents in [`docs/`](docs/) describe the complete system.

---

## Why ZaGo Core?

Almost every internal tool, admin panel or B2B SaaS needs the same foundation before any real business logic can begin: *who can log in, how are they organized, what are they allowed to do, and what did they do?* Teams rebuild this again and again — usually without proper audit trails, with permissions bolted on as an afterthought.

ZaGo Core builds that foundation **once, to a production-grade standard**:

- **Security is the default posture.** Everything is denied unless a rule explicitly allows it.
- **Multi-tenant from the schema up.** Tenant isolation is enforced by the database and by code structure, not by developer memory.
- **Extensible without forking.** Business capabilities (Finance, HR, CRM, …) are modules that depend only on a small, stable SDK.
- **Operable from day one.** Health checks, metrics, tracing, structured logs, graceful shutdown and one-command verification.

## What you get

### Identity & organizations
- Platform → Organizations → Groups (hierarchical, acyclic, bounded depth) → Members
- Email invitations with a preset group and role, and password reset by email
- Users can belong to several organizations and switch between them; groups can be switched per request

### Authentication
- Opaque 256-bit session tokens — only a hash is ever stored, never the token
- Argon2id password hashing with rehash-on-login and timing-equalized responses (no account enumeration)
- `HttpOnly`, `SameSite=Lax` cookies (`__Host-` prefixed in production) and session-bound CSRF tokens
- Temporary account locking with progressive delay, plus per-IP throttling
- Sessions survive an API restart, and survive Redis going down (PostgreSQL is the authority)
- Revoked sessions close live WebSocket connections immediately

### Authorization
- Roles, permissions and role assignments at **platform, organization, group, resource and own** scope
- **Field-level permissions**: a restricted field is simply absent from REST responses, WebSocket events and exports
- **Policies**: conditional allow/deny rules in a small, sandboxed expression language (hand-written parser, no `eval`, fuzz-tested)
- **Authority ceiling**: an admin can never grant more than they hold themselves
- **Instant revocation** through versioned cache keys — no waiting for a TTL
- A **permission simulator** that runs through the exact production code path

### Modules
- Modules declare their permissions, routes, migrations, events, notification templates, jobs and UI in a single manifest
- Enable or disable a module **per organization**; disabling preserves data
- Each module has its own migrations and version table, and its own table prefix
- A module can only import `packages/module-sdk` — Go's `internal/` visibility rule enforces the boundary at compile time

### Realtime
- WebSocket delivery for **notifications**, an actionable **inbox**, and **chat** (direct and group, with attachments)
- Horizontally scalable through Redis Pub/Sub
- Everything is persisted first; offline users catch up over REST and reconnecting clients resync

### Audit & observability
- Three separate logs for three audiences: **activities** (business actions, role-scoped visibility), **security events** (auth-specific), **system logs** (Super Admin only)
- Append-only by database privilege: the runtime role cannot `UPDATE` or `DELETE` audit rows
- State change, audit record and event are committed **atomically** through a transactional outbox
- Structured JSON logs, Prometheus metrics, OpenTelemetry traces and honest readiness checks

### Developer experience
- REST API under `/api/v1` with a consistent envelope, signed cursor pagination, idempotency keys and rate limiting
- OpenAPI generated from code and turned into a typed TypeScript SDK
- `make` is the single interface for contributors and CI alike
- Translation-key-driven UI and error codes, UTC storage with per-user locale and timezone

## How it fits together

```text
Browser (Svelte 5 SPA)
   │  same origin: /, /api, /ws
   ▼
Reverse proxy ───────────────┐
   │                         │
   ▼                         ▼
 cmd/api                 cmd/worker
 HTTP + WebSocket        outbox relay, jobs, email,
 authn → scope → authz   exports, retention
   │                         │
   └───────┬─────────────────┘
           ▼
   PostgreSQL 18  ◄── authoritative state, outbox, audit
   Redis 8        ◄── sessions, cache, rate limits, Pub/Sub (rebuildable)
```

- **Layering:** transport → application → domain → repository. Only application services talk to the authorizer, audit recorder and event bus.
- **Three binaries, one codebase:** `api`, `worker` and `migrate` share code but never memory.
- **Three database roles:** `app_migrator` (DDL), `app_user` (runtime), `app_maintenance` (retention and anonymization only).
- **Dependency failures degrade, they don't fail open:** Redis down means PostgreSQL fallback; PostgreSQL down means `503`, never stale answers.

## Build a module

A module is a manifest plus a small amount of Go and Svelte:

```yaml
id: announcements
name: Announcements
version: 1.0.0

permissions:
  - code: announcement.read
    resource: announcement
    action: read
  - code: announcement.publish
    resource: announcement
    action: publish

events:
  publishes: [announcements.announcement.published]

notifications:
  - event: announcements.announcement.published
    type: announcements.published
    recipients: payload.recipient_ids
```

```go
func (m *Module) RegisterRoutes(r modulesdk.Router, deps modulesdk.CoreDeps) {
    r.Get("/announcements", m.list, modulesdk.Require("announcement.read"))
    r.Post("/announcements/{id}/publish", m.publish,
        modulesdk.Require("announcement.publish"), modulesdk.Idempotent())
}
```

Every route must declare the permission it needs, or registration fails. See the [Module Developer Guide](docs/17-module-developer-guide.md) for the full walkthrough.

## Tech stack

| Area | Choice |
|---|---|
| Language | Go (chi, pgx v5, sqlc, golang-migrate) |
| Database | PostgreSQL 18 (native `uuidv7()`) |
| Cache / messaging | Redis 8 (basic commands only, so it stays Valkey-compatible) |
| Realtime | `coder/websocket` |
| Frontend | Svelte 5 (runes), Vite, TypeScript strict — SPA |
| API contract | OpenAPI via swag → generated TypeScript SDK |
| Observability | `log/slog`, OpenTelemetry, Prometheus |
| Testing | testify, testcontainers-go (real PostgreSQL and Redis, no database mocks), Vitest, Playwright |
| Packaging | Docker, Docker Compose, distroless production images |

## Quick start

**Prerequisites:** Docker, Go, and Node.js (LTS).

```bash
git clone https://github.com/master-abror/zago-core.git
cd zago-core

make setup     # install tools and dependencies
make dev       # start PostgreSQL, Redis, Mailpit, API, worker and web with hot reload
```

Create your first Super Admin:

```bash
make bootstrap-admin EMAIL=you@example.org
```

The password is read from stdin or generated once and printed — it is never read from a committed file.

Other useful targets:

```bash
make verify         # lint + tests + migration round-trip + build + smoke
make docker-up      # run the full stack in containers
make migrate-up     # apply database migrations
make module-create NAME=finance   # scaffold a new module
```

## Repository layout

```text
backend/       Go: cmd/{api,worker,migrate}, internal/*, pkg/*, migrations
packages/      module-sdk (the only package modules may import), ts-sdk, ui
modules/       business modules
apps/web/      Svelte SPA
deploy/        Dockerfiles and database init scripts
docs/          the full design series, ADRs and API spec
```

## Documentation

The `docs/` folder is the specification, and it is meant to be read in order:

| | |
|---|---|
| [01 Product Requirements](docs/01-product-requirements.md) | Why, for whom, and what "done" means |
| [02 Domain Model](docs/02-domain-model.md) | Entities, relationships and invariants |
| [04 Database Schema](docs/04-database-schema.md) | Exact SQL, roles and grants |
| [05 Authentication](docs/05-authentication-design.md) | "Who is this?" |
| [06 Authorization](docs/06-authorization-engine.md) | "What can they do?" |
| [07 Module System](docs/07-module-system.md) | How the platform is extended |
| [08 API Specification](docs/08-api-specification.md) | REST conventions and catalog |
| [09 Realtime](docs/09-websocket-protocol.md) | WebSocket, notifications, inbox, chat |
| [10 Backend Architecture](docs/10-backend-architecture.md) | Packages, transactions, outbox |
| [12 Threat Model](docs/12-security-threat-model.md) | Who attacks what, and how it's handled |
| [13 Audit Logging](docs/13-audit-logging.md) | Activities, security events, system logs |

Architecture decisions are recorded as short ADRs in [`docs/adr/`](docs/adr/); where an ADR and a document disagree, the newer ADR wins.

## Deliberately out of scope for v1.0

These are boundaries drawn on purpose, so v1.0 has a real, shippable edge: TOTP MFA and recovery codes, WebAuthn/passkeys, enterprise SSO (OIDC), a module marketplace, sandboxed modules, S3-compatible storage and multi-region deployment. See the [roadmap](docs/20-roadmap.md) for the reasoning.

A note on honesty: modules run in the same process as core. Treat installing a third-party module like merging an external pull request into production — review it first. The [threat model](docs/12-security-threat-model.md) states this trade-off plainly.

## Contributing

Contributions are welcome. Start with [`docs/10-backend-architecture.md`](docs/10-backend-architecture.md) and [`docs/18-testing-strategy.md`](docs/18-testing-strategy.md). A few ground rules:

- Write the negative tests first for anything touching authentication, authorization or tenant isolation.
- Use Conventional Commits.
- Record any deviation from the docs as an ADR instead of drifting silently.
- Run `make verify` before opening a pull request.

## License

License to be announced.
