# Authorization Engine
## Open Source Modular Application Platform v1.0

**Document:** 06-authorization-engine.md
**Status:** Baseline (Rev 1.1)
**Previous:** 05-authentication-design.md
**Next:** 07-module-system.md
**Builds on:** 02-domain-model.md (§12–17), 03-database-erd.md (§12–16)

---

# 1. Purpose

Authentication answers **"who is this?"**. Authorization answers a harder question:

> Can this identity perform this action, on this resource, under this scope, right now?

The Authorization Engine is the single component every protected operation must pass through. No module, handler, or query is allowed to make its own ad-hoc permission decision — they all call the engine.

Default posture:

```text
No matching ALLOW → DENY
```

There is no implicit trust. Being authenticated, being an Admin, or being a member of a group grants nothing by itself.

---

# 2. The Authorization Chain

```text
Identity
   │
   ▼
Organization Membership   (is this identity even in this org?)
   │
   ▼
Group Membership           (which groups, in this org?)
   │
   ▼
Role Assignment            (which roles, at which scope?)
   │
   ▼
Role → Permission          (what can that role do?)
   │
   ▼
Scope                      (does the assignment's scope cover this request?)
   │
   ▼
Policy                     (any conditional rule that applies?)
   │
   ▼
Field Permission           (which fields may be read/written?)
   │
   ▼
ALLOW / DENY
```

Every link in this chain can independently turn the result into DENY. None of them can independently force ALLOW — an ALLOW only happens when every link agrees.

---

# 3. Inputs to the Engine

The engine never re-derives identity. It receives it from Authentication (05):

```go
type Identity struct {
    UserID         uuid.UUID
    OrganizationID *uuid.UUID // nil only for a pure platform-level identity
    SessionID      uuid.UUID
    AuthLevel      AuthLevel
}
```

To this, the Authorization layer adds a **Scope Context**, resolved per request:

```go
type ScopeContext struct {
    OrganizationID *uuid.UUID // nil for a platform-level request (e.g. Super Admin managing organizations)
    ActiveGroupID  *uuid.UUID // which group this request is "acting as", if any
}
```

A nil `OrganizationID` is valid **only** for requests that are platform-level by nature; every tenant-scoped handler requires a non-nil value and rejects the request otherwise (`scope_mismatch`).

The check itself is a simple, explicit call — nothing implicit:

```go
type Check struct {
    Identity Identity
    Scope    ScopeContext
    Action   string          // e.g. "invoice.approve"
    Resource *ResourceRef    // nil for list/create-type actions
}

type ResourceRef struct {
    Type           string    // "invoice"
    ID             uuid.UUID
    OrganizationID *uuid.UUID // the resource's own organization — always compared with the scope
    OwnerID        *uuid.UUID
    GroupID        *uuid.UUID
    Attrs          map[string]any // fields a Policy expression may need, e.g. "status"
}

type Decision struct {
    Allowed bool
    Reason  string // for the Permission Simulator and audit trail
    Level   DecisionLevel // "platform" | "organization" | "group" | "resource" | "own" — the scope that granted it
}
```

`Decision.Level` matters for repositories: only a decision at `platform` level permits a platform-wide (cross-tenant) query method (10 §13, 12 §6).

---

# 4. Organization Context vs. Active Group

These two are resolved differently, and the difference is intentional.

## 4.1 Organization is session-level

`Identity.OrganizationID` is fixed for the life of a session. A user working across multiple organizations switches context by calling:

```text
POST /api/v1/auth/switch-organization
```

which issues a **new session** (new token, 05 §12.2) scoped to the target organization (subject to the user having an active `organization_membership` there). This keeps every request inside one session unambiguous about which tenant it belongs to — there is never a request where "which organization?" is in doubt.

## 4.2 Group is request-level

A user frequently belongs to several groups inside the same organization (Finance **and** Events, say). Re-authenticating every time someone switches working context would be hostile UX. Instead, the frontend sends the group it is currently "acting as":

```text
X-Active-Group-ID: <group_id>
```

The backend **never trusts this header on its own** — it is only a hint. On every request the server verifies:

```text
group_memberships.status = 'active'
  AND group_memberships.user_id    = identity.user_id
  AND group_memberships.group_id   = header.value
  AND groups.organization_id       = identity.organization_id
  AND groups.status = 'active' AND groups.deleted_at IS NULL
```

If verification fails, the header is discarded and the request proceeds with no active group (group-scoped actions will then correctly DENY rather than silently picking a group for the user).

If a request touches a resource that unambiguously belongs to one group (`resource.group_id`), the engine may use that instead of trusting the header — the header is a **default for ambiguous actions** (e.g. "create a new invoice" — which group does it belong to?), not an override for already-scoped resources.

---

# 5. Role, Permission, Role Assignment — Runtime View

The shapes are defined in 02-domain-model.md §12–15 and 03-database-erd.md §12–15. This document adds the *runtime* behavior.

## 5.1 Permission codes

```text
<resource>.<action>
```

```text
invoice.read
invoice.create
invoice.update
invoice.delete
invoice.approve
invoice.field.internal_cost.read
```

Field permissions reuse the same namespace with a `.field.<name>.<read|write>` suffix rather than being a separate concept the engine has to special-case — this keeps the permission catalog, the permission simulator, and the role editor UI all working off one flat list.

## 5.2 Effective Permission Set

For a given `(Identity, ScopeContext)`, the engine computes one thing first and reuses it for every check in the request:

```text
EffectivePermissions(identity, scope) =
  ⋃ { role.permissions
      | assignment ∈ role_assignments
      , assignment.subject ∈ {identity.user_id}
                             ∪ {groups where identity has an ACTIVE group_membership
                                (in identity's organization)}
                             ∪ {identity.organization_id}
      , assignment.revoked_at IS NULL
      , assignment.expires_at IS NULL OR assignment.expires_at > now()
      , role.status = 'active'
      , ScopeApplies(assignment, scope) }
```

An assignment whose subject is a **group** (`subject_type = 'group'`) is inherited by every *active* member of that group; where it applies is still decided by its `scope_type`/`scope_id`. An assignment whose subject is the **organization** applies to every active organization member.

`ScopeApplies` per `scope_type`:

| scope_type | Applies when |
|---|---|
| `platform` | Always (platform-global authority) |
| `organization` | `scope_id IS NULL OR scope_id = scope.OrganizationID` |
| `group` | `scope_id = scope.ActiveGroupID` (after the verification in §4.2) |
| `resource` | Evaluated per-resource at check time, not part of the cached set (§8) |
| `own` | Evaluated per-resource at check time (`resource.OwnerID == identity.UserID`) |

`resource` and `own` scoped assignments are deliberately **excluded** from the cached Effective Permission Set — they can't be resolved without knowing which resource is being touched, so they're evaluated at check time (§6).

## 5.3 Authority ceiling — admins cannot escalate

> **As built (M04, ADR-0022 #47-#49, #53-#58):** the granter's set is computed from PostgreSQL *inside* the granting transaction, never from the cache. On `PUT /roles/{id}/permissions` the ceiling is checked against the union of the old and the new set, and it also applies to renaming and archiving a role and to revoking an assignment (a Group Admin cannot revoke an organization-level assignment). A group-level granter covers only the active group. A platform target is reachable only from platform scope; a tenant target only from the caller's organization; a Super Admin never grants inside an organization from platform scope and must enter it first through `switch-organization`. Platform assignments carry no `expires_at`, the only platform subject is `user`, and subject type `service` is rejected until it is supported. The last active Super Admin assignment cannot be revoked. Every writer takes the organization advisory lock (platform lock for platform scope and global policies) as the first step of its transaction (#54); any new writer MUST do the same or the ceiling-then-write gap reopens.


An Admin granting a role or permission must never be able to grant more than their own authority:

```text
Grantable(admin, target_permissions, target_scope) :=
  target_permissions ⊆ EffectivePermissions(admin, admin's own equivalent scope)
  AND target_scope ⊆ admin's own scope boundary
```

Concretely: a Finance Group Admin whose own permissions are scoped to `group=Finance` can create roles and assign permissions **only** within `group=Finance`, and only from the subset of permissions the Admin itself holds. This must be enforced at the *service* layer, inside the granting transaction, not just the UI — the UI hiding an option is not a security control (05 §22, principle carried over unchanged: never trust the frontend).

Invitations with a preset role (05 §13.3) are checked against the inviter's authority **at acceptance time** as well.

## 5.4 Core permission catalog (declared in code)

The core permissions are declared in `internal/permission/catalog.go` — not in SQL seeds — and upserted by `code` on every startup with `is_system = true`. The sync never deletes a permission; a removed permission is reported and handled by a migration.

```text
organization   organization.read  .create  .update  .archive  .member.manage
user           user.read  .create  .update  .delete
profile        profile.read  .update                       (own scope)
group          group.read  .create  .update  .delete  .manage_members
role           role.read  .manage  .assign
permission     permission.read  .assign  .simulate
policy         policy.read  .manage
module         module.read  .manage  .enable
settings       settings.read  .update
activity       activity.read_own  .read_group  .read_all  .export
system_log     system_log.read
notification   notification.read                           (own scope)
inbox          inbox.read                                  (own scope)
conversation   conversation.read  .create  .manage
message        message.send  .moderate
```

`activity.read_all` means "everything in the caller's organization" when held at organization scope and "everything on the platform" when held at platform scope. Business modules add their own permissions through their manifest (07 §7).

## 5.5 System roles and their default permission sets

> **As built (M04, ADR-0022 #47, #53):** the allowed scope per system-role template is a table in code. The four system-role slugs are reserved, and the permissions of a system role cannot be changed through the API. Role slugs match `^[a-z0-9]+(-[a-z0-9]+)*$` (at most 100 characters) and cannot be renamed. The canonical form of an organization-scope assignment stores `scope_id` as NULL (docs/02 §15).


Seeded from code (idempotent upsert by slug) with these defaults; an organization may clone and adjust them as custom roles.

| Role | Boundary | Default permissions |
|---|---|---|
| **Super Admin** | `system`, assigned at `platform` scope | every `is_system` permission |
| **Organization Admin** | `system` template, assigned at `organization` scope | `organization.read/update/member.manage`, `user.*`, `group.*`, `role.*`, `permission.*`, `policy.*`, `module.read/enable`, `settings.*`, `activity.read_all/export/read_own`, plus the Member set |
| **Group Admin** | `system` template, assigned at `group` scope | `group.read/update/manage_members`, `user.read`, `role.read/manage/assign` (subject to the authority ceiling), `permission.read/assign/simulate`, `activity.read_group/export`, plus the Member set |
| **Member** | `system` template, assigned at `organization` scope | `profile.read/update`, `notification.read`, `inbox.read`, `conversation.read/create`, `message.send`, `activity.read_own` |

The authority ceiling (§5.3) is what stops "Group Admin holds `role.manage`" from meaning "can grant anything": they can only grant what they hold, inside their own group.

## 5.6 Self-service endpoints

Endpoints that act only on the caller's own records — `GET /auth/me`, `/security/*` (own sessions, own login history, own password), `GET /activities/me`, own notifications and inbox — need **authentication only**, because the `own` scope is satisfied by construction (the handler queries `WHERE user_id = identity.user_id`). They still write activities and never take a user id from the client.

---

# 6. Policy Layer

> **As built (M04, ADR-0022 #30-#34, #50, #59-#61):** a policy can only *restrict*: `deny` refuses, `allow` is an additional condition, and an evaluation error refuses (`policy_evaluation_failed`, 403). A policy that needs resource attributes is deferred when the check carries no resource (`Decision.DeferredPolicies`); resource-level callers MUST apply it. The expression language lives in `internal/policy` (two-segment roots `resource`, `current_user`, `scope`; allow-listed functions `lower`, `len`, `startsWith`, `endsWith`, `contains`; strict typing; hard limits; no `group()` and no `has()`). Writes validate that `actions` exist in the catalog, cap active policies at 50 per organization plus 50 global, and refuse any policy that targets a `policy.*` action (otherwise a `deny` could lock out its own manager). A write needs organization scope or platform scope without an active group. `PATCH` never changes `code`, `null` means unchanged, disabling is always allowed, and there is no `DELETE`. Policies are not cached. Clients always see a policy refusal as `permission_denied` (#32e); the policy code appears only in the simulator (for holders of `policy.read`) and in the `authz.access_denied` audit metadata.


Policies add conditional logic on top of a raw permission grant.

```text
invoice.status != "closed"
resource.owner_id == current_user.id
amount <= approval_limit
current_user in group("finance")
```

Rules:

1. Policies execute **server-side only**, using a small, sandboxed expression grammar (comparisons, boolean logic, field lookups against `ResourceRef.Attrs` and `Identity`) — a hand-written tokenizer and parser with an allowlist of functions, never a general-purpose scripting language, and never `eval`-style arbitrary code. This is a hard line: a policy engine that can execute arbitrary code is a remote-code-execution feature with a permission-editor UI on top of it. Expression length, nesting depth and evaluation steps are bounded, and the parser is fuzz-tested.
2. A `policies` row's `effect` is `allow` or `deny`. A matching `deny` policy always wins, even over a permission the role otherwise grants.
3. Policy evaluation for sensitive actions (anything touching `approve`, `delete`, financial fields, or permission/role changes themselves) is recorded as part of the Activity for that request (13) — not just the final decision, but *which policy fired*.
4. Because policies can depend on resource state (`invoice.status`), they are evaluated fresh on every request. They are never part of the cached Effective Permission Set.
5. `resource` and `own`-scoped assignments are resolved here, at check time, against `ResourceRef`.
6. A resource whose `OrganizationID` differs from `ScopeContext.OrganizationID` is an immediate DENY (`scope_mismatch`) — unless the decision is platform-level.

---

# 7. Field-Level Permissions

> **As built (M04, ADR-0022 #35-#45, #77):** field permissions are catalog codes `<type>.field.<name>.<read|write>` (the third segment from the right, `field`, is reserved); restriction is per action and the default is deny. `FieldGuard.For(ctx, identity, scope, type)` returns a `FieldFilter` with `Apply(ref, obj)` (a new map without restricted unreadable fields) and `CheckWrite(ref, obj)` (a `*FieldDeniedError`); errors are returned rather than hidden, which departs from the signature sketched here. Matching fails closed (non-ASCII keys require every restricted code), covers top-level keys only, and loads definitions from PostgreSQL on each filter creation. `authzhttp.FieldHelper` is the handler-side helper: `DecodeWrite` checks the *raw* keys before decoding into a struct (then `DisallowUnknownFields`), and `FilterRead` filters one object. A refusal is `403 field_permission_denied` without the field name; refusals are logged, not audited. The core catalog declares no field codes yet and no production handler uses the helper (first user: M05).


Field permissions are enforced **during serialization**, after the object is loaded and before it leaves the server — not by the frontend deciding what to render.

```go
type FieldGuard interface {
    Filter(ctx context.Context, identity Identity, scope ScopeContext, resourceType string, obj map[string]any) map[string]any
}
```

A Finance Staff user requesting an invoice gets:

```json
{ "id": "...", "number": "INV-001", "customer": "ABC", "amount": 1000000 }
```

The engine drops `internal_cost` entirely — the key is absent, not null, not masked. This applies identically to:

- REST responses
- WebSocket event payloads (09)
- CSV/export jobs run by the Worker
- Any background job that might otherwise re-serialize the same object with fewer restrictions than the original request had

A field's write permission is checked independently of its read permission — a role may be allowed to *see* `salary` but not *change* it, or vice versa. The write check rejects an over-posted restricted field with `field_permission_denied` rather than silently ignoring it.

---

# 8. Caching & Invalidation

The Effective Permission Set from §5.2 is the expensive part (multiple joins across `role_assignments` → `roles` → `role_permissions` → `permissions`) and the part that's safe to cache, because it doesn't depend on any single resource's state.

## 8.1 Versioned keys (O(1) invalidation)

> **As built (M04, ADR-0022 #16-#20, #28):** invalidation is called explicitly inside the mutating transaction, before and after commit, not through the event bus. The key version is the pair (organization counter, platform counter); changing a system role bumps the platform counter; versions derive from the clock. TTL is 2 minutes. The catalog `Syncer` bumps the platform counter around its commit when anything changed; a failed bump is logged and does not stop startup. Without Redis the cache and the invalidator are nil interfaces and every check reads PostgreSQL.


A per-organization version counter makes invalidation a single `INCR`, with no key scanning and no need to know which users a changed role affects:

```text
authz:ver:{organization_id}                                    → integer, INCR on any authorization change
authz:eff:{organization_id}:{version}:{user_id}:{active_group_id|-}   → the effective permission set
```

A read fetches the current version first (one `GET`), then the set under that version. After an `INCR`, every old key is simply never read again and expires by its short TTL.

- **Written** on first computation per request, short TTL (a few minutes) — a safety net, not the primary mechanism.
- **Invalidated immediately** by bumping `authz:ver:{org}` — done *synchronously inside the request that changed the data, right after commit*, via an in-process handler on the event bus. The cache is in Redis, which every API instance shares, so the in-process delivery is sufficient; no cross-process event is needed for correctness. For platform-level assignments the platform counter `authz:ver:platform` is bumped and included in every key.

Events that bump the version:

```text
role_permissions.changed
role_assignment.created / .revoked
group_membership.changed
organization_membership.changed
user.status_changed
module.enabled / module.disabled
policy.changed
role.archived
```

Because bump-after-commit has a short window in which another request could re-cache stale data under the *old* version, the changing transaction also bumps the version **before** commit (a bump that is harmless if the transaction then rolls back) and once more after. A revoked permission must never stay effective beyond the request that revoked it.

## 8.2 Redis unavailable

The effective set is computed from PostgreSQL on every request (slower, always correct). The engine never serves a stale or default-allow answer because the cache is down — **fail-safe, not fail-open**.

Policies and field permissions are **not** cached (§6, §7) — only the role → permission resolution is.

---

# 9. Middleware & Composition with Authentication

> **As built (M04, ADR-0022 #25, #70):** the protected route order is `Authenticate -> CSRF -> RequireAuth -> Scope -> Authorize(action)`; `Scope` also fills `kernel.Scope` so activities carry the scope. `Authorize` answers 401 without an identity and 500 without a scope; an engine error always wins over a decision (`ErrInvalidCheck`/`ErrInvalidScope` = 500, infrastructure = 503, never ALLOW); a refusal is `403 permission_denied` (`scope_mismatch` has its own code) and writes the `authz.access_denied` activity. Authorization routes are mounted only when the kernel toolkit, authentication and the middleware exist; otherwise they answer 404.


```text
HTTP Request
     │
     ▼
Authentication  →  Identity
     │
     ▼
Resolve Scope   →  ScopeContext (org from session, group from X-Active-Group-ID + verification)
     │
     ▼
Authorize(Check{Identity, Scope, Action, Resource}) →  Decision
     │
     ├── DENY → 403 permission_denied  (audited, in its own transaction)
     │
     ▼
Handler (receives Identity + Scope, never re-derives them)
```

```go
type Authorizer interface {
    Can(ctx context.Context, check Check) (Decision, error)
}
```

Routes declare the action they require (`Authorize("invoice.read")` in core, `modulesdk.Require("invoice.read")` in modules, 07 §5); the middleware performs the generic "may this caller reach this endpoint at all" check. Resource-specific checks happen inside the application service with the loaded resource (10 §6). Handlers depend only on `Authorizer`. They never query `role_assignments` directly, and they never accept a client-supplied "is this allowed" flag.

---

# 10. Permission Simulator

> **As built (M04, ADR-0022 #69, #75):** `POST /api/v1/permissions/simulate` calls the real `Authorizer.Can` with the target's identity in the caller's organization; there is no parallel implementation, and the assembly refuses to start with an authorizer that has no policies. The target must be an active member of the caller's organization; anything else is a uniform `400 validation_failed` on `user_id` (`not_found`). A platform caller gets `403 scope_mismatch`; a group-level caller is locked to their group; for an organization-level caller, groups that are not the target's memberships are dropped as the production header would. The request is `{user_id, action, active_group_id?, resource?{type,id,owner_id,group_id,attrs}}` with bounded attributes (at most 32, reserved core names). The response is `{result: ALLOW|DENY, reason, level, policy_code, deferred_policies}`: `reason` is a stable code rather than a sentence, there is no `evaluated`, and `policy_code`/`deferred_policies` are shown only to callers who also pass `Can(policy.read)`. The audit action `permission.simulate` is written outside the transaction and carries no attribute values. The answer describes the state now, not a guarantee for a later request; rate limiting is deferred to M15.


Directly useful for support and for Admins debugging their own configuration:

```text
POST /api/v1/permissions/simulate
{
  "user_id": "...",
  "action": "invoice.approve",
  "resource": { "type": "invoice", "id": "..." },
  "active_group_id": "..."
}
```

```json
{
  "data": {
    "result": "DENY",
    "reason": "role Finance Staff does not include invoice.approve",
    "evaluated": {
      "roles": ["Finance Staff"],
      "scope_checked": "group:Finance",
      "matched_assignments": [],
      "policies_evaluated": []
    }
  }
}
```

The simulator runs through the exact same `Authorizer.Can` code path as production requests — it must never be a separate, parallel implementation that can drift from the real one. It is itself gated (`permission.simulate`) and can only simulate users within the caller's own administrative scope.

---

# 11. REST API Surface

> **As built (M04, ADR-0022 #62-#68, #70-#76):** all 15 routes are mounted under `/api/v1`. Actions: `role.read` for `GET /roles`, `GET /roles/{id}`, `GET /roles/{id}/permissions` and `GET /role-assignments`; `role.manage` for `POST/PATCH/DELETE /roles`; `permission.assign` for `PUT /roles/{id}/permissions`; `permission.read` for `GET /permissions`; `role.assign` for `POST/DELETE /role-assignments`; `policy.read` for `GET /policies`; `policy.manage` for `POST/PATCH /policies`; `permission.simulate` for simulate. Lists are `{data: [...], meta: {pagination}}`; roles, permissions and policies use offset pagination (08 §4), role-assignments use a cursor. `GET/PUT /roles/{id}/permissions` use `{permissions: [...]}`. `GET /roles` shows system roles plus the caller's organization roles (platform scope: system roles only) with filter `status` (`active` by default); `PATCH /roles/{id}` reads the effective values, merges and writes (two concurrent PATCHes on different fields can overwrite each other). `GET /policies` defaults to status `all` and shows global policies marked `global`, without `organization_id`. `POST /role-assignments` requires `Idempotency-Key` (08 §6) and is checked after `Authorize`; a platform caller asking for an organization or group scope gets `403 scope_mismatch`. `DELETE /role-assignments/{id}` on a platform-scope assignment revokes every session of the subject user after commit (activity `role_assignment.sessions_revoke`; a failure is logged and audited and does not roll back the revocation). Request bodies never carry `organization_id` or a scope; the organization comes only from `ScopeContext`.


```text
GET    /api/v1/roles
POST   /api/v1/roles
GET    /api/v1/roles/{id}
PATCH  /api/v1/roles/{id}
DELETE /api/v1/roles/{id}

GET    /api/v1/roles/{id}/permissions
PUT    /api/v1/roles/{id}/permissions

GET    /api/v1/permissions

GET    /api/v1/role-assignments
POST   /api/v1/role-assignments
DELETE /api/v1/role-assignments/{id}

GET    /api/v1/policies
POST   /api/v1/policies
PATCH  /api/v1/policies/{id}

POST   /api/v1/permissions/simulate
```

Every one of these endpoints is itself gated by the same engine (`role.read`, `role.manage`, `role.assign`, `permission.assign`, etc.) — the authorization system is not exempt from authorization. `POST /role-assignments` requires `Idempotency-Key` (08 §6).

---

# 12. Error Model

> **As built (M04, ADR-0022 #62):** statuses: `role_not_found` 404, `permission_not_found` 422, `grant_exceeds_authority` 403, `invalid_scope` 400, `scope_mismatch` 403, `policy_evaluation_failed` 403, `field_permission_denied` 403. Eight codes beyond this list are 409: `role_slug_taken`, `role_immutable`, `role_archived`, `assignment_exists`, `assignment_revoked`, `last_super_admin`, `policy_code_taken`, `policy_limit_reached`. `resource_not_found` is used for a missing assignment or policy. Another organization's role is indistinguishable from a missing one; a missing subject or group is a `validation_failed` on `subject_id`/`group_id`. Responses carry no client values, permission lists, indexes or expression positions. Any new domain sentinel MUST be added to `programmingErrors`, `codeRules` or `fieldRules` in `authzhttp.DomainError`, otherwise it falls through to 503.


```json
{
  "error": {
    "code": "permission_denied",
    "message": "You do not have permission to perform this action.",
    "request_id": "req_01..."
  }
}
```

```text
permission_denied
invalid_scope
scope_mismatch
role_not_found
permission_not_found
grant_exceeds_authority
policy_evaluation_failed
field_permission_denied
```

`grant_exceeds_authority` is deliberately distinct from `permission_denied` — it's specifically what an Admin sees when trying to hand out more than they hold (§5.3), and is worth its own code for clear UI messaging and for audit search.

---

# 13. Package Structure

> **As built (M04):** `backend/internal/{permission,role,policy}` plus `backend/internal/authorization/{domain,application,repository,cache,transport/http}`. Writers (`RoleWriter`, `AssignmentWriter`, `PolicyWriter`), readers, the `Simulator`, `FieldGuard`, the `Authorizer` and `PlatformEntry` live in `application`; SQL in `repository`; HTTP, DTOs, `Scope`, `Authorize` and `FieldHelper` in `transport/http` (`authzhttp`). Assembly: `authzStack` and `httpAPI` in `internal/app/authz.go`, route mounting in `internal/httpserver/authz_routes.go`.


```text
internal/
├── role/              # domain + repository for roles
├── permission/        # domain + repository for permissions, catalog.go (§5.4), sync
├── policy/            # policy domain, tokenizer + parser + evaluator
├── authorization/     # the Engine itself: composes role+permission+policy+scope
│   ├── domain/
│   │   ├── check.go
│   │   ├── decision.go
│   │   └── errors.go
│   ├── application/
│   │   ├── authorize.go
│   │   ├── simulate.go
│   │   └── grant.go        # authority-ceiling enforcement (§5.3)
│   ├── cache/
│   │   └── effective_permissions.go   # versioned keys (§8)
│   └── transport/http/
│       ├── handler.go
│       └── middleware.go
```

`internal/authorization` is the only package allowed to import all three of `role`, `permission`, and `policy` and combine them into a decision. Nothing else should.

---

# 14. Testing Requirements

> **As built (M04):** met at function and PostgreSQL level: ceiling, policy evaluation and deny-wins, a fuzz test of the parser (`internal/policy/fuzz_test.go`), group scope not leaking across groups, platform Super Admin path, uniform cross-organization negatives in both directions for every read and write path, per-route actions with the real `Authorize`, and concurrency proofs for the advisory lock. End to end with a real server, session, PostgreSQL and Redis, `scripts/smoke/m04.sh` proves 401, 403, `scope_mismatch`, immediate effect of grant and revoke, a policy deny, the simulator's explanation, `Idempotency-Key` not replaying across organizations, and session revocation on platform revoke. Field-level tests exist at function and PostgreSQL level and through `FieldHelper`; no REST, WebSocket or export path uses them yet.


```text
Effective permission resolution
  role permission → allowed
  revoked role assignment → denied
  expired role assignment → denied
  archived role → denied
  wrong organization → denied
  wrong active group → denied
  platform-scoped role → allowed regardless of active group
  group-subject assignment inherited by an active member, not by a suspended one

Scope
  group-scoped assignment for Group A does not apply while X-Active-Group-ID = Group B
  a forged X-Active-Group-ID (not a member / other organization) is discarded
  resource-scoped assignment applies only to the exact resource
  own-scoped assignment denies access to another user's resource
  a resource from another organization → denied (scope_mismatch)
  a nil-organization scope is rejected by tenant-scoped handlers

Authority ceiling
  Admin cannot grant a permission they do not hold
  Admin cannot assign a role scoped wider than their own scope
  group-scoped Admin cannot create a role outside their own group

Policy
  deny policy overrides an otherwise-allowed permission
  policy expression cannot execute arbitrary code (fuzz/negative test)
  expression exceeding length/depth/step limits → policy_evaluation_failed, never a hang

Field permissions
  restricted field absent from REST response
  restricted field absent from WebSocket payload
  restricted field absent from export job output
  writing a restricted field → field_permission_denied

Cache
  role_permissions change takes effect on the very next request (version bump)
  revoked role_assignment takes effect on the very next request
  disabled module's permissions stop being grantable
  Redis unavailable → correct answers from PostgreSQL (fail-safe), never allow-by-default

Catalog & bootstrap
  catalog sync is idempotent and never deletes
  Super Admin holds every is_system permission after each sync
  the system-role default sets match §5.5

Simulator
  the simulator and a real request produce the same decision for the same input
```

---

# 15. Critical Rules

```text
1. Default is DENY. There is no implicit ALLOW anywhere in the chain.
2. The frontend hiding a button is UX, not a security control.
3. Every protected handler calls Authorizer.Can (directly or via the route declaration) —
   no handler invents its own check.
4. An Admin can never grant authority beyond what they themselves hold.
5. Field permissions are enforced at serialization, identically across REST,
   WebSocket, exports and background jobs.
6. Policies never execute arbitrary code.
7. A revoked permission must stop applying immediately, not after a cache TTL.
8. Knowing a resource's ID is never sufficient to access it.
9. The Permission Simulator uses production authorization code — never a
   parallel reimplementation.
10. If the cache is unavailable, compute from the database; never default to allow.
```

---

# 16. Relationship to the Module System

Permissions are not only defined by core — modules declare their own (`invoice.approve` belongs to the Finance module, not core). How a module's permissions get registered, versioned, and removed on uninstall is defined in **07-module-system.md §7**.
