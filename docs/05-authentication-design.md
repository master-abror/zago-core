# Authentication Design
## Open Source Modular Application Platform v1.0

**Document:** 05-authentication-design.md
**Status:** Baseline (Rev 1.1)
**Previous:** 04-database-schema.md
**Next:** 06-authorization-engine.md

---

# 1. Purpose

Authentication answers exactly one question:

> Who is making this request?

It does not answer *what they're allowed to do* — that's 06-authorization-engine.md. Keeping the two separate matters here specifically because this platform's permission model (role, group, scope, policy, field permission) is deliberately rich; folding authorization logic into the login flow would make both harder to reason about.

```text
Authentication: "This is Budi."
Authorization:  "Is Budi allowed to delete a user?"
```

---

# 2. Architecture

```text
Browser
   │
   │ HTTPS  (same origin for SPA, /api and /ws — see 15 §6)
   ▼
Reverse proxy / Vite dev proxy
   │
   ▼
Go API
   │
   ├── Session Service ──► Redis
   │
   └── PostgreSQL
```

The browser never stores a token in `localStorage`/`sessionStorage`. Authentication uses a session cookie:

```text
__Host-platform_session=<opaque random token>     (production)
platform_session=<opaque random token>            (local http development)
```

`Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`. In production the name carries the `__Host-` prefix, which makes browsers require `Secure`, `Path=/` and **no** `Domain` attribute. Name and `Secure` flag come from config (`COOKIE_NAME`, `COOKIE_SECURE`). The cookie carries only the token — never user data, never permissions.

Because the SPA and the API are **same-origin** (no CORS, no cross-site cookies), `SameSite=Lax` is sufficient, and CSRF protection (§8) is defense in depth plus protection for the WebSocket handshake rules in 09 §2.

---

# 3. Login Flow

```text
Browser
   │  POST /api/v1/auth/login  { email, password }
   ▼
Go API
   ├── Validate request (size, format)
   ├── Rate-limit check (per IP + per account)
   ├── Find user by normalized (lowercase) email
   ├── Verify password — ALWAYS runs an Argon2id verification, against a dummy
   │   hash when the account doesn't exist (equalizes timing, §4)
   ├── Check user.status (active? suspended? deactivated?) and the Redis lock (§12.3)
   ├── Check MFA requirement (Phase 2)
   ├── One transaction: write sessions row + security event + activity
   ├── Create the Redis session entries
   └── Set-Cookie (new token)
   ▼
Browser is authenticated
```

---

# 4. Password Verification

Passwords are never stored in plaintext:

```text
password → Argon2id → PHC-format string → credentials.secret_hash
```

A mismatch returns `401` without revealing whether the email exists:

```json
{
  "error": {
    "code": "invalid_credentials",
    "message": "Invalid email or password.",
    "request_id": "req_01..."
  }
}
```

## 4.1 Argon2id parameters and hardening

```go
type PasswordHasher interface {
    Hash(password string) (string, error)           // returns a PHC string
    Compare(hash string, password string) error
    NeedsRehash(hash string) bool
}
```

- **Starting parameters:** memory 64 MiB, time 3, parallelism 2, 16-byte salt, 32-byte key. Tune by benchmarking on the deployment hardware (target roughly 50–150 ms per hash); they live in one security package (`pkg/security`) and in config, never scattered across services.
- **PHC string format** (`$argon2id$v=19$m=65536,t=3,p=2$…`) stores the parameters with the hash, so old hashes stay verifiable after a parameter change. On a successful login, if `NeedsRehash` is true, the password is re-hashed with the current parameters and stored (**rehash-on-login**).
- **Maximum password length** (256 bytes): longer input is rejected before hashing — an unbounded password is a cheap CPU/memory DoS against Argon2.
- **Dummy verification:** for an unknown email the service verifies the submitted password against a fixed dummy hash with the same parameters, so the response time of "no such user" and "wrong password" is indistinguishable.
- **Concurrency limiter:** hashing is guarded by a semaphore (size from config, default about the number of CPU cores) so a login flood cannot exhaust memory (64 MiB × concurrent requests).
- Password policy: minimum length 12 (configurable); no composition rules; optional breached-password check is roadmap work (20 §4).

---

# 5. Login State Machine

```text
Unauthenticated
      │
      ▼
Credentials Submitted ──invalid──► (back to Unauthenticated, rate-limited)
      │ valid
      ▼
Credentials Valid
      │
      ├── MFA required ──► MFA Verification ──► Authenticated        (Phase 2)
      │
      └── MFA not required ──────────────────► Authenticated
```

---

# 6. Multi-Factor Authentication (TOTP) — Phase 2

```text
Password valid → MFA required? → yes → prompt TOTP → verify code → create authenticated session
```

The TOTP secret is **encrypted at rest** (key from `MFA_ENCRYPTION_KEY`), not one-way hashed like a password — verification needs to recover the original secret to compute the expected code, which a hash can never give back.

## 6.1 Recovery codes

```text
MFA enabled → generate recovery codes → show once → store hashed
```

Each code is single-use; the database stores only hashes. A used or expired recovery code fails closed (`invalid_mfa_code`), never falls back to a weaker check.

---

# 7. Session Architecture

Two stores, two jobs:

| | Stores | Purpose |
|---|---|---|
| **Redis** | `session:{sha256(token)}` → `{session_id, user_id, organization_id, auth_level, created_at, expires_at}`; `user_sessions:{user_id}` → set of token hashes | Fast lookup on every request; O(1) "revoke all of this user's sessions" |
| **PostgreSQL** (`sessions`, 04 §10) | `id, token_hash, user_id, organization_id, created_at, last_activity_at, expires_at, ip_address, user_agent, auth_level, revoked_at` | Durable record: history, the "Active Sessions" UI, revoke-all-others — **and the fallback lookup when Redis is unavailable** |

## 7.1 Session token

```text
token      = 32 bytes from crypto/rand, base64url-encoded      (carried only in the cookie)
token_hash = SHA-256(token)                                    (the ONLY thing stored anywhere)
```

- **Never** derive the token from a user id, email, timestamp, counter — and **never use UUIDv7** (or any UUID) as the token: UUIDv7 embeds a timestamp and has only about 74 random bits. The UUIDv7 in `sessions.id` is an internal identifier that is never sent to the client as a credential.
- Redis and PostgreSQL hold only the hash, so a leaked Redis dump or database dump does not contain usable session tokens.

## 7.2 Redis unavailable

PostgreSQL is authoritative. If Redis is unreachable, lookups fall back to `sessions` by `token_hash` (checking `revoked_at`, `expires_at`, idle timeout), a warning is written to the system log, and `last_activity_at` updates are batched. This is *degraded*, not *failed*: Redis loss must not log everyone out (domain invariant 14). Rate limiting and idempotency that depend on Redis fail according to 10 §15.

## 7.3 Revocation reaches WebSockets

Whenever a session is revoked, or a user/organization is suspended, the service publishes `session.revoked` (Redis Pub/Sub and outbox). Every API instance's WebSocket Hub closes matching live connections immediately (09 §2.2).

---

# 8. Cookie & CSRF

```text
Set-Cookie: __Host-platform_session=<token>; HttpOnly; Secure; SameSite=Lax; Path=/; Max-Age=...
```

`HttpOnly` keeps JavaScript from ever reading the session cookie, which limits what a successful XSS can do with it.

State-changing requests (`POST`/`PUT`/`PATCH`/`DELETE`) also need an anti-CSRF token independent of the cookie:

```text
GET /api/v1/auth/csrf  →  { "data": { "csrf_token": "..." } }
```

The token is `HMAC-SHA256(key = SESSION_SECRET-derived, message = session id)`, so it is bound to the session and needs no extra storage. The client sends it back as `X-CSRF-Token`; it is validated server-side before the request reaches a handler. For login (no session yet), the pre-auth endpoints are protected by `SameSite=Lax` + `Origin` header verification.

**WebSocket handshake:** the upgrade is a `GET`, which CSRF tokens do not cover. It is protected by verifying the `Origin` header against `ALLOWED_ORIGINS` (09 §2).

---

# 9. Authentication Middleware

```text
HTTP Request
     │
     ▼
Request ID
     │
     ▼
Security Headers
     │
     ▼
Rate Limit
     │
     ▼
CSRF
     │
     ▼
Authentication  ──►  Identity
     │
     ▼
Authorization  (06-authorization-engine.md)
     │
     ▼
Handler
```

```go
type Identity struct {
    UserID         uuid.UUID
    OrganizationID *uuid.UUID // nil only for a pure platform-level identity
    SessionID      uuid.UUID
    AuthLevel      AuthLevel
}
```

Handlers receive `Identity` from the middleware; they never read the cookie themselves.

---

# 10. The `Authenticator` Interface

```go
type Authenticator interface {
    Authenticate(ctx context.Context, req AuthRequest) (*Identity, error)
}
```

This keeps authentication from being permanently tied to "cookie session" as a concept. The platform will support several methods, all converging on the same `Identity`:

```text
Session Authentication (cookie, Svelte SPA)
OAuth2 / OIDC          (mobile, enterprise SSO — future)
Access Token            (mobile client)
API Token               (integrations)
Service Account         (server-to-server)
```

The application layer never needs to know *how* a request was authenticated — only that it now holds a valid `Identity`.

## 10.1 Why not JWT everywhere?

A JWT is attractive for distributed authentication, but this platform's permissions are designed to change **immediately** — a revoked role, a suspended user, a removed group membership. If permissions were embedded in a signed token:

```text
JWT issued with permissions=[...] → Admin revokes a permission → JWT is still valid until it expires
```

That's stale authorization by design, which 06-authorization-engine.md §8 refuses to accept even from its Redis cache (event-driven invalidation, not TTL-driven). So: **authentication token ≠ authorization truth.** A JWT, if used for mobile access tokens (§15), carries only identity — authorization is still evaluated server-side, on every request, the same way it is for a cookie session.

---

# 11. Logout

```text
POST /api/v1/auth/logout
     │
     ▼
Authenticate session
     │
     ▼
One transaction: mark PostgreSQL session revoked + security event (auth.logout) + activity
     │
     ▼
Delete Redis session entries; publish session.revoked (closes WebSockets)
     │
     ▼
Clear cookie
```

## 11.1 Logout all other sessions

```text
POST /api/v1/security/sessions/revoke-others
```

Finds every other active session for the user (Redis set, or PostgreSQL when Redis is down), revokes each in both stores, writes one security event. Triggered explicitly by the user, and automatically after a password change or reset (§13) or MFA enrollment change.

---

# 12. Session Lifecycle Rules

## 12.1 Expiration

Two independent clocks, both enforced:

- **Absolute**: `expires_at` — a session cannot live past this regardless of activity.
- **Idle**: revoked if `last_activity_at` is older than the deployment's idle timeout, even if `expires_at` hasn't arrived yet.

A security-sensitive deployment tightens the idle timeout via `settings` (platform scope); this is not a hardcoded constant. `last_activity_at` is refreshed at most once per minute per session to avoid a write per request.

## 12.2 Rotation

The session token is replaced — not just refreshed in place — at every authentication boundary:

```text
anonymous → login → new token
```

and again after:

```text
password change
MFA enrollment change
privilege escalation
switch-organization
```

This is what prevents session fixation: an attacker who fixed a pre-login token gets nothing once the real login issues a fresh one.

## 12.3 Account locking

```text
5 failed login attempts → temporary lock → security event
```

The temporary lock lives in **Redis with a TTL** (`auth:lock:{user_id}`), together with per-account and per-IP failure counters and a progressive delay. It is deliberately **not** `users.status`: `status = 'locked'` has no expiry and is reserved for a manual administrative lock. Never a hard lock alone — a hard lock keyed only on the account is itself a way to lock someone else out by repeatedly failing their login; per-IP throttling and the progressive delay limit that.

---

# 13. Password Change, Invitation & Reset

## 13.1 Change (authenticated)

```text
POST /api/v1/security/password
Verify current password → validate new password → hash (Argon2id) →
one transaction: update credential + security event + activity + outbox (notification) →
revoke other sessions → rotate this session
```

A security-sensitive deployment can additionally require MFA re-verification (Phase 2).

## 13.2 Reset (forgot password)

```text
POST /api/v1/auth/forgot-password
```

Always returns the same generic response — *"If the account exists, a reset link has been sent."* — regardless of whether the email is registered, and takes similar time either way (the email is sent by the worker via the outbox, never inline).

```text
Request reset → generate 32-byte token → store SHA-256 hash (password_reset_tokens) →
worker sends email → user opens link → POST /auth/reset-password {token, new_password} →
validate token (single-use, unexpired) → set new password → invalidate the user's other
reset tokens → revoke ALL sessions → security event (auth.password.reset)
```

The reset token is random, single-use, short-lived (default 30 minutes), and — like passwords and recovery codes — stored only as a hash.

## 13.3 Invitation

```text
POST /api/v1/organizations/{id}/invitations         (administrator; 08 §10.6)
   → invitations row + outbox event → worker emails the link (token hash stored)
POST /api/v1/auth/accept-invitation  {token, display_name, password}     (unauthenticated)
   → one transaction: create (or activate) the user, password credential,
     organization membership, optional group membership + role assignment,
     mark the invitation accepted, security event + activity
```

Invitation tokens follow the same rules as reset tokens (random, hashed, single-use) but live longer (default 72 hours). The preset role can never exceed the inviter's authority (06 §5.3); this is re-checked at acceptance time against the inviter's *current* authority. Responses for an invalid, used, or expired token are identical.

---

# 14. Login History & Security Events

```text
Security → Login History
```

is populated from `security_events` (04 §10), scoped so a user only ever sees their own history. Admin visibility into other users' security events follows the authorization engine's scoping rules, not a separate hardcoded admin check (13 §3–4 covers the full visibility model, shared with the Activity Log).

```text
auth.login.success        auth.mfa.enabled           auth.recovery_code.used
auth.login.failed         auth.mfa.disabled          auth.account.locked
auth.logout                auth.mfa.failed            auth.account.unlocked
auth.session.revoked       auth.password.changed      auth.passkey.registered
auth.invitation.accepted   auth.password.reset        auth.passkey.removed
auth.password.reset_requested
```

A security event (the auth-specific fact: *"a login failed"*) is distinct from an activity (the business-facing fact: *"Budi changed his password"*) — both can be written for the same operation; they serve different audiences and different tables.

---

# 15. Mobile Authentication

The Svelte browser client uses session + cookie (§2). A mobile client cannot hold a browser cookie the same way, so it uses:

```text
OAuth2 / OIDC + short-lived access token + refresh token rotation
```

```text
Mobile App → Authorization Server → Access Token → Go API
```

The access token identifies the user; it does **not** embed the permission set (§10.1). Every API call from the mobile client still goes through the same authorization engine as a browser request. (Mobile authentication is post-v1.0; 20 §4.)

---

# 16. Future Readiness

## 16.1 OAuth2 / OIDC (enterprise SSO)

```go
type IdentityProvider interface {
    Authenticate(ctx context.Context, request Request) (*Identity, error)
}
```

with `SessionProvider`, `OIDCProvider`, `OAuthProvider`, `APIKeyProvider`, and `ServiceAccountProvider` as interchangeable implementations — adding Google/Microsoft/enterprise SSO later means writing one new implementation of this interface, not re-architecting authentication.

## 16.2 WebAuthn / Passkeys

Not in v1.0 (§21), but the UI reserves the space:

```text
Security → Passkeys   (shown as "Coming soon", or hidden per feature availability)
```

Future flow: `User → Passkey → WebAuthn → Identity verified → Session created` — converging on the same `Identity` as every other method.

---

# 17. REST API Surface

```text
POST   /api/v1/auth/login
POST   /api/v1/auth/logout
POST   /api/v1/auth/refresh                 (extends the session within its absolute limit; rotates the token)
POST   /api/v1/auth/forgot-password
POST   /api/v1/auth/reset-password
POST   /api/v1/auth/accept-invitation
POST   /api/v1/auth/switch-organization
GET    /api/v1/auth/me
GET    /api/v1/auth/csrf

POST   /api/v1/security/password
GET    /api/v1/security/sessions
DELETE /api/v1/security/sessions/{id}
POST   /api/v1/security/sessions/revoke-others

GET    /api/v1/security/events
POST   /api/v1/security/mfa                 (Phase 2)
DELETE /api/v1/security/mfa                 (Phase 2)
POST   /api/v1/security/mfa/verify          (Phase 2)
```

Self-service `security/*` and `auth/me` endpoints require authentication only — they act on the caller's own records, which is an "own" scope by construction (06 §5.6).

---

# 18. Error Model

```json
{
  "error": {
    "code": "invalid_credentials",
    "message": "Invalid email or password.",
    "request_id": "req_01..."
  }
}
```

```text
authentication_required
invalid_credentials
account_locked
account_suspended
mfa_required
invalid_mfa_code
session_expired
session_revoked
csrf_failed
rate_limit_exceeded
invalid_reset_token
invalid_invitation
```

`account_locked` and `account_suspended` are only returned **after** the password has verified successfully — before that, every failure is `invalid_credentials`, so the status of an account is not an enumeration oracle.

---

# 19. Package Structure

```text
internal/auth/
├── domain/
│   ├── identity.go
│   ├── session.go
│   ├── credential.go
│   └── errors.go
├── application/
│   ├── login.go
│   ├── logout.go
│   ├── password.go
│   ├── session.go
│   ├── mfa.go                  # Phase 2
│   ├── reset_password.go
│   └── accept_invitation.go    # uses internal/invitation
├── repository/
│   ├── credentials.go
│   ├── sessions.go
│   ├── reset_tokens.go
│   └── security_events.go
├── service/
│   ├── password.go
│   ├── session.go              # Redis + PostgreSQL fallback
│   ├── mfa.go                  # Phase 2
│   ├── lock.go
│   └── rate_limit.go
└── transport/http/
    ├── handler.go
    └── middleware.go
```

---

# 20. Testing Requirements

```text
Login
  valid password → success
  wrong password → failure
  unknown email → generic failure (same response, and statistically indistinguishable
    response time, as a wrong password)
  locked (Redis) account → failure; lock expires by TTL
  suspended account → failure only after a correct password
  rate limit exceeded → failure
  password longer than the maximum → rejected before hashing

Password hashing
  hash output is a PHC string; parameters are stored with it
  a hash made with old parameters verifies, and is re-hashed on successful login
  hashing concurrency is bounded

Session
  valid session → authenticated
  expired session → rejected
  revoked session → rejected
  invalid session token → rejected
  idle timeout → rejected even before absolute expiry
  session survives a restart of the API process
  Redis unavailable → authenticated via PostgreSQL fallback (degraded), not logged out
  only the token hash exists in Redis and PostgreSQL (the raw token appears nowhere)
  token rotates on login, password change, switch-organization

MFA (Phase 2)
  valid code → success
  wrong code → failure
  expired or replayed recovery code → failure

CSRF
  valid token → success
  missing token → rejected
  invalid token → rejected
  token from another session → rejected

Password reset / invitation
  unknown email → same generic response as a known one
  token is single-use, expires, and is stored only as a hash
  reset revokes all sessions; change revokes all other sessions
  invitation accept is atomic (nothing is created if any step fails)
  invitation preset role above the inviter's current authority → rejected

Security
  logout → session revoked in both Redis and PostgreSQL and WebSockets closed
  revoked session cannot authenticate even if its Redis entry is restored
```

---

# 21. Phasing

```text
Phase 1 (v1.0)
         Email/password · Argon2id (with rehash, dummy verify, concurrency limit) ·
         Redis session (hashed token) with PostgreSQL fallback · Secure HttpOnly cookie ·
         CSRF · login/logout · session expiration · session management ·
         security events · login history · rate limiting · temporary lock ·
         password change · forgot/reset password · invitation acceptance

Phase 2  TOTP MFA · recovery codes · re-authentication for sensitive actions ·
         breached-password check

Phase 3  WebAuthn/Passkeys · OAuth2 · OIDC · enterprise SSO · mobile access tokens
```

(Rev 1.1 moved forgot/reset password and invitations from Phase 2 into v1.0: the success criterion "invite a Group Admin" in 01 §5 needs the same single-use-token-plus-email mechanism.)

---

# 22. Critical Security Rules

```text
1.  Never store plaintext passwords.
2.  Never store authentication tokens in localStorage/sessionStorage for the Svelte SPA.
3.  Never trust frontend authorization — it's UX, not a control (06 §15).
4.  Never expose sensitive fields merely because the caller can reach the endpoint.
5.  Never log passwords, access tokens, refresh tokens, session tokens, reset/invitation
    tokens, or MFA secrets (13 §2.3).
6.  Never return internal stack traces to clients.
7.  Never allow a revoked session to remain usable.
8.  Never assume authentication means authorization.
9.  Never allow cross-organization access without an explicit authorization decision.
10. Never make Redis the permanent source of truth for business data.
11. Never store a session/reset/invitation token — only its SHA-256 hash.
12. Never use a UUID (including UUIDv7) as a bearer token.
```

---

# 23. Handoff to Authorization

Authentication's output is exactly one thing:

```text
Identity { UserID, OrganizationID, SessionID, AuthLevel }
```

Everything after this point — organization membership, group membership, role assignment, permission, scope, policy, field permission, and the final ALLOW/DENY — is 06-authorization-engine.md.
