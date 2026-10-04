# Docker & Native Development
## Open Source Modular Application Platform v1.0

**Document:** 15-docker-native-development.md
**Status:** Baseline (Rev 1.1)
**Previous:** 14-observability.md
**Next:** 16-makefile-specification.md

---

# 1. Purpose

Two supported ways to run this platform locally, and neither is allowed to drift from what actually ships:

```text
Docker      — one command, no local Go/Node/Postgres/Redis install required.
              The default for anyone trying the platform for the first time
              or building a module against a known-good environment.

Native      — Go and Node run directly on the host, only the datastores (and a mail
              catcher) stay in containers. Faster edit-compile-test loops for anyone
              actively developing core or a module.
```

Both use the exact same migrations, the exact same bootstrap flow, and the exact same environment variable names — the only thing that changes is what's containerized.

Target versions: **PostgreSQL 18**, **Redis 8**, Go latest stable, Node LTS (24). Exact patch tags are pinned in `docs/adr/0001-stack.md`.

---

# 2. Docker Compose Services

```text
docker compose
├── postgres    — PostgreSQL 18, persistent volume, healthcheck: pg_isready,
│                 role-creation init script (deploy/db/init/00-roles.sql, 04 §13)
├── redis        — Redis 8, healthcheck: redis-cli ping
├── mailpit       — SMTP catcher + web UI (:8025) so invitation / reset emails can be seen locally
├── migrate        — one-shot: runs cmd/migrate up with the app_migrator role, then exits   [profile: full]
├── api             — cmd/api with hot reload (air), depends on migrate completing         [profile: full]
├── worker           — cmd/worker with hot reload (air), same image as api                 [profile: full]
└── web               — Vite dev server (hot module reload), proxies /api and /ws to api    [profile: full]
```

`postgres`, `redis` and `mailpit` have no profile (they always start); the application services are in the `full` profile. **`make infra-up`** starts only the first three (native development); **`make docker-up`** starts everything (`--profile full`). This replaces the earlier ambiguity where "docker-up" meant two different things.

## 2.1 `docker-compose.yml`

```yaml
services:
  postgres:
    image: postgres:18
    environment:
      POSTGRES_DB: platform
      POSTGRES_USER: postgres
      POSTGRES_PASSWORD: postgres        # local development only
    volumes:
      # PostgreSQL 18 images keep PGDATA under /var/lib/postgresql/<major>/…;
      # mount the PARENT directory, not /var/lib/postgresql/data.
      - postgres_data:/var/lib/postgresql
      - ./deploy/db/init:/docker-entrypoint-initdb.d:ro   # 00-roles.sql, 01-dev-privileges.sql (dev/CI only: app_migrator CREATEDB for migrate-roundtrip; ADR-0003)
    ports:
      - "127.0.0.1:5432:5432"
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U postgres -d platform"]
      interval: 5s
      timeout: 3s
      retries: 10

  redis:
    image: redis:8
    ports:
      - "127.0.0.1:6379:6379"
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 10

  mailpit:
    image: axllent/mailpit
    ports:
      - "127.0.0.1:1025:1025"   # SMTP
      - "127.0.0.1:8025:8025"   # web UI

  migrate:
    profiles: ["full"]
    build: { context: ., dockerfile: deploy/docker/backend.Dockerfile, target: dev }
    command: ["go", "run", "./backend/cmd/migrate", "up"]
    volumes:                      # stage dev tidak berisi source; tanpa mount `go run` gagal (ADR-0003)
      - .:/app
      - go_mod_cache:/go/pkg/mod
      - go_build_cache:/root/.cache/go-build
    env_file: .env
    environment:
      DATABASE_URL: postgres://app_user:app_dev_pw@postgres:5432/platform
      MIGRATION_DATABASE_URL: pgx5://app_migrator:migrator_dev_pw@postgres:5432/platform
    depends_on:
      postgres: { condition: service_healthy }

  api:
    profiles: ["full"]
    build: { context: ., dockerfile: deploy/docker/backend.Dockerfile, target: dev }
    command: ["air", "-c", ".air.api.toml"]
    volumes:
      - .:/app
      - /app/tmp                   # air build output stays in a volume, not the (root-owned) working tree
      - go_mod_cache:/go/pkg/mod
      - go_build_cache:/root/.cache/go-build
    ports:
      - "127.0.0.1:8080:8080"
    env_file: .env
    environment:
      DATABASE_URL: postgres://app_user:app_dev_pw@postgres:5432/platform
      REDIS_URL: redis://redis:6379
      SMTP_HOST: mailpit
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://127.0.0.1:8080/health/ready >/dev/null || exit 1"]
      interval: 5s
      timeout: 3s
      retries: 40
      start_period: 120s          # air mengompilasi + mengunduh modul saat start pertama (cache dingin)
    depends_on:
      migrate: { condition: service_completed_successfully }
      redis: { condition: service_healthy }

  worker:
    profiles: ["full"]
    build: { context: ., dockerfile: deploy/docker/backend.Dockerfile, target: dev }
    command: ["air", "-c", ".air.worker.toml"]
    volumes:
      - .:/app
      - /app/tmp                   # air build output stays in a volume, not the (root-owned) working tree
      - go_mod_cache:/go/pkg/mod
      - go_build_cache:/root/.cache/go-build
    env_file: .env
    environment:
      DATABASE_URL: postgres://app_user:app_dev_pw@postgres:5432/platform
      MAINTENANCE_DATABASE_URL: postgres://app_maintenance:maintenance_dev_pw@postgres:5432/platform
      REDIS_URL: redis://redis:6379
      SMTP_HOST: mailpit
    depends_on:
      migrate: { condition: service_completed_successfully }
      redis: { condition: service_healthy }

  web:
    profiles: ["full"]
    build: { context: ., dockerfile: deploy/docker/web.Dockerfile, target: dev }
    command: ["npm", "run", "dev", "-w", "apps/web", "--", "--host"]
    volumes:
      - .:/app
      - /app/node_modules
      - /app/apps/web/node_modules   # Vite cache (.vite) stays in a volume, not the working tree
    ports:
      - "127.0.0.1:5173:5173"
    environment:
      VITE_API_PROXY_TARGET: http://api:8080
    depends_on:
      - api

volumes:
  postgres_data:
  go_mod_cache:
  go_build_cache:
```

`condition: service_healthy` and `service_completed_successfully` make the start order deterministic: the API never starts before PostgreSQL accepts connections **and** the migrations have been applied, instead of crash-looping for ten seconds.

The development passwords above exist only for local use. The init script (`deploy/db/init/00-roles.sql`) creates `app_migrator`, `app_user` and `app_maintenance` with those development passwords; production roles are provisioned by the operator from the secret manager (04 §13). Datastore ports are bound to `127.0.0.1` so a development machine does not expose PostgreSQL or Redis to its network.

---

# 3. Dockerfiles

**The build context is the repository root** — the backend build needs the root `go.mod`, `backend/`, `packages/module-sdk/` and `modules/`, none of which are visible from a `./backend` context.

```dockerfile
# deploy/docker/backend.Dockerfile
ARG GO_VERSION=1.xx          # pinned in docs/adr/0001-stack.md and 0002
FROM golang:${GO_VERSION}-alpine AS base
WORKDIR /app
COPY go.mod go.sum ./
# NO `go mod download`: it would pull every dev-tool dependency (golangci-lint, sqlc, swag) into an
# image layer (2 GB, 18-minute builds). Module and build caches use BuildKit cache mounts instead (ADR-0003).

FROM base AS dev
# air is built once into the image (pinned by the go.mod `tool` directive); source is bind-mounted by
# docker-compose.yml; application modules are downloaded on first start into the go_mod_cache volume
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -o /usr/local/bin/air github.com/air-verse/air
CMD ["air", "-c", ".air.api.toml"]

FROM base AS build
COPY backend ./backend
COPY packages ./packages
COPY modules ./modules
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -o /out/api     ./backend/cmd/api \
 && CGO_ENABLED=0 go build -trimpath -o /out/worker  ./backend/cmd/worker \
 && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./backend/cmd/migrate

FROM gcr.io/distroless/static:nonroot AS production
COPY --from=build /out/api /out/worker /out/migrate /
COPY backend/migrations /migrations
USER nonroot:nonroot
ENTRYPOINT ["/api"]
```

The `production` stage never contains the Go toolchain, source code, or a shell — a `distroless` base means there's nothing in the running container an attacker could use to explore the filesystem or install tools even after gaining code execution inside it. It runs as a non-root user, and the same image provides `/worker` and `/migrate` (the entrypoint is overridden per deployment).

```dockerfile
# deploy/docker/web.Dockerfile
ARG NODE_VERSION=24
FROM node:${NODE_VERSION}-alpine AS base
WORKDIR /app
COPY package.json package-lock.json ./
COPY apps/web/package.json apps/web/
COPY packages/ts-sdk/package.json packages/ts-sdk/
COPY packages/ui/package.json packages/ui/
RUN npm ci

FROM base AS dev
CMD ["npm", "run", "dev", "-w", "apps/web", "--", "--host"]

FROM base AS build
COPY apps ./apps
COPY packages ./packages
COPY modules ./modules
RUN npm run build -w apps/web

FROM nginx:alpine AS production
COPY --from=build /app/apps/web/dist /usr/share/nginx/html
COPY deploy/nginx/web.conf /etc/nginx/conf.d/default.conf
```

---

# 4. Native Development

```text
make infra-up       # postgres + redis + mailpit only
make migrate-up     # applies migrations (needs MIGRATION_DATABASE_URL)
make bootstrap-admin EMAIL=you@example.org     # first Super Admin (04 §16)
make run-api        # air-reloaded API, natively
make run-worker     # air-reloaded worker, natively
make frontend       # Vite dev server, natively
```

or, for the common case of wanting all three at once:

```text
make dev            # infra-up, migrate-up, then api + worker + frontend natively in ONE terminal
                    # (scripts/dev.sh supervises them; Ctrl-C stops all of them — no orphan processes)
```

Running Go and Node natively means the edit-compile-test loop never pays container-rebuild overhead — `air` recompiles in place, Vite's dev server hot-reloads — while PostgreSQL, Redis and Mailpit stay containerized because "install and configure PostgreSQL 18 correctly on every contributor's machine" is exactly the kind of setup friction Docker exists to remove and native development doesn't need to re-litigate.

---

# 5. Environment Parity

The same `.env` variable names (10 §8) are used whether the process reading them is inside a container or running natively; only the *values* differ (a container's `DATABASE_URL` points at the `postgres` service name, a native process's points at `localhost`). The same migrations (04) and the same bootstrap command run identically in both modes — there is no separate "dev-only" schema or seed path to keep in sync with the real one.

```text
.env.example    — committed, documents every variable with a safe development value
.env             — gitignored, the contributor's actual local values
```

Development defaults in `.env.example` (native mode):

```text
DATABASE_URL=postgres://app_user:app_dev_pw@localhost:5432/platform
MAINTENANCE_DATABASE_URL=postgres://app_maintenance:maintenance_dev_pw@localhost:5432/platform
MIGRATION_DATABASE_URL=pgx5://app_migrator:migrator_dev_pw@localhost:5432/platform
REDIS_URL=redis://localhost:6379
SESSION_SECRET=dev-only-change-me-dev-only-change-me
COOKIE_NAME=platform_session
COOKIE_SECURE=false
ALLOWED_ORIGINS=http://localhost:5173
PUBLIC_BASE_URL=http://localhost:5173
SMTP_HOST=localhost
SMTP_PORT=1025
SMTP_FROM=platform@localhost
ENVIRONMENT=development
```

`.env.example` never contains a value that would be acceptable in production; the production-mode validation in 10 §8 rejects `COOKIE_SECURE=false` and a short `SESSION_SECRET`.

---

# 6. Same-Origin Serving

The SPA and the API must be served from one origin (08 §2, 11 §3), so cookies stay `SameSite=Lax` without CORS:

- **Development:** the browser talks only to `http://localhost:5173`. Vite proxies `/api` and `/ws` (with WebSocket upgrade) to the API:

```ts
// apps/web/vite.config.ts (excerpt)
server: {
  proxy: {
    '/api': { target: process.env.VITE_API_PROXY_TARGET ?? 'http://localhost:8080', changeOrigin: false },
    '/ws':  { target: process.env.VITE_API_PROXY_TARGET ?? 'http://localhost:8080', ws: true },
  },
}
```

- **Production:** a reverse proxy (Caddy or nginx) terminates TLS on one host, serves the static bundle, and forwards `/api` and `/ws` to the API, setting `X-Forwarded-*` headers (trusted by the API only from `TRUSTED_PROXIES`). It also adds the security headers (CSP `default-src 'self'`, `X-Content-Type-Options`, `Referrer-Policy`, HSTS). The reference configuration is `deploy/nginx/web.conf` / `deploy/caddy/Caddyfile`, finalized in the release milestone.

---

# 7. Production Differences

Production is not "docker-compose with different values" — it's the `production` Dockerfile stages (§3) deployed however the operator chooses (Kubernetes, a single VM with `docker run`, a managed container platform), with:

- `postgres`/`redis` as managed services or separately-operated instances, not sidecar containers
- Secrets from the platform's actual secret manager, not an `.env` file (10 §8, 12 §7)
- The three database roles provisioned by the operator (04 §13); `cmd/migrate` run as a release step with the migrator role, **before** new API/worker versions start
- `api` and `worker` scaled independently (10 §5 keeps them as separate binaries specifically so this is possible without a rewrite)
- A reverse proxy as in §6, and `/metrics` reachable only from the monitoring network (14 §5)

`docker-compose.yml` as written here is a **development** convenience; it is deliberately not the production deployment artifact. The release milestone adds `docker-compose.prod.yml` as a *reference* single-host deployment (with the proxy, no dev tooling, secrets from files), distinct from this file.

---

# 8. Testing Requirements

```text
docker compose --profile full up brings every service to a healthy state within a bounded
  timeout — run as a smoke test in CI (19 §3) on every change to docker-compose.yml or any Dockerfile

make dev, make infra-up, make docker-up, make docker-down all succeed on a clean checkout with
  nothing pre-installed except Docker, Go, and Node — the actual test of "does the onboarding
  instructions work," not just "does someone remember how to set it up"

the production Docker images build from the repository root, contain no shell, run as non-root,
  and start with only the documented environment variables
```
