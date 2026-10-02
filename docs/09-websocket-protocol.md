# Realtime Communication: WebSocket, Notifications, Inbox & Chat
## Open Source Modular Application Platform v1.0

**Document:** 09-websocket-protocol.md
**Status:** Baseline (Rev 1.1)
**Previous:** 08-api-specification.md
**Next:** 10-backend-architecture.md
**Builds on:** 02-domain-model.md (§21–27), 03-database-erd.md (§22–31)
**Note:** The original plan lists this as "WebSocket Protocol." Its scope is widened to also cover the Notification, Inbox and Chat domain rules that ride on top of it.

---

# 1. Purpose

Three related but distinct things live here, and keeping them distinct matters for both the data model and the UX:

| | What it is | Example |
|---|---|---|
| **Notification** | Fire-and-forget, informational | "Your password was changed" |
| **Inbox Item** | Actionable, expects a response | "Invoice INV-001 needs your approval" |
| **Chat Message** | Person-to-person or group conversation | "Can you review this by EOD?" |

An Inbox Item may *generate* a Notification (02 §22), but they are never the same row, the same table, or the same UI concept. Collapsing them is the single most common mistake in systems like this — a "notifications" list that mixes real approvals with informational pings trains users to ignore all of it.

WebSocket is the transport that makes all three feel instant. It is never the source of truth — every notification, inbox item, and message is persisted in PostgreSQL first (03 §24–28); the WebSocket push is a convenience on top of durable state, not a replacement for it. A client that was offline when an event fired must see the same data on reconnect via a normal REST fetch.

---

# 2. Connection Lifecycle

```text
Browser
   │  wss://<same origin>/ws   (same session cookie)
   ▼
Reverse proxy / Vite proxy
   ▼
Go API — WebSocket Handler
   │
   ├── Verify the Origin header against ALLOWED_ORIGINS   (else 403)
   ├── Authenticate (same session as REST; no separate WS-only auth)
   ├── Register connection in the Hub, keyed by user_id (and session id)
   └── Subscribe to Redis Pub/Sub channel ws:user:{user_id} (+ active conversations)
```

A WebSocket connection is authenticated exactly like an HTTP request — the same session cookie, validated the same way. There is no separate, weaker WebSocket-only credential. The upgrade is a `GET`, so it is **not covered by the CSRF token** (05 §8) — the `Origin` check is what prevents Cross-Site WebSocket Hijacking. No token is ever placed in the URL or in `Sec-WebSocket-Protocol`.

Library: `github.com/coder/websocket`.

## 2.1 Connection hygiene

| Concern | Rule |
|---|---|
| Keep-alive | Server sends ping every ~30 s; a connection that misses pongs is closed |
| Read limit | Maximum inbound frame size (default 64 KiB); larger → close 1009 |
| Backpressure | Each connection has a bounded outbound buffer; a **slow consumer** whose buffer fills is disconnected (it reconnects and resyncs, §2.3) instead of stalling the Hub |
| Limits | Maximum connections per user (default 5) and maximum subscriptions per connection |
| Rate limiting | Per-user limits on inbound messages and typing events (§9) |
| Re-validation | Session validity is re-checked periodically and on every `session.revoked` |

## 2.2 Revocation closes sockets

When a session is revoked, a user or organization is suspended, or a conversation membership ends, the Hub closes the affected connections (code 4401) or drops the affected subscription. The trigger is the `session.revoked` / membership events on Redis Pub/Sub (05 §7.3), so it works across instances.

## 2.3 Reconnection and resync

WebSocket delivery is **at-least-once, not exactly-once**: clients must ignore a duplicate event for an `id` they've already applied.

The event stream carries two kinds of events:

- **Durable creations** (`notification.created`, `inbox.created`, `message.created`, …) — each corresponds to a PostgreSQL row. On reconnect the client sends `last_seen` (the newest event/resource timestamp it processed); the server replays everything created after it that the client is authorized to see, then resumes the live stream.
- **Non-durable events** (`*.updated`, `*.deleted`, `typing.*`, `presence.*`) — these are **not** replayed. If the gap since `last_seen` is larger than a configured window, or anything non-durable may have been missed, the server sends `resync_required` and the client **refetches its state over REST** (notification list, inbox, open conversations since `updated_at`). REST is always the source of truth.

## 2.4 Frame format

Every frame, in both directions, is a JSON object:

```json
{ "v": 1, "type": "message.created", "id": "01J...", "ts": "2026-09-30T08:21:00Z", "data": { } }
```

- `v` — protocol version; `type` — an event name from §5 or a client command; `id` — a unique event id (UUIDv7) used for deduplication; `ts` — server time (UTC).
- Client → server commands: `subscribe` / `unsubscribe` (`data: {conversation_id}`), `typing.start` / `typing.stop`, `ack` (`data: {last_seen}`), `ping`.
- Server → client control frames: `resync_required`, `error` (`data: {code, message}` using the registry in 08 §11), `subscribed`.
- Unknown `type` or malformed frames produce an `error` frame; repeated violations close the connection.

---

# 3. Authorization Per Subscription

> Knowing a conversation ID is never sufficient to access it.

Every subscribe action — joining a conversation's event stream, requesting message history — re-runs the same `Authorizer.Can` check the REST endpoint for that resource would run (06), plus the membership check. A client cannot simply open a socket and ask for `conversation:abc123`'s events without the server verifying `conversation_members` first.

```go
func (h *Hub) Subscribe(ctx context.Context, conn *Conn, conversationID uuid.UUID) error {
    d, err := h.authz.Can(ctx, Check{
        Identity: conn.Identity, Scope: conn.Scope, Action: "conversation.read",
        Resource: &ResourceRef{Type: "conversation", ID: conversationID, OrganizationID: &conn.OrgID},
    })
    if err != nil || !d.Allowed {
        return ErrForbidden
    }
    ok, err := h.chat.IsActiveMember(ctx, conn.OrgID, conversationID, conn.Identity.UserID)
    if err != nil || !ok {
        return ErrForbidden
    }
    ...
}
```

---

# 4. Horizontal Scaling

A single Go process holding all WebSocket connections in memory doesn't scale past one instance. The fix is standard:

```text
Client A ── wss ──► API instance 1 ──┐
                                      │
Client B ── wss ──► API instance 2 ──┼──► Redis Pub/Sub ──► fan-out to whichever
                                      │                      instance holds the
Client C ── wss ──► API instance 3 ──┘                      target connection
```

- Each API instance holds only the WebSocket connections it physically accepted.
- When any instance needs to deliver an event to a user, it publishes to a Redis channel (`ws:user:{user_id}`); whichever instance holds that user's connection (if any) delivers it. Conversation events fan out to each active member's user channel after the membership check.
- If no instance holds the connection (user offline), the event still exists durably in `notifications`/`inbox_items`/`messages` — nothing is lost, it's simply picked up on the next login/reconnect. Redis Pub/Sub is fire-and-forget, which is acceptable precisely because of that durability.
- Presence (`presence.online` / `presence.offline`) is tracked as short-TTL Redis keys (`presence:{user_id}`), refreshed on activity/heartbeat — ephemeral by design, exactly the kind of state Redis is for and PostgreSQL is not.
- Who publishes: the **worker** relays outbox events (for example `notification.created` produced by a module event) to `ws:user:{id}`; the **API** publishes directly for latency-sensitive chat events after the message transaction commits.

---

# 5. Event Catalog

```text
notification.created
notification.read

inbox.created
inbox.updated
inbox.completed

message.created
message.updated
message.deleted
message.read

conversation.created
conversation.updated

presence.online
presence.offline

typing.started
typing.stopped

session.revoked          (internal, Redis only — never sent to clients as-is)
```

Every payload is subject to the same Field Permission filtering as a REST response (06 §7) before it's serialized onto the socket — a WebSocket push is not a side-channel that bypasses field-level restrictions.

---

# 6. Notifications

```text
notifications (id, organization_id?, recipient_user_id, type, title?, body?,
                data jsonb, priority, status, created_at, read_at?, expires_at?)
```

- `type` is a namespaced string (`security.password_changed`, `finance.invoice.approved`) so the frontend can pick an icon/route and render the text from its i18n catalog using `data` as parameters, without special-casing every possible notification. `title`/`body` are optional English fallbacks only (08 §8).
- Created either directly by core (security events, module lifecycle) or by a module's event triggering a registered **Notification Template** (07 §8) — core's notification service never contains module-specific logic.
- `status`: `unread → read → archived`, or `expired` if `expires_at` passes unread.
- Delivery: written to PostgreSQL first (durable, from an outbox handler or the originating transaction), then pushed over the user's live WebSocket connection if one exists, then — for high-priority types — handed to the worker for an email fallback if the user hasn't acknowledged it within a configurable window.

```text
GET   /api/v1/notifications
POST  /api/v1/notifications/{id}/read
POST  /api/v1/notifications/read-all
```

---

# 7. Inbox

```text
inbox_items (id, organization_id, recipient_user_id, type, title?, description?, data,
             resource_type, resource_id, action_url, status, priority,
             created_at, completed_at?, expires_at?)
```

- Represents work, not information: `pending → in_progress → completed / dismissed / expired`.
- `resource_type` + `resource_id` point back at whatever needs action (an invoice, a registration) — the Inbox item itself is a pointer, not a copy of the business data, so it can't go stale relative to the real record.
- Completing the underlying action (e.g. approving the invoice via the Finance module's own endpoint) is what marks the Inbox item `completed` — via the module publishing an event the Inbox service subscribes to (idempotent handler), not the client calling two separate "approve" and "mark inbox done" endpoints that can drift out of sync.

```text
GET   /api/v1/inbox
POST  /api/v1/inbox/{id}/dismiss
```

---

# 8. Chat: Conversations & Messages

## 8.1 Conversations

- `type: direct` — exactly two members, created once per pair: the application computes `direct_key` and inserts; the unique index (04 §11) makes a concurrent duplicate fail, and the loser simply returns the existing conversation.
- `type: group` — two or more members; membership managed like any other resource (`conversation_members`, with `member_role: member/moderator/owner`).
- A conversation belongs to exactly one organization (02 §23) — cross-organization chat is out of scope for v1. A member must have an organization membership in that organization (composite foreign key, 04 §11) and an **active** one (application check).

## 8.2 Messages

- Sender must be an **active** `conversation_members` row at write time — leaving a conversation stops future sends but never rewrites history.
- Deletes are tombstones (`deleted_at` set, `body` cleared server-side) — not row deletion, so counts/ordering in the client don't jump around and moderation history is preserved.
- `message_type: text | system | file | image | event` — `system`/`event` types let the conversation surface things like "Budi added Sinta to the group" without inventing a parallel activity feed just for chat.
- Size-limited (default text body 4,000 characters) and validated server-side; edits are allowed only by the sender within a configurable window and mark the message `updated_at`.
- **Attachments** are metadata rows (`attachments`) pointing at storage through the `storage` interface (local disk driver in v1; S3-compatible driver is roadmap work, 20 §4) — never raw bytes in PostgreSQL. Uploads are limited by size (default 10 MiB, configurable), the **content type is detected server-side** from the bytes (a declared type that contradicts the content is rejected with `attachment_rejected`), file names are never used as storage paths, and downloads go through `GET /attachments/{id}` which re-runs the authorization check — the storage location is never directly exposed.

## 8.3 Read State

Default: `conversation_members.last_read_message_id` — one column, updated on read, cheap. Only add a detailed `message_reads` table (03 §29) for a specific conversation type that genuinely needs per-recipient read receipts (e.g. a broadcast-style announcement channel) — for normal chat volume, per-message-per-user rows are a write-amplification cost with little payoff.

## 8.4 Reactions

`message_reactions (message_id, user_id, reaction)`, unique per `(message, user, reaction)` — simple, no need for a state machine.

## 8.5 REST Surface (mirrors the WebSocket events)

```text
GET    /api/v1/conversations
POST   /api/v1/conversations
GET    /api/v1/conversations/{id}/messages        (cursor-paginated)
POST   /api/v1/conversations/{id}/messages
POST   /api/v1/conversations/{id}/attachments
PATCH  /api/v1/messages/{id}
DELETE /api/v1/messages/{id}
POST   /api/v1/messages/{id}/reactions
GET    /api/v1/attachments/{id}
```

REST is the source of truth and the fallback for any client that isn't currently connected over WebSocket (initial load, pagination, catching up after being offline or after `resync_required`); WebSocket only carries the live delta on top of it. A message sent through REST and one sent through a WebSocket command go through the **same** application service.

---

# 9. Rate Limiting

Chat and notification endpoints sit in the same rate-limiting posture as everything sensitive in 05 §12.3 — keyed per-user in Redis:

```text
ws:message:user:<user_id>
ws:typing:user:<user_id>
```

A message-flood or typing-indicator-spam from one connection must not degrade the Hub for anyone else.

---

# 10. Error Model

```text
conversation_not_found
not_a_conversation_member
message_too_large
invalid_message_type
websocket_unauthorized
subscription_denied
attachment_rejected
```

---

# 11. Package Structure

```text
internal/
├── websocket/    # Hub, connection registry, frame protocol, Redis pub/sub fan-out
├── notification/ # service + template registry
├── inbox/
├── chat/         # conversations, messages, reactions, attachments
└── storage/      # storage interface + local disk driver
```

---

# 12. Testing Requirements

```text
Auth
  WebSocket connection without a valid session is rejected at handshake
  handshake with a wrong or missing Origin is rejected
  subscribing to a conversation the user isn't a member of is rejected
  revoking the session / suspending the user closes the live socket (across instances)

Protocol
  a frame larger than the read limit closes the connection
  a slow consumer is disconnected without blocking other connections
  unknown frame type → error frame

Delivery
  offline user still sees the notification/message via REST on next login
  duplicate delivery of the same event id is safely ignored by the client contract
  after a gap, reconnect yields replay of created rows and resync_required for non-durable events

Scaling
  event published on instance A reaches a client connected to instance B (Redis pub/sub)

Chat
  non-member cannot send a message
  a user without an organization membership cannot be added (database rejects)
  deleted message becomes a tombstone, not a missing row
  two concurrent requests to create the same direct conversation yield ONE conversation
  an attachment whose content contradicts its declared type is rejected
  an attachment download re-checks authorization

Field permissions
  a restricted field is absent from a WebSocket payload exactly as it would be from REST
```
