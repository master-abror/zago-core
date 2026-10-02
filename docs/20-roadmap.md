# Roadmap
## Open Source Modular Application Platform v1.0

**Document:** 20-roadmap.md
**Status:** Baseline (Rev 1.1)
**Previous:** 19-ci-cd.md
**Next:** 21-milestone-plan.md (the build order for everything *inside* v1.0)
**Builds on:** deferred items flagged across every prior document

---

# 1. Purpose

Many sections across this series say some version of "not yet, deliberately" — a residual risk accepted in 12, a simplification noted in 11, a phase-2 item in 05. Scattered like that, they're easy to lose track of and easy to mistake for oversights instead of decisions. This document is the one place they're all visible together, organized by when they matter, not by which document happened to mention them first.

**A rule for this document specifically**: when a future document (or an implementation PR) identifies a new deferred item, it gets added here in the same change. A roadmap that drifts out of date is worse than no roadmap — it actively misleads whoever reads it last.

This document is about what comes **after** v1.0. The order in which v1.0 itself is built is 21-milestone-plan.md.

---

# 2. What v1.0 Already Means

Documents 02 through 19 are not a partial system waiting on this roadmap to become real — together they specify a complete, shippable v1.0: multi-tenant organizations and groups, invitations and password reset, full RBAC with scope/policy/field permissions, a working module system with a reference module, realtime notifications/inbox/chat, complete audit logging, and the operational surface (observability, Docker, CI/CD) to run it. Everything below is what comes *after* that, not what's missing *from* it.

Rev 1.1 moved two items **into** v1.0 that earlier drafts listed as later: *forgot/reset password* and *invitations* (05 §13, 21 M05) — the success criterion "invite a Group Admin" (01 §5) requires the same single-use-token-plus-email mechanism, so building it twice would have been the wasteful choice.

---

# 3. Near-Term (expected shortly after v1.0 ships)

| Item | First flagged in | Why it's near-term, not v1.0 |
|---|---|---|
| TOTP MFA + recovery codes (strongly recommended for Super Admins) | 05 §6, §21 (Phase 2) | Password + session auth is a complete, secure baseline on its own; MFA is additive |
| Re-authentication for sensitive actions | 05 §21 (Phase 2) | Builds on MFA |
| Breached-password check | 05 §4.1 | Optional hardening on top of the length/hash policy |
| Tuning of rate-limit and upload-size defaults against real traffic | 12 §4.5 | v1.0 ships concrete, configurable defaults (08 §2.3, 09 §8.2); the right numbers are a tuning decision best made against real traffic |
| Per-module frontend code-splitting | 11 §5.1 | v1.0 ships every module's frontend in one bundle (routes are already lazy); splitting is a load-time optimization, not a functional gap |
| Mobile push notification delivery (APNs/FCM integration) | 09 §6 | The worker's fallback hook exists in v1.0; wiring a provider needs app-store presence and accounts outside this series |
| Valkey compatibility run in CI (matrix with Redis) | 00 §A | The code uses only basic commands; proving it keeps the licensing choice open for operators |

---

# 4. Mid-Term

| Item | First flagged in | Notes |
|---|---|---|
| WebAuthn / Passkeys | 05 §16.2, §21 (Phase 3) | UI space is already reserved (Security → Passkeys); real implementation work once TOTP MFA has proven the MFA UX patterns |
| OAuth2 / OIDC / enterprise SSO | 05 §16.1, §21 (Phase 3) | The `IdentityProvider` interface exists specifically so this is a new implementation, not a redesign |
| Mobile client authentication (OAuth2 + short-lived access tokens) | 05 §15 | Needs the OIDC work above |
| S3-compatible object storage driver | 09 §8.2 | v1.0 has the `storage` interface and a local-disk driver |
| Antivirus scanning of uploads | 12 §10 | Pluggable hook on the `storage` interface |
| Per-recipient read receipts for broadcast-style channels | 09 §8.3 | Only needed if a specific conversation type genuinely requires it — not a default upgrade to every conversation |
| Time-partitioning of `activities`, `system_logs`, `messages` | 13 §6 | Requires primary keys that include the partition key (`(id, created_at)`) and changes retention to partition drops; deliberately deferred until real volume justifies it |
| swag v2 / OpenAPI 3.x if not already adopted | 08 §9 | Decided by ADR in the frontend foundation milestone |

---

# 5. Long-Term (a future architecture generation, not an extension of this one)

| Item | First flagged in | Why it's genuinely long-term |
|---|---|---|
| Module marketplace + module signing | 07 §13, 12 §8 | Needs a trust/vetting process and a distribution mechanism that don't exist yet — a real product surface of its own |
| Sandboxed, resource-limited modules | 07 §13, 12 §5 | Requires process isolation (containers-per-module, WASM, or similar) — a different execution model than "modular monolith," not a tweak to this one |
| True out-of-tree third-party modules | 10 §4.1 | Needs `packages/module-sdk` published as its own independently versioned Go module — meaningful API-stability commitments that are premature before more than one or two real modules exist |
| Multi-region deployment | 12 §11 | Data-residency and replication implications that a single-region deployment's architecture doesn't need to answer today |
| Advanced policy engine (beyond the restricted expression grammar) | 06 §6, 12 §10 | The current grammar is deliberately non-Turing-complete for safety; a more expressive engine would need its own safety analysis, not an incremental extension |
| PostgreSQL Row-Level Security for high-assurance tenants | 03 §42, 12 §6 | Must be designed together with connection pooling and transaction-local tenant context, not enabled partially |

---

# 6. Explicitly Not a Goal (for now)

Distinguishing "not yet" from "not planned" matters as much as the roadmap itself:

```text
A visual/no-code policy or workflow builder — the expression grammar (06 §6) is
  developer-facing by design; a no-code layer on top is a distinct product
  decision this series doesn't presume to make.

Supporting a database other than PostgreSQL — the platform leans on PostgreSQL-
  specific features (composite and partial unique indexes, jsonb, native uuidv7(),
  SKIP LOCKED for the outbox) deliberately; "database-agnostic" is not a stated goal
  anywhere in this series and adding it would weaken decisions already made for good reasons.

Cross-organization chat and cross-organization role assignments — tenant isolation (12 §6)
  is the platform's central guarantee; features that cut across it are product decisions of
  their own, not extensions.
```

---

# 7. How Priority Gets Decided

Not first-come-first-served through the tables above — three questions, in order, when it's time to pick up the next roadmap item:

```text
1. Does a real module or a real deployment need this NOW, or is it still
   speculative? (An actual third-party module wanting to live out-of-tree
   is what should trigger §5's module-sdk publishing work — not a calendar date.)
2. Does it close a named residual risk (12 §5, §10)
   or is it a pure feature addition? Closing a named risk generally outranks
   a new capability.
3. Is the prerequisite work actually done? (WebAuthn's UI space assumes MFA
   patterns are already proven via TOTP — building passkeys first would mean
   designing MFA UX twice.)
```

---

# 8. Document Series Status

For reference — everything this roadmap builds on:

```text
00  Errata & Amendments            ✓ (Rev 1.1: all items applied to 01–20; kept as the change record)
01  Product Requirements             ✓
02  Domain Model                      ✓
03  Database ERD                       ✓
04  Database Schema                     ✓
05  Authentication Design                ✓
06  Authorization Engine                  ✓
07  Module System                          ✓
08  API Specification                       ✓
09  WebSocket / Realtime                     ✓
10  Backend Architecture                      ✓
11  Frontend Architecture                      ✓
12  Security Threat Model                       ✓
13  Audit Logging                                 ✓
14  Observability                                  ✓
15  Docker & Native Development                     ✓
16  Makefile Specification                            ✓
17  Module Developer Guide                             ✓
18  Testing Strategy                                    ✓
19  CI/CD                                                ✓
20  Roadmap                                               ✓ (this document)
21  Milestone Plan                                         ✓ (build order for v1.0)
22  Handover Protocol                                       ✓ (how sessions hand over)
23  Prompt Library                                           ✓ (prompts for each session)
```
