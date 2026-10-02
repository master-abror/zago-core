# Module System
## Open Source Modular Application Platform v1.0

**Document:** 07-module-system.md
**Status:** Baseline (Rev 1.1)
**Previous:** 06-authorization-engine.md
**Next:** 08-api-specification.md
**Builds on:** 02-domain-model.md (§18–19), 03-database-erd.md (§17–19)

---

# 1. Purpose

The entire point of this platform is captured in one sentence:

> Core provides reusable platform capabilities; modules provide business capabilities.

This document defines what a module *is*, how it is built, installed, versioned, secured, and removed — precisely enough that a third-party developer can build a Finance, HR, or CRM module without ever touching core code.

---

# 2. Module vs. Module Installation

These are two different things and the distinction matters:

```text
Module               → the software: code, manifest, migrations, permissions it declares
                        (one row in `modules`, platform-wide)

Module Installation  → the fact that a given Organization has turned that
                        module on, with its own configuration
                        (one row per organization in `module_installations`)
```

A module can be registered on the platform but disabled for Organization A while enabled for Organization B, each with independent configuration (02 §19). This is what makes the platform genuinely multi-tenant rather than "install once, on for everyone."

---

# 3. Module Lifecycle

**What "install" means.** Modules are **compiled into** the `api` and `worker` binaries (the registry is generated, §5.2). A module's code therefore exists in the binary before anything is "installed". *Install* is the **database-level activation of a module that is already compiled in**: run its migrations, sync its permissions, record it. It never downloads or loads code at runtime.

```text
registered   (compiled in; manifest validated at startup; row in `modules`)
    │
    ▼
installed ──────► failed  (migration or dependency failure)
    │
    ▼
enabled ◄──────► disabled      (per organization, via module_installations)
    │
    ▼
uninstalled
```

| Transition | What happens |
|---|---|
| register | At startup: manifest validated, module row created/updated, `status = registered` |
| install | Dependencies resolved, migrations run with the `app_migrator` role (§5.3), permissions synced into `permissions` (§7), `status = installed` |
| enable (per org) | `module_installations` row created/updated to `enabled`, routes become reachable for that organization, menus appear for authorized users, the module's `.enabled` event is published |
| disable (per org) | Routes return 404 for that organization, menus hidden, data **preserved** — this is reversible |
| uninstall | Destructive. Requires explicit confirmation (02 §18 rule 6). Data handling (archive vs. delete) must be declared by the module and confirmed by the Super Admin; the down migrations run only after that |

Disabling must never be silently equivalent to uninstalling. A Group Admin turning off a module by mistake should be able to turn it back on and find their data intact.

A failed install leaves `status = failed` with the error recorded; it is never half-enabled, and the migration runner records its progress in the module's own version table (§5.3) so a retry resumes safely.

---

# 4. Module Manifest

```yaml
id: finance                       # also the table-name prefix: lowercase letters, digits, underscore
name: Finance
version: 1.2.0
description: Invoicing, approvals and financial reporting
author: Example Org
license: MIT
homepage: https://example.org/modules/finance

core:
  min_version: "1.0.0"

dependencies:
  - module: core
    version: ">=1.0.0"

permissions:
  - code: invoice.read
    resource: invoice
    action: read
    description: View invoices
  - code: invoice.create
    resource: invoice
    action: create
  - code: invoice.approve
    resource: invoice
    action: approve
  - code: invoice.field.internal_cost.read
    resource: invoice
    action: field.internal_cost.read

migrations: ./migrations

backend:
  entrypoint: finance.NewModule

frontend:
  entrypoint: ./frontend/index.ts
  routes:
    - path: /modules/finance
      component: ./frontend/routes/Dashboard.svelte
      requires_permission: invoice.read
  menu:
    - label: Finance
      icon: wallet
      route: /modules/finance
      requires_permission: invoice.read

settings_schema: ./settings.schema.json

events:
  publishes:
    - finance.invoice.created
    - finance.invoice.approved
  subscribes:
    - core.user.deactivated

notifications:                      # Notification Templates (§8): event → notification
  - event: finance.invoice.approved
    type: finance.invoice.approved  # i18n key suffix: notifications.finance.invoice.approved
    recipients: payload.requested_by           # a payload path resolving to user id(s)
    priority: normal
    params: [invoice_number, approved_by_name]  # payload fields copied into notifications.data
  - event: finance.invoice.submitted
    inbox: true                       # creates an Inbox item (actionable) instead of a Notification
    type: finance.invoice.needs_approval
    recipients: payload.approver_ids
    resource: { type: invoice, id: payload.invoice_id }

jobs:                               # background jobs run by the worker
  - name: finance.send_overdue_reminders
    schedule: "0 8 * * *"
```

The manifest is the contract. Everything the platform needs to know about a module — what it needs, what it grants, what it shows, what it listens for, what notifications it produces — is declared here, not discovered by introspecting code.

In the database, this is stored as-is in `modules.manifest jsonb` (03 §17) precisely because the contract will keep growing; a rigid relational schema for it would mean a migration every time a new manifest field is added. The manifest is validated against a JSON Schema at startup; an invalid manifest (`module_manifest_invalid`) stops that module from registering and is reported in the system log and readiness output — it does not crash core.

---

# 5. Backend Module Contract

```go
// packages/module-sdk — interfaces only; implementations live in backend/internal/module.
type Module interface {
    Manifest() Manifest
    Migrate(ctx context.Context, runner MigrationRunner) error
    RegisterRoutes(r Router, deps CoreDeps)                 // executed in the API process
    RegisterPermissions() []PermissionDecl
    RegisterEventHandlers(bus EventBus)                     // executed in the WORKER process (via the outbox)
    RegisterJobs(s JobScheduler)                            // executed in the WORKER process
    Health(ctx context.Context) error                       // feeds /health/ready and module_health_status
}

type MigrationRunner interface {
    // Runs the module's own migration sequence (starting at 001) against its own
    // version table, using the privileged migration role. Up and Down are
    // called only by the core lifecycle, never by module code at runtime.
    Up(ctx context.Context) error
    Down(ctx context.Context) error
}
```

`CoreDeps` is how a module reaches core capabilities (identity lookups, the authorizer, the audit recorder, the notification service, the database pool, the transaction manager) — **always through an interface core exposes**, never by importing another module's internal package.

```text
┌───────────────┐        ┌───────────────┐
│     core      │        │    modules    │
│               │◄───────┤               │
│  interfaces   │ depends │  finance, hr, │
│  contracts    │   on    │  crm, ...     │
└───────────────┘        └───────────────┘
```

The dependency arrow only ever points one way. Core has no idea `finance` exists *in source code*. This is what makes "delete a module's folder and the platform still builds" true (after re-running `make modules-sync`, §5.2), and it is what makes third-party, out-of-tree modules possible at all.

## 5.1 Routing contract

`module-sdk` exposes a deliberately thin `Router` on top of chi, so modules neither import chi nor need to know it exists:

```go
type Router interface {
    Get(path string, h http.HandlerFunc, opts ...RouteOption)
    Post(path string, h http.HandlerFunc, opts ...RouteOption)
    Put(path string, h http.HandlerFunc, opts ...RouteOption)
    Patch(path string, h http.HandlerFunc, opts ...RouteOption)
    Delete(path string, h http.HandlerFunc, opts ...RouteOption)
}

func Require(permission string) RouteOption   // the generic "may this caller reach this endpoint" check
func Idempotent() RouteOption                 // requires Idempotency-Key (08 §6)
func URLParam(r *http.Request, name string) string
```

Every module route **must** declare `Require(...)` (or an explicit `Public()` for the rare unauthenticated route, which is flagged in review and listed in the module's README). A route without either is rejected at registration. This is what gives 17 §9's statement "the middleware already checked" a mechanical basis. All module routes are mounted under `/api/v1/` and are automatically wrapped by the module gate (§5.4).

## 5.2 Compile-time registry and the generator

```text
make modules-sync
```

reads `modules/*/module.yaml` and writes the generated registries:

```text
backend/cmd/api/modules_gen.go        # blank-imports + registration of every module for the API
backend/cmd/worker/modules_gen.go     # the same for the worker
apps/web/src/modules_gen.ts           # frontend route/menu/i18n registration
```

The generated files are committed. CI fails if they are out of date (`make modules-sync` produces a diff). A module added to `modules/` but not yet synced is simply not part of the build.

## 5.3 Migrations

- A module's migrations live in its own `migrations/` directory, **numbered from 001**, and are tracked in the module's **own version table** `schema_migrations_<module_code>` — so module numbering never collides with core's sequence or another module's (04 §15.2).
- They are executed by core's lifecycle through `MigrationRunner` using the **`app_migrator`** database role. A module never receives DDL rights through `CoreDeps`; its runtime pool is the ordinary `app_user` pool.
- Tables created this way automatically inherit the `app_user` default privileges (04 §13.2). A module that wants an append-only table adds its own `REVOKE UPDATE, DELETE` in the migration.
- Every table with an `updated_at` column calls `SELECT attach_updated_at('<table>');` (04 §4). CI checks this for module tables too.

## 5.4 The module gate

A middleware wraps every module route and, per request, verifies that the module is **enabled for the caller's organization** (Redis flag `module:{organization_id}:{code}`, backed by `module_installations`). A disabled module's routes respond `404 resource_not_found` — indistinguishable from a route that does not exist, and unreachable even for a Super Admin acting inside that organization. Disable/enable events update the flag immediately.

---

# 6. Module Isolation

A module owns its own tables and must not read or write another module's tables directly, even though they physically live in the same PostgreSQL database.

Convention:

```text
finance_invoices
finance_invoice_lines
hr_employees
```

Table names are prefixed by module code (`<code>_*`). This isn't just cosmetic — it makes an accidental cross-module join or an ad-hoc query against another module's table obvious in review, and it means a module can be given its own PostgreSQL schema later (`finance.invoices`) without a rename if stricter isolation is ever needed. A CI lint scans a module's SQL and Go query strings for table names that are neither `<code>_*` nor on the allowlist of core tables a module may **read** (`users`, `organizations`, `groups`, `organization_memberships`, `group_memberships`).

If Finance needs to know a user was deactivated, it does **not** query `users` for anything beyond what core already exposes — it subscribes to the `core.user.deactivated` event (§8) or calls a core-provided `IdentityLookup` interface. If Finance needs to react to an HR employee's termination, it subscribes to an event HR publishes; it never reaches into `hr_employees`.

---

# 7. Permission Registration

Permission codes and the Effective Permission Set model are defined in 06 §5. This section defines how a module's declared permissions enter that catalog.

On `install` or `enable`:

```text
For each permission in manifest.permissions:
    UPSERT INTO permissions (module_id, resource, action, code, description, is_system = false)
    ON CONFLICT (code) DO UPDATE SET description = EXCLUDED.description
       -- but only if the existing row belongs to the SAME module; a code owned by
       -- another module or by core is a conflict → module_permission_conflict
```

Permission codes must start with the module's own resource names declared in its manifest; a module can never declare a permission code in a core namespace (`user.*`, `role.*`, …).

On `disable`:

- Permissions rows are **not** deleted (`role_permissions` referencing them would orphan, and audit history references permission codes).
- No **new** role_permission assignments referencing the module's permissions are allowed while disabled.
- Existing assignments remain in the database but resolve to nothing, because the module's routes are gated — there's nothing left to authorize access *to*.

On `uninstall`:

- The Super Admin is shown exactly which roles reference the module's permissions before confirming, since removing the permission rows will affect those roles.

---

# 8. Module Events & Notification Templates

The Internal Event Bus is how modules talk to core and to each other without a compile-time dependency:

```go
type EventBus interface {
    Publish(ctx context.Context, event Event)
    Subscribe(eventName string, handler EventHandler)
}
```

**Two delivery paths** (the reason the API and worker being separate processes does not break event handling):

1. **Transactional outbox (durable, cross-process).** `Publish` called while a transaction is open in `ctx` writes the event to `outbox_events` **in that same transaction** (04 §12, 10 §6). The worker's relay delivers it to subscribers, **at-least-once**. Handlers registered through `RegisterEventHandlers` run in the worker and **must be idempotent** (deduplicate on `event.ID`). This is the path for everything that must not be lost: notifications, inbox items, module-to-module reactions.
2. **In-process synchronous handlers.** A small set of core handlers that only do cheap, local work (authorization cache version bump, module-flag refresh) run inside the publishing API process immediately after commit. Modules do not use this path.

Naming convention:

```text
<module>.<entity>.<action>
```

```text
finance.invoice.created
finance.invoice.approved
hr.employee.terminated
core.user.deactivated
core.role.changed
```

**Notification Templates.** The manifest's `notifications:` section (§4) maps an event to a Notification or an Inbox item: which payload path resolves to the recipients, which payload fields become `notifications.data` parameters, the `type` the frontend renders through its i18n catalog (08 §8), and the priority. Core's notification service (09 §6) only ever deals in "an event happened, here's who to notify and with which parameters" — it has no Finance-specific code path, and a module never calls the notification API directly.

The event payload is validated against the template at registration time where possible (recipient paths must exist in the declared event schema); a template that resolves to no recipients at runtime logs a warning rather than failing the event.

The authorization cache invalidation (06 §8) uses the in-process path; its events (`role_permissions.changed`, …) are ordinary events on the same bus.

---

# 9. Module Dependencies

```text
module_dependencies (module_id, dependency_module_id, version_constraint)
```

- Resolved at install/enable time using semantic version constraints (`>=1.0.0`, `^2.1.0`).
- Circular dependencies are rejected at registration, not discovered later at runtime.
- A module cannot be enabled while a required dependency is missing or version-incompatible; this is a hard `install`-time failure (`module.status = failed`), not a warning.
- Disabling a module that another *enabled* module depends on is refused with `module_dependency_missing` until the dependent is disabled.

---

# 10. Module Configuration & Settings

Module configuration rides on the generic `settings` table (02 §20), scoped `module` + `organization_id`:

- Validated against the module's declared `settings_schema`.
- `is_secret = true` values (API keys, webhook secrets) are stored encrypted, are never returned in ordinary read responses — write-only from the API's perspective — and never appear in activity metadata.
- Changing module settings is audited like any other configuration change, using optimistic locking (`row_version`, 03 §48).

---

# 11. Frontend Module Contract & the Six-File Page Pattern

Every module page that's a standard list/detail/create/edit workflow should follow the same shape, rather than each module inventing its own structure. This is the platform-wide convention:

```text
modules/<code>/frontend/routes/<entity>/
├── List.svelte           # table, pagination, filters
├── Detail.svelte         # single-record view
├── Form.svelte           # shared create/edit form
├── Modal.svelte          # confirm/delete/bulk-action dialogs
├── PermissionGate.svelte # re-export of the shared gate from packages/ui (no per-module copy)
└── Breadcrumb.ts         # hierarchy nav (Org → Group → Entity)
```

`PermissionGate` calls the same frontend permission-check helper everywhere (`$can('invoice.approve')`), backed by the effective-permission set the API already returned at login/session-refresh — never a per-module reimplementation of "should I show this button." The backend re-checks on every request regardless (06 §15) — this pattern is about consistent UX, not security.

The frontend registers itself through the generated `apps/web/src/modules_gen.ts` (§5.2): routes, menu entries and translation files from each manifest.

---

# 12. Module CLI

```text
make module-create NAME=finance
```

generates the manifest stub, `migrations/` (starting at 001), `permissions/permissions.yaml` starter, `backend/`, `frontend/` (six-file scaffold for the first entity), `events/`, `tests/`, and a `README.md` following this document — then runs `make modules-sync` — turning "read the module docs and set up boilerplate by hand" into "run one command and fill in business logic." The full walkthrough is 17-module-developer-guide.md; this document defines the contract that guide teaches against.

---

# 13. Module Security Considerations

Because this is a modular monolith, every enabled module runs **in the same process** as core and every other module. This is a deliberate simplicity trade-off ("simple before distributed"), but it has a real consequence worth stating plainly:

> A broken or malicious module can affect the whole platform, not just its own data.

Until the sandboxed-modules roadmap item (20 §5) exists, treat installing a third-party module the same way you'd treat merging an external pull request into production: review the code, review the manifest's declared permissions for anything broader than the module needs, and don't enable unreviewed modules in a production organization. This isn't a gap in the design — it's an explicit, documented boundary of what "modular monolith" means, so nobody discovers it the hard way. (What *is* enforced: the compile-time import boundary, the permission-namespace rule in §7, the table-prefix lint in §6, and least-privilege database roles.)

A module's `Health()` (§5) is how it tells the platform it is degraded; `/health/ready` and the `module_health_status` metric consume it (10 §11, 14 §7).

---

# 14. REST API — Module Management (Super Admin / Org Admin)

```text
GET    /api/v1/modules
GET    /api/v1/modules/{code}
POST   /api/v1/modules/{code}/install
POST   /api/v1/organizations/{org_id}/modules/{code}/enable
POST   /api/v1/organizations/{org_id}/modules/{code}/disable
PATCH  /api/v1/organizations/{org_id}/modules/{code}/configuration
DELETE /api/v1/modules/{code}                       # uninstall; requires ?confirm=<code>
GET    /api/v1/modules/{code}/uninstall-impact      # roles/permissions affected, data-handling options
```

`install`, `enable` and `disable` require `Idempotency-Key` (08 §6).

---

# 15. Error Model

```text
module_not_found
module_already_installed
module_dependency_missing
module_version_incompatible
module_migration_failed
module_manifest_invalid
module_circular_dependency
module_permission_conflict
module_not_enabled
```

---

# 16. Package Structure

```text
packages/
└── module-sdk/             # interfaces only: Module, CoreDeps, Router, EventBus, MigrationRunner, …

backend/
├── internal/
│   └── module/
│       ├── domain/         # Module, ModuleInstallation, Manifest
│       ├── application/    # register, install, enable, disable, uninstall use cases
│       ├── registry/       # compiled-in module list (from the generated files)
│       ├── gate/           # per-organization route gate
│       ├── migrate/        # MigrationRunner implementation (app_migrator pool, per-module version table)
│       ├── repository/
│       └── transport/http/
└── cmd/
    ├── api/modules_gen.go      # generated
    └── worker/modules_gen.go   # generated
modules/
├── finance/
│   ├── module.yaml
│   ├── backend/
│   ├── frontend/
│   ├── migrations/
│   ├── permissions/
│   ├── events/
│   ├── tests/
│   └── README.md
```

`internal/module` is core's bookkeeping about modules. `modules/<code>/` is the module's own code — core never imports it *by hand*; only the generated `modules_gen.go` does.

---

# 17. Testing Requirements

```text
Lifecycle
  install runs migrations; failure leaves status = failed, not partially enabled
  a failed install can be retried and resumes from the module's own version table
  enable syncs permissions; disable does not delete data
  uninstall requires explicit confirmation and reports affected roles first

Dependencies
  circular dependency rejected at registration
  incompatible core version rejected at install
  disabling a module that an enabled module depends on is refused

Contract
  a route declared without Require()/Public() is rejected at registration
  a manifest that fails JSON-Schema validation does not register and does not crash core
  a permission code in a core namespace (user.*, role.*) or owned by another module → module_permission_conflict
  make modules-sync is reproducible: no diff on a second run

Isolation
  a module cannot import backend/internal/* (compile test in a fixture module)
  a module's SQL touching another module's tables fails the isolation lint
  disabling a module immediately 404s its routes for that organization only

Events
  an event published in a transaction that rolls back is never delivered
  a handler receiving the same event twice produces the effect once (idempotency)
  notification template: event → Notification/Inbox item with the declared recipients and params

Multi-tenancy
  module enabled for Org A has no effect on Org B
  configuration for Org A is not visible to Org B
  secret settings are never returned by any read endpoint

Health
  a failing Health() is reflected in /health/ready (per policy) and in module_health_status
```
