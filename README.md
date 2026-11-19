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

**Otorisasi (M04, ADR-0022):** katalog permission core dan empat role sistem ada di kode dan disinkronkan saat start; setiap rute terlindungi melewati `Authenticate → CSRF → RequireAuth → Scope → Authorize(action)`;
hak efektif di-cache per versi di Redis (gagal-aman, dicabut seketika lewat invalidasi eksplisit) dan policy hanya membatasi (deny menang, galat evaluasi = tolak); authority ceiling mencegah admin memberi lebih dari yang ia pegang.
15 rute di `/api/v1` (role, permission, role-assignment, policy, `permissions/simulate`); mencabut assignment platform mencabut semua sesi user itu. Rincian, batas yang diketahui, dan utang (mis. nama definisi OpenAPI) ada di `docs/handover/HANDOVER-M04.md`. Tidak ada jaminan keamanan absolut.

Port host bentrok dengan proyek lain? Jangan edit `.env` manual: `bash scripts/env-ports.sh --force POSTGRES=25000 REDIS=25001 API=25002 WEB=25003 SMTP=25004 MAILUI=25005`
(ADR-0021; port di dalam jaringan compose tidak berubah).

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
| `make verify` | **Gerbang tunggal**: lint + `generate-check` + tes + migrate-roundtrip + build + smoke pada stack Docker sementara. Harus hijau sebelum merge. |
| `make lint` / `make test` | lint Go + frontend + pemeriksaan struktural / tes backend (`-race`, testcontainers) + frontend |
| `make generate` / `make generate-check` | regenerasi OpenAPI (`docs/api/swagger.json`), kode sqlc, SDK TS dan registri modul (bertahap per milestone) / gagal bila keluaran generator di disk basi |
| `make smoke` | semua `scripts/smoke/m*.sh` terhadap stack yang berjalan (`M00`: fondasi; `M02`: kernel lewat endpoint echo; `M03`: login/logout, ganti password, kunci akun; `M04`: otorisasi ujung-ke-ujung dengan fixture `psql`). Membuat user Super Admin smoke, jadi hanya terhadap localhost; butuh Go, DB terjangkau dari host, dan `docker compose exec redis` |
| `make migrate-up` / `migrate-down` / `migrate-version` | terapkan / mundurkan satu / tampilkan versi migrasi inti (`MIGRATION_DATABASE_URL`) |
| `make db-seed` | pastikan baris platform ada (idempoten); WAJIB sebelum `make bootstrap-admin` (`make setup`/`make dev` hanya menjalankan migrasi) |
| `make bootstrap-admin EMAIL=…` | buat Super Admin pertama (idempoten, tidak pernah menimpa password): `NAME=…`, `PASSWORD_STDIN=1` (baca password dari stdin; tanpa itu password acak dicetak SEKALI) |
| `make migrate-roundtrip` / `make db-test` | up→down→up pada PostgreSQL sementara / tes constraint, trigger, cascade, grant skema (butuh Docker). Skema: [docs/erd.md](docs/erd.md) |
| `make infra-up` | hanya datastore, untuk pengembangan native |
| `make clean` | hapus artefak **dan volume data** (destruktif) |

Endpoint khusus pengembangan (hanya saat `ENVIRONMENT` = `development`/`test`; tidak pernah ada di staging/production): `POST` dan
`GET /api/v1/_kernel/echo` (+ `/items`) memperagakan envelope, idempotency, rate limit, dan pagination cursor; lihat `scripts/smoke/m02.sh`.
Metrik Prometheus disajikan di **port terpisah** (bukan port API) bila `METRICS_PORT` (API) / `WORKER_METRICS_PORT` (worker) diisi;
`.env.example` memakai 9090/9091 dan Compose tidak mem-publish-nya. Kecepatan `make test`: `make test-backend GO_TEST_PARALLEL=N`
membatasi jumlah paket tes yang menyalakan container bersamaan (bawaan 3).

`make verify` memakai compose project terpisah (`platform-verify`) sehingga tidak menghapus data pengembanganmu, tetapi
port host yang dipublikasikan compose (bawaan 5432/6379/8080/5173/1025/8025, atau yang kamu setel di `.env`) harus bebas (hentikan `make dev`/`make docker-up` dulu).
Di Docker Desktop bermemori kecil, tes integrasi bisa flake (`Reaper handshake failed`, `connection refused`): jalankan paket itu sendirian dan ulangi, jangan ubah kode.
Build pertama dengan cache dingin butuh beberapa menit; jangan hentikan saat tampak diam di `menunggu: api`.

## Struktur

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
