# Backend Architecture
## Open Source Modular Application Platform v1.0

**Document:** 10-backend-architecture.md
**Status:** Baseline (Rev 1.1)
**Previous:** 09-websocket-protocol.md
**Next:** 11-frontend-architecture.md

---

# 1. Purpose

This document makes the package layout and layering concrete enough to start writing Go code from: exactly where a module's dependencies can come from, how a request is wired from socket to database and back, how a state change, its audit record and its event stay consistent, and the conventions (config, errors, context, shutdown) that keep every `internal/` package looking like it was written by the same team.

## 1.1 Technology baseline

| Area | Choice |
|---|---|
| Language | Go (latest stable; `go` and `toolchain` pinned in `go.mod`) |
| HTTP | chi v5 (`github.com/go-chi/chi/v5`) |
| PostgreSQL driver | pgx v5 + `pgxpool` (`github.com/jackc/pgx/v5`) — not `database/sql` |
| SQL | hand-written SQL, typed access generated with **sqlc** (pgx/v5 target); no ORM |
| Migrations | golang-migrate as a library (`cmd/migrate`, `pgx5://` URLs) |
| Redis | go-redis v9 |
| WebSocket | `github.com/coder/websocket` |
| Logging | `log/slog` (JSON), wrapped by `pkg/logger` |
| Config | `github.com/caarlos0/env/v11` |
| Validation | `github.com/go-playground/validator/v10` |
| IDs | `github.com/google/uuid` (`NewV7`) via `pkg/id` |
| Telemetry | OpenTelemetry + Prometheus |
| Tests | testify, testcontainers-go |

PostgreSQL 18 and Redis 8 are the target data stores. Exact patch versions are pinned in `docs/adr/0001-stack.md`.

---

# 2. Repository Layout, in Full

```text
platform/
├── apps/
│   ├── web/                  # Svelte SPA — 11-frontend-architecture.md
│   ├── docs/
│   └── playground/
├── backend/
│   ├── cmd/
│   │   ├── api/               # HTTP + WebSocket server entrypoint (+ modules_gen.go, generated)
│   │   ├── worker/            # background job runner, outbox relay (+ modules_gen.go, generated)
│   │   └── migrate/           # migration runner (golang-migrate library)
│   ├── internal/               # core's private implementation — see §3
│   ├── pkg/                    # shared utilities — see §7
│   └── migrations/             # 04-database-schema.md
├── packages/
│   ├── ts-sdk/                 # generated from the OpenAPI spec, 08 §9
│   ├── ui/                     # shared Svelte components
│   └── module-sdk/             # the ONLY package a module may import from core — see §4
├── modules/                     # business modules — 07-module-system.md
├── deploy/
│   ├── docker/                  # Dockerfiles (build context is the repository ROOT)
│   └── db/init/                 # role creation for PostgreSQL (04 §13)
├── scripts/
├── docs/
├── Makefile
├── docker-compose.yml
└── go.mod                       # one Go module for the whole repository, at the root
```

One `go.mod` at the **repository root**. `backend/`, `modules/`, and `packages/module-sdk/` are all part of the same Go module — this single decision is what makes §4 below work at all. Consequences: `go test ./...` from the root covers core, `module-sdk` and every module; Docker builds use the repository root as the build context (15 §3) because the Dockerfile must see `go.mod`, `packages/module-sdk` and `modules/`.

---

# 3. `internal/` Package Map

```text
backend/internal/
├── kernel/        transactions, outbox, event bus, HTTP toolkit (envelope, errors, pagination,
│                  idempotency, rate limit), database pools — §6, §13
├── auth/          identity, sessions, credentials — 05-authentication-design.md
├── identity/       the User aggregate itself
├── organization/   organizations, organization_memberships
├── invitation/     invitations (05 §13.3)
├── group/          groups, group hierarchy
├── membership/      group_memberships
├── role/           roles
├── permission/      permissions, the catalog — 06 §5.4
├── policy/          policy expressions
├── authorization/   composes role+permission+policy+scope into a decision — 06 §13
├── module/          module lifecycle, installation — 07-module-system.md
├── notification/
├── inbox/
├── chat/
├── storage/          attachment storage interface + local driver
├── email/            SMTP sending, templates per locale (used by the worker)
├── websocket/        the Hub — 09-websocket-protocol.md
├── audit/            activities — 13-audit-logging.md
├── systemlog/
├── settings/
└── platform/         the single platforms row, installation-level config
```

Every one of these follows the same internal shape (05 §19 is the template): `domain/`, `application/`, `repository/`, and (where it's directly HTTP-reachable) `transport/http/`.

---

# 4. Where Module Contracts Actually Live — and Why It Isn't `internal/`

This is worth spelling out because it's easy to get wrong in a way that compiles right up until someone tries to build an out-of-tree module.

Go's `internal/` visibility rule: a package under `.../internal/...` is importable only by code rooted at the **parent** of that `internal` directory. `backend/internal/role`'s parent is `backend/`. `modules/finance/` is a **sibling** of `backend/`, not a descendant of it — so `modules/finance` **cannot import `backend/internal/anything`**, full stop, enforced by the compiler, not by a code-review checklist.

This isn't a bug to work around — it's the mechanism that makes "explicit boundaries" real instead of aspirational. The fix is what `packages/module-sdk/` is for: it sits outside every `internal/` path, so anything in the repo can import it.

```go
// packages/module-sdk/module.go
package modulesdk

type CoreDeps struct {
    Authz         Authorizer
    Audit         AuditRecorder
    Events        EventBus
    Identity      IdentityLookup
    Notifications NotificationPublisher
    Tx            TxManager   // run a function inside one transaction (§6)
    DB            DB          // the runtime (app_user) pool, as an interface
    Settings      SettingsReader
}

type Authorizer interface {
    Can(ctx context.Context, check Check) (Decision, error)
}
type AuditRecorder interface {
    Record(ctx context.Context, entry ActivityEntry)
}
type EventBus interface {
    Publish(ctx context.Context, event Event)
    Subscribe(eventName string, handler EventHandler)
}
type TxManager interface {
    WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}
type DB interface {
    Exec(ctx context.Context, sql string, args ...any) (CommandTag, error)
    Query(ctx context.Context, sql string, args ...any) (Rows, error)
    QueryRow(ctx context.Context, sql string, args ...any) Row
}
```

`DB` is a small interface satisfied by both the pgx pool and a pgx transaction, so module code written against it is transaction-agnostic: inside `WithinTx` the context carries the transaction and the same calls run in it.

`module-sdk` defines interfaces only — no implementation. `backend/internal/module` builds the real `CoreDeps` at startup, backed by the actual `internal/authorization`, `internal/audit`, etc., and hands it to each module's `RegisterRoutes(r, deps)`. A module imports `module-sdk`; it never imports, and structurally cannot import, `backend/internal/*`.

```text
┌────────────────────┐        ┌──────────────────────┐
│   backend/internal │◄───── implements ─────│   packages/module-sdk │
│   (concrete, private) │                        │   (interfaces, public) │
└────────────────────┘                        └───────────┬───────────┘
                                                              │ imported by
                                                              ▼
                                                    ┌──────────────────┐
                                                    │   modules/finance  │
                                                    └──────────────────┘
```

(`backend/cmd/api` and `backend/cmd/worker` *do* import the modules — through the generated `modules_gen.go`, 07 §5.2 — which is allowed: importing outward from `backend/` is fine; only the reverse direction is blocked.)

## 4.1 In-tree today, out-of-tree later

For v1.0, modules live inside this same repository (`modules/`) and this same `go.mod` — "out-of-tree" means "a different top-level directory that can't see `internal/`," not "a separate repository yet." Making `module-sdk` a genuinely independent, versioned, `go get`-able module (its own `go.mod`, its own release tags) so a third party can build a module in a completely separate repository is real future work — deliberately deferred alongside the module marketplace (20 §5), not something this document pretends is already true.

---

# 5. Three Binaries, One Codebase

```go
// cmd/api/main.go      — HTTP + WebSocket
// cmd/worker/main.go   — outbox relay + event handlers (07 §8), scheduled/queued jobs,
//                        email (05 §13), notification fallback (09 §6), exports (13 §8),
//                        retention + anonymization (13 §6–7)
// cmd/migrate/main.go  — applies migrations (04 §15); runs with the app_migrator role
```

`api` and `worker` import the same `internal/` packages and the same registered modules; they differ only in what they run at startup — `cmd/api` mounts routers and the WebSocket hub, `cmd/worker` starts the outbox relay, a job scheduler and event subscribers. Keeping them as separate binaries from day one (rather than one binary with a `--mode` flag) makes it trivial to scale them independently later without a rewrite. They do **not** share memory: anything one must tell the other goes through PostgreSQL (the outbox) or Redis (Pub/Sub), never a package-level variable.

The worker can run several instances: the outbox relay reads with `FOR UPDATE SKIP LOCKED`, and scheduled jobs take a short Redis lock so a cron-style job fires once per tick.

---

# 6. Request Layering, Transactions, and the Outbox

```text
HTTP/WebSocket Handler   (transport/http, transport/websocket)
        │  — decodes request, calls one application method, encodes response
        ▼
Application Service       (application/)
        │  — orchestrates a use case: Authorizer, Domain, Repository, Audit, Events — inside ONE transaction
        ▼
Domain                     (domain/)
        │  — entities, invariants, no I/O
        ▼
Repository Interface       (domain/ defines it, repository/ implements it)
        │
        ▼
Infrastructure              (postgres, redis clients)
```

## 6.1 One transaction: state + audit + event

A concrete trace, approving an invoice:

```text
1. Handler decodes {invoice_id} from the URL (uuid.Parse — a bad id is validation_failed, never a panic),
   calls application.ApproveInvoice(ctx, identity, scope, invoiceID)
2. Application service:
     a. authz.Can(ctx, Check{..., Action: "invoice.approve"})   → deny short-circuits here
        (the denial is audited in its OWN transaction: the main one never started / rolls back)
     b. tx.WithinTx(ctx, func(ctx) error {
          i.   repository.FindByID(ctx, orgID, invoiceID)         → domain object
          ii.  invoice.Approve()                                   → domain invariant (e.g. can't approve a closed invoice)
          iii. repository.Save(ctx, invoice)
          iv.  audit.Record(ctx, ActivityEntry{Action: "invoice.approved", ...})
          v.   events.Publish(ctx, Event{Name: "finance.invoice.approved", ...})   → writes to outbox_events
        })
3. After commit: in-process handlers run (e.g. authorization cache version bump); the worker
   relay will deliver the outbox event.
4. Handler encodes the result into the standard envelope (08 §3)
```

```go
type TxManager interface {
    WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}
```

Rules:

- The transaction travels in the `context`; repositories use it when present and the pool otherwise — a repository method has the same signature either way.
- `Save`, `audit.Record` and `events.Publish` **commit together or not at all** (02 §34 invariant 18). If the audit write fails on a sensitive action, the operation fails (fail-closed); it is never silently dropped.
- Slow or fallible side effects — email, WebSocket push, Redis cache bumps — never run inside the transaction. They happen after commit or are driven by the outbox.
- Nested `WithinTx` joins the outer transaction (savepoints are not used implicitly).

## 6.2 The outbox

`events.Publish` inside a transaction inserts a row in `outbox_events` (04 §12). The **worker relay** loop:

```text
loop:
  BEGIN
  SELECT … FROM outbox_events
    WHERE status = 'pending' AND available_at <= now()
    ORDER BY available_at, created_at LIMIT n FOR UPDATE SKIP LOCKED
  for each event: run the registered handlers (module and core), each idempotent on event.ID
  success → status = 'processed', processed_at = now()
  failure → attempts++, available_at = now() + backoff (exponential, capped); after max attempts → status = 'dead' + system log error
  COMMIT
```

Delivery is **at-least-once**; handlers deduplicate by event id (for example a unique constraint on `(event_id, handler)` in their own side-effect table, or a natural idempotency such as "upsert notification by (event_id, recipient)"). Processed rows are purged by the maintenance job after a retention window (13 §6). The queue depth and oldest-pending age are exported as metrics (14 §5).

The application service is the **only** layer that talks to `Authorizer`, `AuditRecorder`, and `EventBus` — domain objects stay pure (no context, no I/O, easy to unit test with plain values), and handlers stay thin (no business logic, easy to swap REST for WebSocket or gRPC later without touching a single rule).

---

# 7. `pkg/` — Shared, Not Private

```text
backend/pkg/
├── logger/      slog JSON logging, one shared logger instance/interface; .SystemLog() sink (14 §3)
├── validator/    input validation (struct tags → rejected fields, used at the transport boundary)
├── security/     Argon2id (05 §4), CSRF token generation/verification, constant-time comparison,
│                 random token generation + SHA-256 token hashing (sessions, reset tokens, invitations)
├── id/            UUIDv7 generation (04 §2.1) — the one place `NewID()` lives
├── config/        env parsing + fail-fast validation (§8)
└── telemetry/     OpenTelemetry setup, /metrics handler, span helpers (14 §10)
```

Unlike `internal/`, `pkg/` has no Go-enforced restriction on who can import it — but the convention is: `pkg/` holds cross-cutting technical utilities with no business meaning (hashing, logging, ID generation), never domain logic. If a module needs `pkg/id` to generate its own primary keys, that's fine and expected; if a module needs something that looks like business logic, that belongs behind a `module-sdk` interface instead (§4), not in `pkg/`.

---

# 8. Configuration

Environment variables only for v1.0 — no config file, no hot-reload. Validated **once**, at process startup, in `cmd/api`, `cmd/worker` and `cmd/migrate`:

```go
type Config struct {
    // data stores
    DatabaseURL            string `env:"DATABASE_URL,required"`             // role app_user
    MaintenanceDatabaseURL string `env:"MAINTENANCE_DATABASE_URL"`          // role app_maintenance (worker only)
    MigrationDatabaseURL   string `env:"MIGRATION_DATABASE_URL"`            // role app_migrator (cmd/migrate, module lifecycle)
    RedisURL               string `env:"REDIS_URL,required"`

    // security
    SessionSecret   string `env:"SESSION_SECRET,required"`   // CSRF token key derivation; ≥ 32 bytes
    CookieName      string `env:"COOKIE_NAME" envDefault:"platform_session"`   // production: __Host-platform_session
    CookieSecure    bool   `env:"COOKIE_SECURE" envDefault:"true"`
    AllowedOrigins  []string `env:"ALLOWED_ORIGINS,required"`  // WebSocket Origin check and Origin verification
    TrustedProxies  []string `env:"TRUSTED_PROXIES"`           // CIDRs whose X-Forwarded-* are trusted
    MFAEncryptionKey string `env:"MFA_ENCRYPTION_KEY"`         // Phase 2

    // server
    Port        int    `env:"PORT" envDefault:"8080"`
    Environment string `env:"ENVIRONMENT" envDefault:"development"`
    LogLevel    string `env:"LOG_LEVEL" envDefault:"info"`
    PublicBaseURL string `env:"PUBLIC_BASE_URL,required"`    // used in email links

    // email
    SMTPHost string `env:"SMTP_HOST"`
    SMTPPort int    `env:"SMTP_PORT" envDefault:"1025"`
    SMTPUser string `env:"SMTP_USER"`
    SMTPPass string `env:"SMTP_PASS"`
    SMTPFrom string `env:"SMTP_FROM"`

    // storage and telemetry
    StoragePath string `env:"STORAGE_PATH" envDefault:"./data/uploads"`
    OTLPEndpoint string `env:"OTEL_EXPORTER_OTLP_ENDPOINT"`
}
```

A missing or invalid required value fails the process **before** it binds a port or accepts a connection — never a partially-configured process serving traffic with a nil dependency waiting to panic on the first request that needs it. Additional cross-field rules: `CookieSecure=false` or a non-HTTPS `PublicBaseURL` is rejected when `ENVIRONMENT=production`; `MaintenanceDatabaseURL` is required by the worker; `SessionSecret` shorter than 32 bytes is rejected. Configuration values are never logged.

---

# 9. Error Handling

Domain and application layers return **sentinel or wrapped Go errors** — never an HTTP status code, never the error envelope shape from 08 §3. Only the transport layer knows about HTTP:

```go
var ErrInvoiceClosed = errors.New("invoice is closed")

// application layer
if invoice.Status == "closed" {
    return fmt.Errorf("approve invoice %s: %w", invoice.ID, ErrInvoiceClosed)
}

// transport layer — the ONLY place that maps a domain error to an API error code
switch {
case errors.Is(err, ErrInvoiceClosed):
    writeError(w, http.StatusConflict, "finance.invoice_closed", err)
case errors.Is(err, authorization.ErrDenied):
    writeError(w, http.StatusForbidden, "permission_denied", err)
default:
    logger.Error(ctx, "unhandled error", err) // full detail to logs + system_logs (13 §5)
    writeError(w, http.StatusInternalServerError, "internal_error", nil) // never the raw error to the client
}
```

This is what makes "never return internal stack traces to clients" (05 §22) mechanically true rather than a habit someone can forget under deadline pressure — the `default` case is the only path that reaches the client for anything unrecognized, and it never carries the original error's text. A `Recoverer` middleware turns a panic into the same `internal_error`.

---

# 10. Context Propagation

Exactly one way to carry request-scoped values, using unexported key types so two packages can never accidentally collide on the same string key:

```go
type ctxKey int

const (
    ctxKeyIdentity ctxKey = iota
    ctxKeyScope
    ctxKeyRequestID
    ctxKeyTraceID
    ctxKeyTx
)

func IdentityFromContext(ctx context.Context) (Identity, bool) { ... }
func WithIdentity(ctx context.Context, id Identity) context.Context { ... }
```

Populated once, by the Authentication and Authorization middleware (05 §9), and read everywhere downstream via the accessor functions above — never `ctx.Value("identity")` with a bare string. The worker populates `request_id`/`trace_id` from the outbox event so an activity written by a worker job is still correlated to the originating request.

---

# 11. Health & Readiness

```text
GET /health        — process is up (no dependency checks; for a load balancer's basic liveness probe)
GET /health/live    — same as above, Kubernetes-style naming
GET /health/ready   — PostgreSQL reachable, Redis reachable, and every enabled module's Health()
                      (07 §5) reports healthy — each check bounded by a 1–2 s timeout, no heavy queries
```

`/health/ready` failing takes an instance out of a load balancer's rotation without killing it — a slow database failover shouldn't restart every API pod at once. By default a single unhealthy module degrades its `module_health_status` metric and is reported in the readiness body without failing readiness; a `strict` deployment policy fails readiness instead (14 §7). Health endpoints are unauthenticated and reveal no versions or hostnames.

---

# 12. Graceful Shutdown

```text
SIGTERM received
     │
     ▼
Stop accepting new HTTP connections
     │
     ▼
Let in-flight HTTP requests finish (bounded timeout)
     │
     ▼
Close WebSocket connections with a clean close frame (client reconnect logic in
09 §2.3 already expects this, not a dropped connection)
     │
     ▼
cmd/worker: let the current job and outbox batch finish (bounded timeout), do not pick up a new one
     │
     ▼
Close database and Redis connections
     │
     ▼
Exit
```

A hard-killed process mid-request is how a WebSocket client ends up thinking it's connected when it isn't, and how a worker job ends up half-applied — the bounded-timeout drain is what turns "restart the API" into a non-event for anyone using it. (An outbox batch interrupted anyway is safe: its transaction rolls back and the events stay `pending`.)

---

# 13. Database Connections and Tenant Scoping

`pgxpool`, **one pool per role per binary**, sized from configuration rather than hardcoded:

| Pool | Role | Used by |
|---|---|---|
| app | `app_user` | API and worker, all normal traffic |
| maintenance | `app_maintenance` | worker only — retention, anonymization (13 §6–7) |
| migration | `app_migrator` | `cmd/migrate` and the module lifecycle only (07 §5.3); never held by request handlers |

- a statement timeout on every query — a single slow query must not be able to exhaust the pool and take the whole instance down with it
- every query built with placeholders (`$1`, `$2`, ...) — string-concatenated SQL is a rejected pattern in review, full stop
- every tenant-scoped query required to filter by `organization_id`, enforced by structure rather than memory:
  - repository methods take `organizationID` as an explicit parameter; there is no method that can read or write a tenant row without it;
  - the rare genuinely cross-tenant operation (a Super Admin listing all organizations or all activities) is a **separately named method** (`…Platform…`/`…Unscoped…`) that requires a `Decision` with `Level == platform` as an argument, so it cannot be reached from a tenant-level code path (06 §3, 12 §6).

---

# 14. Testing Conventions

```text
Domain           — plain unit tests, no I/O, no mocks needed (pure functions/invariants)
Application       — unit tests with a mocked Authorizer, AuditRecorder, EventBus, TxManager and Repository
Repository        — integration tests against a real, ephemeral PostgreSQL 18 (testcontainers) —
                    never mocked, since the whole point is catching a wrong query, a missed
                    index, or a constraint that doesn't fire the way it's assumed to
Kernel            — transaction atomicity (rollback leaves no audit/outbox row), outbox relay
                    (retry, dead-letter, idempotent re-delivery) against real PostgreSQL + Redis
Transport/HTTP    — table-driven request/response tests through the full middleware chain
```

Full detail (negative-test catalog, coverage expectations, CI wiring) is 18-testing-strategy.md; this section only fixes which layer gets which kind of test, so a module author writing their first test knows where to start.

---

# 15. Dependency Failure Behavior

| Dependency down | Behavior |
|---|---|
| Redis | sessions: PostgreSQL fallback (05 §7.2); authorization: compute from PostgreSQL (06 §8.2); rate limiting: conservative per-instance fallback on sensitive endpoints; idempotency-required endpoints: fail closed (503); WebSocket fan-out: degraded — clients use REST; readiness: not ready |
| PostgreSQL | requests that need it fail with 503; readiness: not ready; nothing is served from stale memory |
| SMTP | the email job retries with backoff through the outbox; the request that triggered it is unaffected |
| Worker | outbox rows accumulate (`outbox_pending` metric/alert); user-facing requests keep working; notifications and emails are delayed, never lost |

---

# 16. Critical Rules

```text
1.  A module imports packages/module-sdk. A module never imports backend/internal/*
    — and, thanks to Go's own visibility rules, it structurally cannot.
2.  Domain and application code never returns an HTTP status code or the API
    error envelope — only the transport layer knows what HTTP is.
3.  Every context value is read/written through a typed accessor function,
    never a bare string key.
4.  Every tenant-scoped repository query filters by organization_id; a cross-tenant
    method is separately named and demands a platform-level Decision.
5.  Configuration is validated once at startup; a missing required value
    stops the process before it binds a port.
6.  cmd/api and cmd/worker both drain in-flight work on shutdown; neither
    exits mid-request or mid-job.
7.  A state change, its audit record and its outbox event are one transaction.
8.  Request handlers never hold DDL rights; only cmd/migrate and the module lifecycle use the migrator role.
9.  Event handlers are idempotent — delivery is at-least-once.
```
