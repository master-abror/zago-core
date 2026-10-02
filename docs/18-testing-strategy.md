# Testing Strategy
## Open Source Modular Application Platform v1.0

**Document:** 18-testing-strategy.md
**Status:** Baseline (Rev 1.1)
**Previous:** 17-module-developer-guide.md
**Next:** 19-ci-cd.md
**Builds on:** the "Testing Requirements" section of every prior document

---

# 1. Purpose

Every document so far ends with its own "Testing Requirements" — that's *what* needs a test, decided by whoever owns that piece of the system. This document is the layer above: *how* those tests are written, what tools, what the coverage bar actually means, and — because scattering the full list across many documents makes it easy to lose track — a single consolidated catalog (§5) so nothing on any of those lists quietly goes unimplemented.

---

# 2. The Test Pyramid, Mapped to This Platform

```text
                    ▲
                   /e2e\           few, slow, expensive — the handful of flows
                  /------\          where an integration bug would be costly
                 /  integr. \       repository + kernel tests against real PostgreSQL/Redis;
                /------------\      contract tests for the API surface; smoke scripts
               /   component   \     frontend logic-bearing components
              /------------------\
             /       unit          \  domain logic, application services with
            /------------------------\ mocked dependencies — many, fast, cheap
```

This mirrors 10 §14's layering exactly: domain and application code sit at the wide, cheap base; repository, kernel and transport tests sit in the middle; full end-to-end flows are the narrow, expensive top, used sparingly and deliberately.

---

# 3. Tooling

```text
Backend
  Unit / application   — Go's standard `testing` package + testify (require/assert, mocks)
  Repository / kernel   — testcontainers-go with a real ephemeral PostgreSQL 18 and Redis 8 per
                          test run — never a mocked database for repository tests, since the
                          entire point is catching a wrong query, a missed index, a
                          constraint or trigger that doesn't fire the way it's assumed to,
                          or a transaction that isn't actually atomic
  Schema                — a dedicated `db-test` suite (04 §17) that applies the real migrations
                          and attacks the constraints, triggers and grants directly (SQL-level)
  Contract              — the generated OpenAPI schema (08 §9) validated against actual handler
                          responses in CI
  Fuzz                  — Go native fuzzing for the policy expression parser and the cursor decoder
  Smoke                 — scripts/smoke/mNN.sh: black-box curl scripts against a running stack,
                          cumulative (`make smoke`)

Frontend
  Component / unit      — Vitest (pairs naturally with the existing Vite build, 11 §1.1)
  E2E                   — Playwright, against a real backend running via the compose stack
                          (15 §2) with Mailpit capturing emails
```

Tests run with `-race` (16 §2.3). A test never depends on another test's data: each gets its own organization(s) through fixtures.

---

# 4. What "Good Coverage" Actually Means Here

A coverage percentage target is a weak proxy on its own — it's trivial to hit 90% while never testing the one `if` that matters. Concretely, for this platform:

```text
Domain layer                — near-100% is realistic and expected: it's pure functions
                              over plain values, cheap to test exhaustively, and it's
                              where invariants like "can't approve a closed invoice"
                              or "can't publish twice" live.

Authorization & authentication — every negative case is mandatory, not optional
                              (06 §14, 05 §20). A PR touching either without a
                              corresponding denial test is incomplete, regardless of
                              overall coverage percentage.

Tenant isolation              — every new tenant-scoped repository method gets a
                              cross-organization negative test at the same time it's
                              written (12 §6) — not filed as a follow-up ticket.

Transactions & events         — every use case that writes state + audit + event has a test
                              that forces a failure mid-transaction and asserts that NOTHING
                              (state, audit, outbox) was persisted (10 §6.1), and every
                              event handler has a duplicate-delivery test.

Everything else                — reviewer judgment. A trivial getter doesn't need a
                              test; a branch with real business logic does.
```

A PR is reviewed against this list, not against a single coverage number the CI dashboard reports — the number is a signal to look closer, not a pass/fail gate on its own (though a *regression* in the number, §8.1, is a gate).

---

# 5. Consolidated Negative-Test Catalog

Every "Testing Requirements" section written so far, in one place. This table is the actual test plan for the platform; treat a document that introduces a new testing requirement as incomplete until it's added here too.

```text
Database (04)
  CHECK / unique / partial-unique constraints (roles, role_assignments, settings, conversations,
    invitations, users.email lowercase, modules.code format)
  composite foreign keys reject cross-organization membership/role/conversation rows
  group hierarchy: cycles, depth, cross-org parent, organization_id immutability
  EVERY table with updated_at has the trigger (catalog scan — also for module tables)
  app_user cannot UPDATE/DELETE activities, security_events, system_logs; new tables inherit DML grants
  app_maintenance can only do what its grants allow
  every .down.sql reverses its .up.sql (migrate-roundtrip)

Authentication (05)
  wrong password / unknown email → same generic failure and indistinguishable timing
  locked (Redis TTL) / suspended account → failure; status revealed only after a correct password
  expired / revoked / invalid session → rejected; idle timeout → rejected before absolute expiry
  only the token hash is stored; Redis down → PostgreSQL fallback (degraded), not logout
  missing/invalid/foreign CSRF token → rejected
  password change / reset / logout → sessions revoked; token rotates on login, password change, switch-org
  reset and invitation tokens: single-use, expiring, hashed, identical responses when invalid
  invitation preset role above the inviter's current authority → rejected
  Argon2id: PHC format, rehash-on-login, max length, bounded concurrency
  wrong/expired/replayed MFA or recovery code → failure (Phase 2)

Authorization (06)
  revoked / expired / archived-role assignment → denied
  wrong organization / wrong active group / forged X-Active-Group-ID → denied
  resource-scoped and own-scoped assignments apply only to the exact resource/owner
  Admin cannot grant a permission or scope beyond their own
  deny policy overrides an otherwise-allowed permission
  policy expression cannot execute arbitrary code; limits enforced; fuzzed
  restricted field absent from REST, WebSocket, and export output alike; write → field_permission_denied
  role/permission change takes effect on the very next request (version bump)
  Redis down → computed from PostgreSQL, never allow-by-default
  catalog sync idempotent, never deletes; Super Admin holds every is_system permission

Modules (07)
  failed install leaves status = failed, never partially enabled; retry resumes
  disabling preserves data; uninstall requires explicit confirmation
  circular dependency / incompatible core version rejected
  route without Require()/Public() rejected; invalid manifest does not register nor crash core
  permission code in a core namespace / owned by another module rejected
  disabled module's routes 404 immediately, for that organization only
  module enabled for Org A has no effect on Org B
  module cannot import backend/internal/*; module SQL touching another module's tables fails lint
  make modules-sync reproducible

Kernel (10)
  rollback leaves no state, audit or outbox row; denied attempts still leave an activity row
  outbox relay: retry with backoff, dead-letter, duplicate delivery produces the effect once
  every tenant-scoped repository method rejects cross-organization access
  a cross-tenant (platform) method cannot be called without a platform-level Decision
  a slow query cannot exhaust the connection pool (statement timeout fires)
  SIGTERM drains in-flight requests/jobs before exit; an interrupted outbox batch is safe
  Redis outage behaves per 10 §15

API (08)
  envelope shapes; unknown server error → internal_error with no detail
  tampered / filter-mismatched cursor → 400; stable ordering on equal timestamps
  idempotency: same body → same response, different body → conflict, concurrent → in_progress,
    cross-user replay impossible, Redis down + key required → fail closed
  oversized body → 413; wrong content type → 415; unknown JSON field → rejected
  stale version → version_conflict

Realtime (09)
  unauthenticated WebSocket connection rejected at handshake; wrong Origin rejected
  subscribing to a conversation you're not a member of is rejected
  session revoked / user suspended → live socket closed (across instances)
  offline user still sees the notification/message via REST on reconnect; resync_required for non-durable gaps
  event published on one instance reaches a client connected to another (Redis pub/sub)
  slow consumer disconnected without affecting others; oversized frame closes the connection
  deleted message becomes a tombstone; concurrent direct-conversation creation yields one
  attachment with contradictory content type rejected; download re-checks authorization

Frontend (11)
  a restricted field's absence renders correctly, no crash on a guessed default
  a denied permission both hides the control AND the underlying call still 403s
  core/api attaches CSRF, request id, active-group header and handles the error envelope
  duplicate WebSocket event is a no-op; reconnect + resync restores state

Audit (13)
  a user can never see another user's activity; a Group Admin never sees another group's
  "My Activity" and "All Activities" are always two distinct queries
  UPDATE/DELETE on activities/security_events/system_logs fails at the database grant level
  user.anonymize (maintenance connection) preserves the row while clearing the actor reference
  sensitive values never reach metadata (property test); CSV formula injection neutralized
  retention deletes in bounded batches and only beyond the window

Observability (14)
  /health/ready reflects a genuinely unreachable dependency or unhealthy module
  request_id/trace_id thread through logs, activity rows, outbox and traces consistently
  metrics labels use route patterns; logs never contain redacted values

Docker / Make (15, 16)
  the full compose stack reaches healthy within a bounded timeout
  production images build from the repository root, run as non-root without a shell
  a freshly scaffolded module's tests run under the ordinary `make test` with no extra wiring
  make dev leaves no orphan processes; make verify fails when any stage fails
```

---

# 6. Module Testing Requirements

A module's own test suite (17 §10) is judged the same way core's is — §4's bar applies identically, plus module-specific automated checks run in CI (19) rather than relying on a reviewer to notice by eye:

- **Isolation lint:** a static scan of the module's SQL and Go query strings for table names that are neither `<module_code>_*` nor on the allowlist of core tables a module may read (07 §6).
- **`attach_updated_at` check** for every module table with an `updated_at` column.
- **Route declaration check:** every module route declares `Require(...)` or `Public()` (07 §5.1).
- **Cross-tenant negative test** for every tenant-scoped table the module owns.
- **Registry reproducibility:** `make modules-sync` yields no diff.

---

# 7. Test Data & Fixtures

A shared builder pattern, not hand-written `INSERT` statements repeated across every test file:

```go
org := fixtures.NewOrganization(t, db)
user := fixtures.NewUser(t, db, fixtures.InOrganization(org))
role := fixtures.NewRole(t, db, fixtures.WithPermissions("invoice.approve"))
fixtures.Assign(t, db, user, role, fixtures.ScopedTo(org))
```

Each helper produces a valid, minimal row satisfying every constraint from 04 by default (including the composite foreign keys — `NewUser(…InOrganization)` creates the organization membership; `NewGroupMember` creates both memberships), with functional options for the specific variation a test actually needs — so a test reads as "what's different about this case" rather than fifteen lines of setup boilerplate repeated with one field changed. The fixtures live in `backend/internal/testkit` and are also exported for module authors.

---

# 8. Flaky Tests Are Bugs

A test that fails intermittently is quarantined (marked skipped, with a linked issue) the same day it's noticed — never silently re-run until green and left as-is. A flaky test that gets "fixed" by adding a retry or a longer sleep is usually a real race condition wearing a disguise (a cache invalidation that hasn't propagated yet, a WebSocket message that arrived out of order, an outbox event not yet relayed) — exactly the kind of bug this platform's own consistency guarantees (06 §8's synchronous invalidation, 09 §2.3's at-least-once delivery, 10 §6's outbox) depend on actually holding under load, so masking the symptom in the test is the wrong fix. Tests that wait for asynchronous work poll for the condition with a deadline rather than sleeping a fixed time.

## 8.1 Coverage regression

CI fails a PR that drops backend or frontend coverage below the committed baseline (not a fixed universal percentage — a ratchet that only ever holds steady or improves, tracked per the numbers already in place at the time this document is adopted) — this catches the specific failure mode of a large PR quietly skipping tests under deadline pressure, without pretending a single global target is meaningful across a domain layer and a thin HTTP handler alike.

---

# 9. What This Document Does Not Cover

Load/performance testing thresholds (target p95 latency, concurrent user targets) are a product/ops decision this document doesn't presume to set — the release milestone runs a light k6 smoke load to find gross problems and records results without claiming an SLA; when real thresholds exist, they belong here as a new section, not invented speculatively now.
