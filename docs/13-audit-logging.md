# Audit Logging: Activities, Security Events & System Logs
## Open Source Modular Application Platform v1.0

**Document:** 13-audit-logging.md
**Status:** Baseline (Rev 1.1)
**Previous:** 12-security-threat-model.md
**Next:** 14-observability.md
**Builds on:** 02-domain-model.md (§28–29, §32), 03-database-erd.md (§23, §32–33), 05-authentication-design.md (§14), 06-authorization-engine.md

---

# 1. Purpose

Three tables, three audiences, three purposes. Conflating any two of them is the mistake this document exists to prevent:

| Table | Records | Who can see it | Example |
|---|---|---|---|
| `activities` | Business/user actions | Scoped by role (§4) | "Budi updated Invoice INV-001" |
| `security_events` | Auth-specific events | The affected user (own history) and Super Admin | "Failed login from 203.0.113.4" |
| `system_logs` | Infrastructure/operational events | **Super Admin only** | "Redis connection pool exhausted" |

`system_logs` is not a slower or more technical version of `activities` — it doesn't contain user actions at all. A user opening an invoice is an Activity. A worker job timing out while sending that user's notification email is a System Log. They are different domains with different retention, different audiences, and (per 02 §34 invariant 10) different access rules enforced at the database-role level, not just the API level.

---

# 2. Activity Log

## 2.1 What gets recorded

Every state-changing operation, and every access to something sensitive enough to warrant a trail, produces one `activities` row (03 §32):

```text
actor_user_id, actor_type, action, resource_type, resource_id,
scope_type, scope_id, result, ip_address, user_agent,
request_id, trace_id, metadata, created_at
```

`action` follows the same `<resource>.<verb>` shape as permissions:

```text
user.login
user.password_changed
invoice.created
invoice.updated
invoice.deleted
invoice.approved
message.read
role.assigned
permission.changed
group.member.activated
```

## 2.2 How modules emit activities without reimplementing logging

A shared recorder, injected into every application service — not something each module hand-rolls:

```go
type AuditRecorder interface {
    Record(ctx context.Context, entry ActivityEntry)
}

type ActivityEntry struct {
    Action       string
    ResourceType string
    ResourceID   uuid.UUID
    Result       string // "success" | "denied" | "failed"
    Metadata     map[string]any
}
```

`ActorUserID`, `OrganizationID`, `ScopeType`/`ScopeID`, `IPAddress`, `UserAgent`, `RequestID`, and `TraceID` are filled in by the recorder itself from the request context — a module author writes `audit.Record(ctx, entry)` and never has to know how to extract an IP address or a trace ID correctly. This is also what guarantees consistency: every module's activities have the same shape, so the "All Activities" view in §4 doesn't need per-module rendering logic.

**Atomicity.** When called inside `TxManager.WithinTx`, the activity is written **in the same transaction** as the state change it describes (10 §6.1): both commit or neither does, and for sensitive actions a failure to write the audit row fails the operation. Activities with `result = denied` or `failed` are written in their **own** transaction (the main one never began or is rolled back), so refused attempts still leave a trace.

## 2.3 What never goes in `metadata`

```text
passwords, access tokens, refresh tokens, session tokens, reset/invitation tokens,
session secrets, encryption keys, raw MFA secrets, full credit card numbers,
any field the resource type marks as a restricted field
(06 §7) unless the actor's own access to
it was itself the point of the log entry
```

The recorder applies a **redaction list** (key names such as `password`, `token`, `secret`, `authorization`, plus module-declared sensitive fields) before writing, and a property-based test proves no listed value ever reaches `metadata` (§11). A password-change Activity records *that* it happened, not the password. An unauthorized-field-access attempt (a Denial) may legitimately record *which field* was attempted, since that's the security-relevant fact — but never the field's value.

## 2.4 Append-only, for real

"Append-only" is a promise the application layer alone can't fully keep — anyone with a database console could still `UPDATE activities`. Two layers, both required:

- **Application layer**: no `UPDATE`/`DELETE` code path exists for `activities`/`security_events`/`system_logs` in request-handling code — not even for admins. The only mutation paths are the two named maintenance jobs in §6 and §7.
- **Database layer**: the runtime role `app_user` has `INSERT`/`SELECT` only on these three tables; `UPDATE`/`DELETE` are revoked at the grant level (04 §13). The maintenance role `app_maintenance` holds the minimum needed for those two jobs — `DELETE` for retention and `UPDATE (actor_user_id, metadata)` on `activities` (plus a short column list on `security_events`) for anonymization — and its connection (`MAINTENANCE_DATABASE_URL`) exists only in the worker. If a bug in request code ever tried to modify a row, the database rejects it, not just the application logic.

---

# 3. Security Events

Introduced in 05 §14 as the auth-specific subset. This document adds only the retention and visibility posture: security events are visible to the affected user as their own login history, and to a Super Admin globally — they follow the same scoping rules as Activities (§4), they simply live in a separate table because they're append-only in the same way but conceptually about the *authentication* boundary rather than business actions. 05 §14 gives their exact event catalog (`auth.login.success`, `auth.mfa.failed`, etc.), so it isn't repeated here.

Like activities, security events are written in the same transaction as the auth change they describe, except failed attempts (written independently).

---

# 4. Visibility Rules

This is the part spelled out precisely because "who sees what" is where a logging system either earns trust or leaks data.

| Role | "All Activities" view | "My Activity" view |
|---|---|---|
| **Super Admin** | Every activity, every organization (platform-level decision) | Only rows where `actor_user_id = self` |
| **Organization Admin** | Every activity in the organization | Only rows where `actor_user_id = self` |
| **Group Admin** | Activities scoped to the group(s) they administer | Only rows where `actor_user_id = self` (even if that action happened outside their managed group) |
| **User** | *(no "all" view — only their own)* | Only rows where `actor_user_id = self` |

The two columns are genuinely two different queries, not one query with a UI toggle that trims the same data — they answer different questions ("what happened in my domain" vs. "what did *I* do"), and a Super Admin's own login is an Activity too, one that should never get lost inside "everything everyone did today." Concretely:

```text
-- "All Activities", Group Admin scope (activity.read_group)
WHERE organization_id = :org_id
  AND scope_type = 'group' AND scope_id IN (:managed_group_ids)

-- "All Activities", Organization Admin (activity.read_all at organization scope)
WHERE organization_id = :org_id

-- "All Activities", Super Admin (activity.read_all at platform scope) — the explicit
-- platform-level repository method (10 §13); optional organization filter
WHERE (:org_id IS NULL OR organization_id = :org_id)

-- "My Activity", any role
WHERE actor_user_id = :current_user_id
```

The same authorization engine that gates every other resource (06) gates this query — a Group Admin's "All Activities" is filtered server-side by their actual managed-group scope, never by a client-supplied group list. Only the platform-level path may run without an `organization_id`.

`system_logs` has no equivalent "scoped" view — it is Super Admin-only, full stop, because it contains no per-user or per-organization concept to scope by in the first place (it's about services, not actors).

---

# 5. System Log

```text
system_logs (id, service, environment, level, event_code,
             message, request_id?, trace_id?, metadata, created_at)
```

- `level`: `debug | info | warn | error | fatal`.
- `service`: which component logged it (`api`, `worker`, `websocket-hub`).
- Structured JSON at write time — `message` is human-readable, `metadata` carries the structured fields, so the same record works for a human scanning the Super Admin UI and for a future export to Loki/OpenSearch without reformatting.
- Correlated to a specific request/trace via `request_id`/`trace_id`, so a Super Admin investigating a bug report can go from "this activity failed" (Activity) → "here's the trace_id" → "here's what the system was doing at that moment" (System Log), without the two tables ever needing to reference each other by foreign key.
- Only the **curated subset** of log lines is written here (warnings and errors worth surfacing in the product, dead-lettered outbox events, degraded-mode notices); the full firehose goes to stdout (14 §3).

```text
GET /api/v1/system-logs?level=error&service=worker&since=...
```

Super Admin only — enforced the same way `activities`' scoped views are: through the authorization engine (`system_log.read`), not a separate ad-hoc admin-check.

---

# 6. Retention

```text
activities        — long retention (business/compliance driven, configurable per deployment)
security_events   — long retention
system_logs       — shorter, configurable (these are operational, not evidentiary)
outbox_events     — processed rows purged after a short window (default 7 days); dead rows kept until reviewed
chat messages     — business-configurable
```

Retention is a **platform setting** (scope `platform`, 02 §20), not a hardcoded constant — a healthcare or finance deployment will need years; a hobby deployment may want 90 days of system logs and nothing more.

**How purging happens:** a scheduled worker job running on the **maintenance** connection deletes expired rows in small batches (for example 5,000 rows per statement, ordered by `created_at`) so it never holds long locks, and records each run as a system log entry. Partitioning of the large tables is deliberately deferred (20 §4); if it is introduced later, the retention job becomes a partition drop.

---

# 7. Audit vs. the Right to Be Forgotten

A real tension worth naming rather than glossing over: `activities` are append-only and (per 02 §2.5) immutable, but a deployment operating under a data-protection regime may need to honor a user-deletion request that would otherwise mean rewriting history.

Resolution: user deletion never deletes `activities` rows. It **anonymizes the actor reference**:

```text
actor_user_id → NULL (or a stable "deleted-user" tombstone id)
metadata      → strip any embedded PII (display name, email) that had been
                 denormalized into metadata for readability
security_events: user_id, ip_address, user_agent, metadata → cleared likewise
```

— while the row itself (`action`, `resource_type`, `resource_id`, `created_at`, `result`) survives, because *that an action happened* is often the actual compliance-relevant fact, independent of *who* did it once they're gone.

This is a documented, deliberate procedure (`user.anonymize`), executed **only by the worker's maintenance connection** (`app_maintenance`, whose `UPDATE` is limited to those named columns). It is itself audited (an activity `user.anonymized` written by the runtime role, with no PII), requires `user.delete` plus an explicit confirmation in the request, and is never a hand-run `DELETE` or `UPDATE` by a support engineer.

---

# 8. Export

```text
GET /api/v1/activities/export?format=csv&scope=...
```

Reuses the exact same scoping as the on-screen view (§4) — an export can never contain rows the requester couldn't otherwise see on screen. Exports are handed to the worker rather than generated synchronously in the request/response cycle: the request creates an export job and returns its id; the worker streams the CSV (applying field permissions, 06 §7) to storage, and the requester is notified (a Notification) with an authorized download link. CSV cells are escaped against formula injection (a leading `=`, `+`, `-`, `@` is neutralized). `activity.export` is required in addition to the read permission.

---

# 9. Error Model

```text
activity_access_denied
system_log_access_denied
export_scope_exceeded
invalid_date_range
```

---

# 10. Package Structure

```text
internal/
├── audit/          # Activity domain, AuditRecorder, scoped queries, export, retention/anonymize jobs
└── systemlog/      # System log domain, write path used by internal services only
```

`internal/systemlog` is written to by other internal packages (a repository failing, a worker job erroring) — it has no meaningful "create via API" path from outside the platform itself.

---

# 11. Testing Requirements

```text
Visibility
  User cannot see another user's activity
  Group Admin cannot see activity from a group they don't manage
  Organization Admin cannot see another organization's activity
  Group Admin's own action, even inside their managed group, appears in
    "My Activity" AND in "All Activities" — not one or the other
  Normal user cannot access /system-logs regardless of any role they hold
    that isn't Super Admin
  Only the platform-level path can query activities without an organization_id

Integrity
  Attempting UPDATE/DELETE on activities at the runtime database role level fails
  A state change that rolls back leaves no activity row; a denied attempt DOES leave one
  A sensitive action fails if its audit row cannot be written (fail-closed)
  No sensitive field (§2.3) ever appears in metadata (property-based test
    over the redaction list)

Anonymization & retention
  user.anonymize (maintenance connection) clears actor_user_id and PII from metadata but
    preserves the activity row and its timestamp/action/result
  the maintenance role cannot update any column outside its granted list
  retention deletes only rows older than the window, in bounded batches

Export
  export respects the same scope as the equivalent on-screen query
  export never contains a field the requester's field permissions hide
  CSV cells starting with = + - @ are neutralized
```

---

# 12. Critical Rules

```text
1. activities, security_events and system_logs are append-only for the runtime role at the
   database grant level, not only by application convention.
2. system_logs contains no user activity and is Super Admin-only.
3. "All Activities" and "My Activity" are always two distinct query paths,
   for every role, including Super Admin.
4. Sensitive values are never written to metadata, ever — not hashed,
   not truncated, not present.
5. Deleting a user anonymizes their activity trail; it never deletes it — and only the
   maintenance connection may do it.
6. An export can never surface a row the requester's on-screen view wouldn't.
7. An activity is committed atomically with the change it describes; a refused attempt is
   recorded independently.
```
