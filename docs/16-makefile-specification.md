# Makefile Specification
## Open Source Modular Application Platform v1.0

**Document:** 16-makefile-specification.md
**Status:** Baseline (Rev 1.1)
**Previous:** 15-docker-native-development.md
**Next:** 17-module-developer-guide.md

---

# 1. Purpose

The `Makefile` is the one interface a contributor, a module author, and CI all use identically. If CI runs a different set of commands than what's documented for local use, "passes locally, fails in CI" becomes a recurring, trust-eroding experience — so every target below is exactly what 19's pipeline calls, not a parallel set invented for automation.

**Note on syntax:** recipe lines in a Makefile must start with a **TAB** character, not spaces. The recipes below use tabs; if you copy them through a tool that converts tabs to spaces, `make` will fail with "missing separator."

**Tools are pinned, not assumed.** Go-based tools (`golangci-lint`, `swag`, `sqlc`, `air`, `goimports`) are declared with `tool` directives in `go.mod` and run as `go tool <name>`; migrations run through the repository's own `cmd/migrate` binary. A clean machine therefore needs only Docker, Go and Node.

---

# 2. Full Target Reference

## 2.1 Onboarding

```makefile
SHELL := /bin/bash
.DEFAULT_GOAL := help

help:            ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*## "}; {printf "%-22s %s\n", $$1, $$2}'

setup:           ## First-time setup: deps, .env, datastores, migrations
	cp -n .env.example .env || true
	$(MAKE) install
	$(MAKE) infra-up
	$(MAKE) migrate-up

install:         ## Install backend and frontend dependencies
	go mod download
	npm ci

tools:           ## Verify pinned developer tools resolve (go tool …)
	go tool golangci-lint --version
	go tool swag --version
	go tool sqlc version
```

`make setup` is deliberately the **one command** a brand-new contributor runs — a README that says "run these seven commands in this exact order" is a README that goes stale the first time step four changes. After it, `make bootstrap-admin EMAIL=…` (2.4) creates the first Super Admin.

## 2.2 Running

```makefile
infra-up:        ## Start postgres + redis + mailpit (native development)
	docker compose up -d postgres redis mailpit

dev:             ## infra + migrations, then api + worker + frontend natively in one terminal
	$(MAKE) infra-up
	$(MAKE) migrate-up
	./scripts/dev.sh

run-api:         ## Run the API natively with hot reload
	go tool air -c .air.api.toml

run-worker:      ## Run the worker natively with hot reload
	go tool air -c .air.worker.toml

frontend:        ## Run the Svelte dev server natively
	npm run dev -w apps/web
```

`scripts/dev.sh` starts `run-api`, `run-worker` and `frontend` as children and installs `trap 'kill 0' EXIT` so Ctrl-C stops all of them — the old pattern of backgrounding two processes with `&` inside a recipe left orphans.

## 2.3 Testing & Quality

```makefile
test:            ## Full test suite: backend (core + module-sdk + every module) and frontend
	$(MAKE) test-backend
	$(MAKE) test-frontend

test-backend:    ## Go unit + integration tests (needs Docker for testcontainers)
	go test ./... -race -count=1

test-frontend:   ## Frontend component tests
	npm test -w apps/web

test-e2e:        ## Playwright end-to-end tests against a running stack
	npm run e2e -w apps/web

lint:            ## Lint backend and frontend, plus structural checks
	go tool golangci-lint run ./...
	npm run lint -w apps/web
	./scripts/lint-structure.sh

format:          ## Auto-format backend and frontend
	gofmt -w backend packages modules
	go tool goimports -w backend packages modules
	npm run format -w apps/web

generate:        ## Regenerate OpenAPI, the TypeScript SDK, sqlc code and the module registries
	go tool swag init -g backend/cmd/api/main.go -o docs/api --parseDependency
	go tool sqlc generate
	npm run generate -w packages/ts-sdk
	$(MAKE) modules-sync

modules-sync:    ## Regenerate module registries from modules/*/module.yaml (07 §5.2)
	go run ./backend/cmd/modulegen
```

`go test ./...` run from the repository root covers core, `packages/module-sdk` **and every module** (one `go.mod`, 10 §2) — a module's tests are not a separate, optional suite a contributor has to remember to also run. `make test` is "is the whole platform, core and every installed module, actually working," not just "is core working."

`scripts/lint-structure.sh` runs the structural checks that golangci-lint can't: module table-prefix isolation (07 §6), `attach_updated_at` present for every table with `updated_at` (04 §4, via the catalog test), every module route declares `Require`/`Public` (07 §5.1), and that generated files are committed.

## 2.4 Database

```makefile
migrate-up:      ## Apply all pending core migrations (MIGRATION_DATABASE_URL)
	go run ./backend/cmd/migrate up

migrate-down:    ## Roll back ONE core migration
	go run ./backend/cmd/migrate down 1

migrate-version: ## Show the installed migration version
	go run ./backend/cmd/migrate version

db-seed:         ## Ensure the platform row exists (idempotent, 04 §16)
	go run ./backend/cmd/migrate seed

migrate-roundtrip: ## up → down(all) → up on a throwaway database in a throwaway PostgreSQL container (ADR-0005); used by CI
	bash ./scripts/migrate-roundtrip.sh

db-test:         ## Schema constraint/trigger/cascade/grant + seeder tests (04 §17); needs Docker, fails (not skips) without it
	REQUIRE_DOCKER=1 go test ./backend/migrations/... -race -count=1

bootstrap-admin: ## Create the first Super Admin: make bootstrap-admin EMAIL=you@example.org
	@test -n "$(EMAIL)" || (echo "usage: make bootstrap-admin EMAIL=<email>" && exit 1)
	go run ./backend/cmd/api bootstrap-admin --email "$(EMAIL)"
```

`cmd/migrate` embeds golang-migrate as a library (04 §15) and reads `MIGRATION_DATABASE_URL` (role `app_migrator`, `pgx5://` scheme) — there is no separate CLI to install. `bootstrap-admin` reads the password from a prompt/stdin or prints a generated one once; never from a file.

## 2.5 Build

```makefile
build:           ## Compile production binaries and the frontend bundle
	CGO_ENABLED=0 go build -trimpath -o bin/api     ./backend/cmd/api
	CGO_ENABLED=0 go build -trimpath -o bin/worker  ./backend/cmd/worker
	CGO_ENABLED=0 go build -trimpath -o bin/migrate ./backend/cmd/migrate
	npm run build -w apps/web
```

## 2.6 Docker

```makefile
docker-up:       ## Start the FULL stack in Docker (profile full): datastores, migrate, api, worker, web
	docker compose --profile full up -d --build

docker-down:     ## Stop and remove all compose containers (keeps data)
	docker compose --profile full down

docker-build:    ## Build production images (build context = repository root)
	docker build -f deploy/docker/backend.Dockerfile --target production -t platform-backend:local .
	docker build -f deploy/docker/web.Dockerfile --target production -t platform-web:local .
```

`infra-up` (2.2) is "datastores only"; `docker-up` is "everything" — one meaning each, matching 15 §2.

## 2.7 Smoke tests and verification

```makefile
smoke:           ## Run every milestone smoke script against a running stack
	@for f in scripts/smoke/m*.sh; do echo "== $$f"; bash $$f || exit 1; done

verify:          ## The single gate: lint + test + migrate-roundtrip + build + smoke
	$(MAKE) lint
	$(MAKE) test
	$(MAKE) migrate-roundtrip
	$(MAKE) build
	./scripts/verify-smoke.sh
```

`make verify` is **the one command** used by CI (19), by a contributor before opening a pull request, and by a new chat/session at the start of a milestone to prove the previous milestone's work is actually green (22 §4). `scripts/verify-smoke.sh` brings up a throwaway stack (infra, migrate, api, worker), runs `make smoke`, and tears it down.

## 2.8 Cleanup

```makefile
clean:           ## Remove build artifacts and containers AND data volumes
	rm -rf bin/ apps/web/dist/
	docker compose --profile full down -v
```

`-v` on `docker compose down` here (and only here) also drops the PostgreSQL data volume — `clean` is the "start completely fresh" target, distinct from the everyday `docker-down` which preserves data across a normal stop/start.

## 2.9 Module Scaffolding

```makefile
module-create:   ## Scaffold a new module: make module-create NAME=announcements
	@test -n "$(NAME)" || (echo "usage: make module-create NAME=<module-code>" && exit 1)
	go run ./backend/cmd/modulegen scaffold $(NAME)
	$(MAKE) modules-sync
```

Generates the structure and stub files described in 07 §12 and 17 §3. The scaffolder is a Go program (`backend/cmd/modulegen`), not an embedded Makefile or shell script, so it can be tested and reads as normal code; `NAME` is validated against the module-code format (`^[a-z][a-z0-9_]{1,49}$`, 04 §7).

## 2.10 Handover

```makefile
handover-pack:   ## Package files for a new chat: make handover-pack M=03
	@test -n "$(M)" || (echo "usage: make handover-pack M=<NN>" && exit 1)
	./scripts/handover-pack.sh $(M)

repo-zip:        ## Whole-repo zip (no node_modules/bin/dist/.env/data) to upload to the next chat
	./scripts/repo-zip.sh
```

---

# 3. CI Uses These Same Targets

19's pipeline calls `make lint`, `make test`, `make migrate-roundtrip`, `make build`, `make generate` (and `make docker-build` for image publishing) — never a separately maintained set of CI-only commands. If a target needs different behavior in CI (for example, tests against service containers instead of testcontainers), that's an environment variable the target itself branches on, not a second target that quietly diverges from the one contributors actually run day to day.

---

# 4. Testing Requirements

```text
make help lists every target below and none are undocumented
make setup succeeds on a completely clean checkout with only Docker, Go and Node installed
  (15 §8) — including every pinned tool resolving through `go tool`
make test-backend actually picks up and runs a newly-scaffolded module's tests
  without any manual wiring — verified by scaffolding a throwaway module with
  make module-create and confirming `make test` includes it
make dev leaves no orphan processes after Ctrl-C
make modules-sync is idempotent: a second run produces no diff
make verify fails (non-zero) when any one of its stages fails
```
