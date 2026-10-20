# Observability
## Open Source Modular Application Platform v1.0

**Document:** 14-observability.md
**Status:** Baseline (Rev 1.1)
**Previous:** 13-audit-logging.md
**Next:** 15-docker-native-development.md

---

# 1. Purpose

This document says what to track, what to track it *with*, what shape it takes, and — critically — how it relates to `system_logs` (13), since "structured logging" and "the system_logs table" are easy to conflate and are not the same thing.

---

# 2. The Three Pillars, and What Each One Is For

```text
Logs     — what happened, in detail, at a point in time. High volume, cheap per-entry,
           read mostly when you already know roughly when/where to look.
Metrics   — how much / how often, aggregated over time. Cheap to store long-term,
           the thing an alert fires on.
Traces     — the causal path of one request across every layer and service it touched.
           Read when you know *which* request and need to know *where* it went slow
           or wrong.
```

All three are correlated by the same two IDs, generated once and carried everywhere (10 §10):

```text
request_id   — one HTTP (or WebSocket) request/response cycle, or one worker job run
trace_id     — the whole causal chain, which may span an HTTP request, the outbox event it
                publishes, and the worker job that event triggers
```

`trace_id` crosses the API → worker boundary **inside the outbox row** (`outbox_events.trace_id`, `request_id`) — the relay restores both into the handler's context (10 §10), which is what keeps a slow approval email traceable to the request that caused it.

---

# 3. Logs vs. `system_logs`: Not the Same Thing

This needs to be explicit because 13 introduces `system_logs` and it's tempting to assume every log line ends up there. It doesn't.

```text
stdout/stderr, structured JSON       →  the firehose. Every log line, every level,
(shipped to the deployment's log       from every request. Read via the deployment's
aggregator — Loki, CloudWatch,         own log tooling. Not queryable from inside the
OpenSearch, whatever the operator      platform's own UI.
already runs)

system_logs (PostgreSQL, 04 §12)       the curated subset worth surfacing inside the
                                          product itself: warnings and errors significant
                                          enough that a Super Admin should be able to see
                                          them without shell access to the deployment,
                                          queryable from the Super Admin UI (13 §5).
```

A `debug`-level line logging a cache hit goes to stdout only. An `error`-level line for "worker job failed after all retries" goes to **both** — stdout for the full detail (stack trace included, since this destination is never client-facing), and a `system_logs` row with the summary (`event_code`, `message`, `request_id`, `trace_id`) for the Super Admin UI. The application code decides this per call site: `logger.Error(ctx, msg, …)` for the firehose, `logger.Error(ctx, msg, …, logger.SystemLog("event.code"))` (an explicit option on the same call) for the subset that also gets a durable, queryable row. The `system_logs` write goes through the runtime pool, is best-effort and rate-limited per `event_code` (a failure storm must not become a write storm), and never blocks or fails the request.

Logging uses the standard library `log/slog` with a JSON handler, wrapped by `pkg/logger`.

**Implementation note (ADR-0015):** `pkg/logger` wraps `log/slog`; there is no `logger.Error(ctx, …)` function. The marker is an ordinary slog attribute on the standard call: `log.ErrorContext(ctx, msg, "error", err, logger.SystemLog("event.code"))`. The marker is always stripped before the JSON handler, so it never appears on stdout. The logger is created first in the composition root, before any pool exists, so it starts with an empty `logger.Gate`; `app.AttachSystemLog` attaches the `systemlog.Writer` sink once the pool is ready and detaches it **before** the pool closes (until then a marked line goes to stdout only). Only the call's own attributes become the entry's fields — attributes added through `With()` / `WithGroup()` do not — and the logger's minimum level still applies, so `system_logs` is for `warn` and `error`. A panicking sink is recovered and reported as a single stdout warning.

---

# 4. Structured Log Format

One JSON shape, everywhere, produced by `pkg/logger` (10 §7):

```json
{
  "timestamp": "2026-09-30T08:21:00.123Z",
  "level": "error",
  "service": "api",
  "environment": "production",
  "request_id": "req_01...",
  "trace_id": "trace_01...",
  "message": "failed to send approval notification",
  "error": "smtp timeout after 3 retries",
  "module": "finance",
  "fields": { "invoice_id": "...", "recipient_user_id": "..." }
}
```

`fields` is the only open-ended part — everything else is a fixed key so log-aggregator queries and dashboards don't have to guess at field names per service. The logger applies the same redaction list as audit metadata (13 §2.3) to `fields`.

---

# 5. Metrics

A `/metrics` endpoint (Prometheus exposition format is the default assumption — anything OpenTelemetry-compatible can export to it without a rewrite). It is **not** exposed publicly: it is served on a separate internal port (or restricted by the reverse proxy to the monitoring network).

```text
http_request_duration_seconds{route, method, status}       histogram   (route = chi route PATTERN, never the raw path)
http_requests_total{route, method, status}                  counter
auth_login_failures_total{reason}                             counter
authz_denials_total{action, scope_type}                        counter
db_query_duration_seconds{operation}                             histogram
db_pool_acquired_connections{pool}                                gauge
redis_command_duration_seconds{command}                          histogram
websocket_connections_active                                       gauge
websocket_messages_total{event}                                     counter
websocket_slow_consumer_disconnects_total                            counter
outbox_events_pending                                                   gauge
outbox_oldest_pending_age_seconds                                        gauge
outbox_events_dead_total                                                  counter
worker_job_duration_seconds{job_type}                                      histogram
worker_job_failures_total{job_type}                                         counter
module_health_status{module_code}                                            gauge (1=healthy, 0=unhealthy)
```

Labels are low-cardinality by rule: never a user id, organization id, or raw URL path (those belong in logs and traces).

`authz_denials_total` is worth calling out specifically: a normal application has a low, steady background rate of denials (users occasionally clicking something they don't have access to). A sudden spike, especially concentrated on one `action`, is a legitimate signal worth alerting on (§8) — this metric exists for security observability, not just performance. The `outbox_*` gauges are the worker's health signal: user-facing requests keep working when the worker is down, so *this* is how its absence is noticed.

**Implementation note (ADR-0017):** each process serves its own `/metrics` on its own port: the API on `METRICS_PORT`, the worker on `WORKER_METRICS_PORT` (0 = off, 10 §8), never on the public port. The **API** exposes the HTTP metrics, `db_pool_*`, and the two `outbox_*` gauges, read from the database at scrape time — which is why a dead worker is still visible. The **worker** exposes `worker_job_*`, `outbox_events_dead_total` (incremented by the relay's dead-letter hook), and its own pool statistics. Beyond `db_pool_acquired_connections`, the pool collector also exports `db_pool_idle_connections` and `db_pool_max_connections`. Cardinality guards: `route` is the chi pattern, and a request with no matching route is labelled `unmatched`; an HTTP method outside the standard set is labelled `OTHER`, because clients can send any token. If the outbox read fails (for example the database is down) the scrape still returns `200`: the two gauges disappear and the cause is logged, so alert on `absent()`. Not yet implemented (they arrive with the milestone that owns them): `auth_*`, `authz_*`, `db_query_duration_seconds`, `redis_command_duration_seconds`, `websocket_*`, `module_health_status`; `worker_job_*` has no call site until the worker runs jobs other than the outbox relay (M08).

---

# 6. Tracing

A span per layer boundary from the request-layering model (10 §6):

```text
[span] HTTP handler: POST /api/v1/finance/invoices/{id}/approve
   └─ [span] application.ApproveInvoice
        ├─ [span] authorization.Can
        ├─ [span] tx (db transaction)
        │    ├─ [span] repository.FindByID  (db query span, auto-instrumented)
        │    ├─ [span] repository.Save      (db query span)
        │    ├─ [span] audit.Record         (db insert span)
        │    └─ [span] events.Publish       (db insert into outbox_events; the row carries trace_id forward)
        │
[span, new process, linked by trace_id] worker: handle finance.invoice.approved
   └─ [span] notification.Create
        └─ [span] email delivery
```

The trace continues into `cmd/worker` because the outbox row carries `trace_id` — a slow approval email is traceable back to the exact HTTP request that triggered it, not just visible as an isolated slow job with no context. Export is OTLP (`OTEL_EXPORTER_OTLP_ENDPOINT`); with no endpoint configured, tracing is a no-op.

**Implementation note (ADR-0017):** M02 delivers the **hook only**; no span is created anywhere yet, and the layer spans above arrive together with the layers that create them (the same goes for linking `trace_id` into logs and the outbox). `telemetry.SetupTracing` does nothing — touches no global state — when the endpoint is empty. When set, it exports OTLP/HTTP (protobuf) to the base URL plus `/v1/traces`, with resource attributes `service.name` (`zago-api` / `zago-worker`) and `deployment.environment.name`, the W3C `traceparent` and `baggage` propagators, and batch export that is flushed on shutdown (5 s, run last). An unreachable collector never stops the process: OpenTelemetry's own errors are routed to the logger at `WARN`. Sampling uses the SDK default (`ParentBased(AlwaysSample)`) until real spans exist to sample.

---

# 7. Health Endpoints

Defined in 10 §11; this document adds only what `/health/ready` aggregates:

```text
/health/ready = PostgreSQL reachable
              AND Redis reachable
              AND (policy-dependent) every enabled module's own health check (07 §5)
                  reports healthy
```

A single unhealthy module degrades its `module_health_status` metric and is listed in the readiness response body; depending on deployment policy it either leaves overall readiness unaffected (default — the rest of the platform stays "ready" while one module recovers) or fails it (`strict`). This is a deployment-time choice this document deliberately leaves open rather than hardcoding one answer for every operator.

---

# 8. What's Worth Alerting On

Not a specific alerting tool's config — just which of the above are actually actionable signals rather than noise:

```text
http_requests_total{status=~"5.."} rate         — backend is failing requests
db_query_duration_seconds p99                     — database is degraded
db_pool_acquired_connections near max              — pool exhaustion imminent
redis_command_duration_seconds p99                 — cache/session layer is degraded
outbox_oldest_pending_age_seconds > threshold        — the worker is down or stuck: notifications/emails delayed
outbox_events_dead_total increasing                    — poison events needing attention
worker_job_failures_total rate                          — async work is silently not happening
module_health_status == 0 for any enabled module         — a module needs attention
authz_denials_total spike, especially concentrated         — possible probing/attack, or a
  on one action                                             broken permission rollout
auth_login_failures_total spike from one IP or account       — credential-stuffing attempt
websocket_slow_consumer_disconnects_total rising              — realtime path under pressure
```

---

# 9. Dashboards

A starter dashboard (Grafana or equivalent; the JSON is committed under `deploy/observability/`) should answer, at a glance:

```text
1. Is the API up and fast?           (request rate, latency, error rate)
2. Are the data stores healthy?       (PostgreSQL + Redis latency, pool usage)
3. Is realtime working?                (active WebSocket connections, message rate, slow-consumer disconnects)
4. Is async work keeping up?             (outbox depth + oldest age, worker job rate, failure rate)
5. Are all modules healthy?               (module_health_status grid, one cell per module)
6. Is anything suspicious happening?       (authz denial rate, login failure rate)
```

---

# 10. Package Structure

```text
backend/pkg/
├── logger/       slog JSON logging (§4) — see 10 §7
└── telemetry/    OpenTelemetry setup: tracer/meter providers, span helpers, the
                  /metrics HTTP handler
```

A module wanting a custom metric or span uses the helpers exposed through `module-sdk` (backed by `pkg/telemetry`) the same way core does — observability is infrastructure every module gets for free, not something each module wires up independently.

---

# 11. Testing Requirements

```text
Health
  /health/ready returns 503 when PostgreSQL is unreachable
  /health/ready returns 503 when Redis is unreachable
  /health/ready reflects a module reporting itself unhealthy (per policy)

Correlation
  request_id set on an incoming request is present in the resulting log lines,
    the resulting activity row, and the resulting trace
  an event published from an HTTP request carries that request's trace_id through the
    outbox into the worker job it triggers

Metrics
  a failed login increments auth_login_failures_total
  a denied authorization check increments authz_denials_total with the correct
    action and scope_type labels
  route labels use the route pattern, not the raw path (no cardinality explosion)
  outbox gauges reflect pending rows and the oldest pending age

Logging
  a log line never contains a value from the redaction list
  a SystemLog-flagged error produces a system_logs row; a failure to write it does not fail the request
```
