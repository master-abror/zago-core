# Security Threat Model
## Open Source Modular Application Platform v1.0

**Document:** 12-security-threat-model.md
**Status:** Baseline (Rev 1.1)
**Previous:** 11-frontend-architecture.md
**Next:** 13-audit-logging.md
**Builds on:** every prior document's own security sections

---

# 1. Purpose

A checklist (Argon2id, CSRF, rate limiting, and so on) tells you what's present; it doesn't tell you what it's *for*. This document organizes the same ground by **who is attacking what** — so a reviewer can ask "does this design handle a malicious module?" and get a direct answer with a pointer to the exact mechanism, rather than re-deriving it from a list of features.

Nothing here is a new mechanism beyond what the earlier documents specify. Every mitigation cited already exists in an earlier document; this document's job is to confirm nothing was missed and to say plainly which residual risks are accepted rather than solved.

---

# 2. Threat Actors

```text
1. Anonymous internet attacker       — no credentials, no session
2. Authenticated User                — valid session, minimal permissions
3. Group Admin                       — valid session, elevated permissions within a scope
4. A compromised or malicious Organization — in a multi-tenant deployment, one tenant
                                        attempting to reach another tenant's data
5. A buggy or malicious Module        — runs in-process with core and every other module
6. A Super Admin acting in bad faith, or a Super Admin account that's been compromised
7. Supply chain                        — a compromised dependency (Go module, npm package)
8. A malicious web page                — a page in the victim's browser targeting their
                                        logged-in session (CSRF, cross-site WebSocket)
```

Actors 4 and 5 are the two this platform's specific architecture (multi-tenant, modular monolith) makes distinctive — most of this document's weight sits there.

---

# 3. Trust Boundaries

```text
Internet
   │  (boundary 1 — anything crossing here is untrusted input)
   ▼
Browser
   │  (boundary 2 — same-origin session cookie + CSRF token; Origin check on WebSocket)
   ▼
Reverse proxy  (TLS, security headers; X-Forwarded-* trusted only from TRUSTED_PROXIES)
   ▼
Go API (cmd/api)  ◄── Redis / PostgreSQL ──►  Worker (cmd/worker)
   │  (boundary 3 — Organization A's data vs. Organization B's data, same database)
   │  (boundary 4 — core vs. an installed business module, same process)
   │  (boundary 5 — runtime database role vs. maintenance vs. migrator roles)
   ▼
PostgreSQL / Redis
```

Boundaries 3 and 4 are **internal** to a single running process and a single database — they exist only because of authorization logic and code discipline, not because of network segmentation. That's precisely why 06's "default deny, checked on every request" posture and 07's isolation conventions carry as much weight as they do here: nothing at the network layer is doing this job for them. Boundary 5 *is* enforced by the database: a bug in request-handling code cannot alter audit rows or run DDL, because the connection it holds is not allowed to.

---

# 4. STRIDE

## 4.1 Spoofing

| Threat | Mitigation |
|---|---|
| Session fixation | Session token rotates at every auth boundary (05 §12.2) |
| Stolen session token from a database or Redis dump | Only the SHA-256 hash is stored anywhere; the raw token exists only in the cookie (05 §7.1) |
| Predictable session token | 32 random bytes; never a UUID/UUIDv7, never derived from user data (05 §7.1) |
| Credential stuffing / brute force | Per-account + per-IP rate limiting, progressive delay, Redis TTL lock rather than a bare hard-lock (05 §12.3) |
| CSRF on a mutating request | `X-CSRF-Token` bound to the session, validated server-side, plus `SameSite=Lax` and same-origin deployment (05 §8) |
| Cross-site WebSocket hijacking | Handshake verifies `Origin` against `ALLOWED_ORIGINS`; no token in URL (09 §2) |
| Forged `X-Active-Group-ID` | Never trusted on its own — re-verified against real `group_memberships` on every request (06 §4.2) |
| Forged `X-Forwarded-For` to evade rate limits | Trusted only from `TRUSTED_PROXIES` (08 §7, 10 §8) |
| Reusing an invitation or reset link | Random, single-use, short-lived, stored as hash; identical responses for invalid/used/expired (05 §13) |

## 4.2 Tampering

| Threat | Mitigation |
|---|---|
| Mass assignment / over-posting a restricted field | Unknown JSON fields rejected (08 §2.3); field write-permissions checked independently of read (06 §7) |
| SQL injection | Parameterized queries only; sqlc-generated access; no repository method accepts a raw SQL fragment from a caller (10 §13) |
| Audit trail tampering | `activities`/`security_events`/`system_logs` are append-only at the PostgreSQL grant level for the runtime role; only a separate maintenance role may anonymize or purge (04 §13) |
| Event/audit lost or forged after a crash | Audit and outbox rows are written in the same transaction as the state change (10 §6) |
| Policy expression escaping its sandbox | Restricted grammar, hand-written parser, bounded size/depth/steps, fuzz-tested; no `eval` (06 §6) |
| Tampered pagination cursor | HMAC-signed cursor (08 §4) |
| Replayed or double-applied request | `Idempotency-Key` on sensitive mutations, scoped to the caller (08 §6) |
| Cross-tenant write through a crafted foreign key | Composite foreign keys tie memberships, roles, invitations and conversation members to the same organization at the database level (04 §2.4) |

## 4.3 Repudiation

| Threat | Mitigation |
|---|---|
| "I didn't do that" / no record of a sensitive action | Every state change produces an Activity; auth events produce a Security Event; both are append-only and correlated by `request_id`/`trace_id` (13 §2, §5) |
| Denied attempts leave no trace | `result = denied` activities are written in their own transaction (10 §6.1) |
| A deleted user's actions become unattributable | Deletion anonymizes the actor reference through the maintenance role; it never deletes the activity row itself (13 §7) |

## 4.4 Information Disclosure

| Threat | Mitigation |
|---|---|
| Cross-organization data leak | Every tenant-scoped repository method requires `organization_id`; cross-tenant methods are separately named and require a platform-level decision; explicitly negative-tested (10 §13, this doc §6) |
| Cross-group data leak within one organization | Scope-checked at the authorization layer, not the query layer alone (06 §5.2) |
| Restricted field exposure | Enforced at serialization, identically across REST, WebSocket, exports, and background jobs (06 §7) |
| Account enumeration via error messages or timing | Generic login/forgot-password/invitation responses; dummy password verification equalizes timing; lock/suspend status revealed only after a correct password (05 §3, §4, §13, §18) |
| Verbose errors / stack traces reaching a client | Only the transport layer maps errors; anything unrecognized returns a bare `internal_error` (10 §9) |
| Sensitive values in logs/audit | Passwords, tokens, session/reset/invitation tokens, and MFA secrets are never logged, in any table (13 §2.3); configuration values are never logged (10 §8) |
| Uploaded file served as active content, or storage path guessed | Server-side content-type detection, files served only through an authorized endpoint with safe headers (`Content-Disposition`, `X-Content-Type-Options: nosniff`), storage keys unrelated to file names (09 §8.2) |
| Secrets in module settings | `is_secret` values encrypted, write-only from the API (07 §10) |
| XSS exposing the session | Cookie is `HttpOnly`; strict CSP; user content rendered as text (11 §12) |

## 4.5 Denial of Service

| Threat | Mitigation |
|---|---|
| Login / password-reset / invitation / MFA-verify flooding | Dedicated tighter rate limits on these endpoints (05 §12.3, 08 §7) |
| Argon2id used as a CPU/memory amplifier | Maximum password length, hashing concurrency semaphore (05 §4.1) |
| WebSocket message or typing-indicator flood; slow consumer | Per-user Redis-backed rate limiting; read limit; bounded buffer with slow-consumer disconnect; connection caps (09 §2.1, §9) |
| A single slow query exhausting the connection pool | Statement timeouts on every query (10 §13) |
| Oversized request bodies / uploads | Default 1 MiB JSON body limit, 10 MiB upload limit (configurable), content-type validation (08 §2.3, 09 §8.2) |
| Pathological policy expression | Length/depth/step limits (06 §6) |
| Outbox backlog or poison event | Exponential backoff, dead-letter state, `outbox_pending`/age alerts (10 §6.2, 14 §8) |
| Redis outage turning into an application outage | Defined degraded behavior for every Redis use (10 §15) |

## 4.6 Elevation of Privilege

| Threat | Mitigation |
|---|---|
| An Admin granting more authority than they hold | Authority-ceiling check inside the granting transaction (06 §5.3); re-checked for invitations at acceptance (05 §13.3) |
| A revoked permission remaining usable via a stale cache | Cache invalidation is a synchronous version bump, not TTL-driven (06 §8) |
| A module declaring permissions broader than it needs, or in a core namespace | Core-namespace and cross-module permission codes rejected at registration (07 §7); breadth is a human-reviewed control (§5) |
| A long-lived token outliving a permission change | Access tokens never embed permissions; authorization is re-evaluated server-side on every request (05 §10.1) |
| Request handler abusing database power | Runtime role has no DDL and no UPDATE/DELETE on audit tables; module installation uses a separate role held only by the lifecycle service (04 §13, 10 §13) |
| Super Admin account compromise | Super Admin actions are fully audited; MFA is Phase 2 and strongly recommended for Super Admins (20 §3); platform-level actions require the platform-level decision path |

---

# 5. Module Trust — Accepted Residual Risk

Stated plainly, because a threat model that hides its own gaps is worse than one that names them: **a buggy or malicious module can affect the whole platform**, not just its own data, because every module runs in the same process as core (07 §13). Go's `internal/` visibility rule stops a module from *importing* core's private packages (10 §4), but it does nothing to stop a module from making excessive outbound network calls, consuming unbounded memory, or corrupting its own tables in a way that degrades the shared database.

This is accepted, not solved, for the current architecture generation:

```text
Mitigated today:    compile-time import boundary (module-sdk only), manifest-declared
                    permissions reviewed before granting, permission-namespace rule,
                    table-name-prefix convention + CI lint limiting *accidental*
                    cross-module queries, least-privilege runtime database role

Not yet mitigated:  a module cannot be resource-limited (CPU/memory/network) independently
                    of the whole process; a module's code runs with the same OS-level
                    privileges as core and can read any table the runtime role can read

Planned mitigation: sandboxed modules (20 §5) — a future architecture generation, not this one
```

Operational guidance until then: treat installing a third-party module in a production organization the same as merging an external pull request — review it first (07 §13).

---

# 6. Multi-Tenant Isolation — Making It Structural, Not Just Disciplined

"Every query filters by `organization_id`" is a rule a person can forget under deadline pressure. Structural backstops, on top of the convention:

1. **Repositories require an `organization_id` argument to compile.** A method with no plausible tenant-scoping is the deliberate exception, written as a distinctly-named method (`…Platform…` or `FindByIDUnscoped`) that demands a platform-level `Decision` argument (10 §13), so it cannot be called from a tenant-level path and is conspicuous in review.
2. **Database-level tenant integrity** where a plain foreign key can't express it: composite foreign keys for memberships, roles, invitations and conversation members (04 §2.4); an immutable `organization_id` on groups.
3. **A negative-test requirement, not an optional nice-to-have** (10 §14, 18 §4): every new tenant-scoped table gets a test asserting Organization A's session cannot read or write a row that belongs to Organization B, written at the same time as the feature, not bolted on later.
4. **Row-Level Security** is not used in v1.0 (03 §42); if a high-assurance deployment needs it, it must be designed together with connection pooling and transaction-local tenant context — not enabled partially.

---

# 7. Secrets Management

```text
Session/CSRF signing material, database credentials (three roles), Redis credentials, SMTP
credentials — environment variables sourced from the deployment's secret manager, never
committed (10 §8)

Session, reset and invitation tokens — only SHA-256 hashes are stored (05 §7.1, §13)

MFA/TOTP secrets — encrypted at rest, not hashed, because verification needs to recover
the original value (05 §6)

Module-declared secrets (API keys, webhook secrets in `settings`) — encrypted, write-only from
the API's perspective; never returned in a read response (07 §10)
```

Key rotation for the encryption-at-rest key protecting MFA secrets and secret settings is an operational runbook this document flags as needed but does not itself specify — it belongs in a deployment/operations document once there's a real key-management story (KMS, Vault, or equivalent) to describe.

---

# 8. Supply Chain

- Backend (Go modules) and frontend (npm packages) dependencies are scanned in CI (19 §4) — a known-vulnerable dependency fails the build, it doesn't just generate a warning someone can ignore. Lockfiles (`go.sum`, `package-lock.json`) are committed and installs use `npm ci`.
- Build and CLI tools are pinned (`go.mod` tool directives) so CI and contributors run the same versions.
- Production images are built from a minimal base (distroless for the Go binaries) and run as a non-root user (15 §3).
- A community module is source you're choosing to run in your own process (§5) — the same scrutiny applied to a dependency applies to a module, arguably more so given the shared fault domain.
- Module signing and a vetted module marketplace are the long-term answer to "can I trust a module I didn't write without reading all of it myself" — deliberately future work (20 §5).

---

# 9. Forensics & Incident Response

The data this platform already keeps is what an investigation would need: `activities` (what happened), `security_events` (auth-specific detail), `system_logs` (what the infrastructure was doing at that moment), all correlated by `request_id`/`trace_id` (13 §5). A full incident-response runbook (who gets paged, escalation paths, communication templates) is an operational document, not an architecture one — out of scope here, but worth naming so it isn't mistaken for "not needed."

---

# 10. Explicitly Deferred

Named here so a reviewer can see these are deliberate choices, not oversights:

```text
TOTP MFA / recovery codes           — 05 §6, §21 Phase 2
WebAuthn / Passkeys                  — 05 §16.2
OIDC as an identity provider          — 05 §16.1
Sandboxed / resource-limited modules   — §5 above
Module signing & marketplace vetting    — §8 above
Multi-region deployment (blast-radius / data-residency implications) — 20 §5
Advanced policy engine (beyond the restricted expression grammar)     — 06 §6
Antivirus scanning of uploads              — 20 §4
```

---

# 11. Compliance Posture

Not a certification claim — a note on what the architecture does and doesn't get in the way of:

- **Right to be forgotten vs. audit integrity**: resolved by anonymizing the actor reference on user deletion while preserving the activity row itself (13 §7) — a deployment can honor a deletion request without losing its audit trail.
- **Configurable retention** (13 §6) means a deployment operating under a stricter or laxer regulatory regime sets its own retention window rather than the platform hardcoding one that fits nobody exactly.
- **Data residency** (which region PostgreSQL/Redis run in) is a deployment decision this architecture doesn't constrain, but multi-region *replication* is explicitly future work (§10) — a single-region deployment today has one clear answer to "where does the data live," a multi-region one doesn't yet.

---

# 12. Testing Requirements

The consolidated negative-test list, organized by which document already specifies the corresponding mechanism — this table exists so nothing is quietly unimplemented:

```text
cross-tenant access                        → 10 §14, this doc §6, 18 §5
cross-group access                          → 06 §14
unauthorized delete/approval                 → 06 §14
unauthorized field access                     → 06 §14
private chat access                            → 09 §12
disabled-user login                             → 05 §20
revoked / expired session                        → 05 §20
permission escalation                             → 06 §14
Admin granting beyond their authority               → 06 §14
cross-site WebSocket handshake (bad Origin)           → 09 §12
CSRF (missing/foreign token)                            → 05 §20
session token storage (only the hash exists)              → 05 §20
audit rows cannot be altered by the runtime role            → 04 §17
module permission in a core namespace                         → 07 §17
tenant-integrity foreign keys reject cross-org rows             → 04 §17
```

If a future document introduces a new negative-test category, it's added to this table in the same change — this table drifting out of date is itself the failure mode a threat model exists to prevent.
