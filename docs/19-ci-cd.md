# CI/CD
## Open Source Modular Application Platform v1.0

**Document:** 19-ci-cd.md
**Status:** Baseline (Rev 1.1)
**Previous:** 18-testing-strategy.md
**Next:** 20-roadmap.md

---

# 1. Purpose

Every check described in this document runs through the exact `make` targets from 16 — CI is not a second, parallel definition of "passing," it's the same commands a contributor runs locally, executed automatically and enforced before merge. **`make verify` is the local equivalent of the whole pipeline.**

**Platform: GitHub Actions**, as the natural default for a GitHub-hosted open-source project; every stage below is expressed as ordinary shell/`make` calls specifically so moving to GitLab CI or another runner later is a syntax change, not a redesign.

---

# 2. Pipeline Stages

```text
On every pull request:
   lint  →  test  →  migrations (roundtrip)  →  build  →  docker smoke  →  contract check

On merge to main:
   (all of the above)  →  docker-build & push  →  deploy to staging

On a version tag:
   (all of the above)  →  release (§7)
```

Nothing merges to `main` without every PR-stage check passing — there is no "merge now, fix CI after" path.

---

# 3. Workflow

Versions (Go, Node, PostgreSQL, Redis) come from one place — `docs/adr/0001-stack.md` — and are mirrored here. Go is installed from `go.mod` (`go-version-file`), so CI always uses the version the repository declares.

```yaml
# .github/workflows/ci.yml
name: CI
on:
  pull_request:
  push:
    branches: [main]

env:
  NODE_VERSION: "24"

jobs:
  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - uses: actions/setup-node@v4
        with: { node-version: "${{ env.NODE_VERSION }}", cache: npm }
      - run: make install
      - run: make lint

  test:
    runs-on: ubuntu-latest
    # testcontainers starts its own PostgreSQL 18 / Redis 8 per test run (Docker is available on ubuntu runners)
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - uses: actions/setup-node@v4
        with: { node-version: "${{ env.NODE_VERSION }}", cache: npm }
      - run: make install
      - run: make test

  migrations:
    # up → down → up on a throwaway database; also runs the schema constraint/trigger/grant suite
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:18
        env: { POSTGRES_PASSWORD: postgres, POSTGRES_DB: platform }
        ports: ["5432:5432"]
        options: >-
          --health-cmd "pg_isready -U postgres" --health-interval 5s --health-retries 10
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - run: psql "postgres://postgres:postgres@localhost:5432/platform" -f deploy/db/init/00-roles.sql   # CI passwords supplied via psql variables
      - run: make migrate-roundtrip
        env:
          MIGRATION_DATABASE_URL: pgx5://app_migrator:migrator_dev_pw@localhost:5432/platform
      - run: make db-test

  build:
    needs: [lint, test, migrations]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - uses: actions/setup-node@v4
        with: { node-version: "${{ env.NODE_VERSION }}", cache: npm }
      - run: make install
      - run: make build
      - run: make docker-build      # proves the production images build from the repository root

  docker-smoke:
    needs: [build]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: cp .env.example .env
      - run: docker compose --profile full up -d --build
      - run: ./scripts/wait-for-healthy.sh   # polls until every service reports healthy, bounded timeout
      - run: make smoke                       # every milestone smoke script against the running stack
      - run: docker compose --profile full down -v
        if: always()

  contract-check:
    needs: [build]
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - uses: actions/setup-node@v4
        with: { node-version: "${{ env.NODE_VERSION }}", cache: npm }
      - run: make install
      - run: make generate
      - run: git diff --exit-code docs/api packages/ts-sdk backend/cmd/api/modules_gen.go backend/cmd/worker/modules_gen.go apps/web/src/modules_gen.ts
        # fails the build if generated output (OpenAPI, TypeScript SDK, sqlc, module registries)
        # differs from what's committed — the drift check from 08 §9 and 07 §5.2
```

`make verify` (16 §2.7) runs the same lint/test/roundtrip/build/smoke stages locally. Contributors run it before opening a PR; a new development session runs it first to prove the baseline is green.

---

# 4. Dependency Scanning

A separate, scheduled workflow (not on every PR, to avoid noise from upstream advisories unrelated to the current change) plus a blocking check for newly-introduced vulnerable dependencies:

```yaml
# .github/workflows/security.yml
on:
  pull_request:
    paths: ["**/go.sum", "**/package-lock.json", "**/Dockerfile"]
  schedule:
    - cron: "0 6 * * 1"   # weekly

jobs:
  govulncheck:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with: { go-version-file: go.mod }
      - run: go run golang.org/x/vuln/cmd/govulncheck@latest ./...

  npm-audit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with: { node-version: "24" }
      - run: npm ci
      - run: npm audit --audit-level=high

  image-scan:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: make docker-build
      - uses: aquasecurity/trivy-action@master
        with: { image-ref: platform-backend:local, severity: "CRITICAL,HIGH", exit-code: "1" }
```

A known-vulnerable dependency introduced by a PR fails that PR's checks (12 §8) — the scheduled weekly run catches a vulnerability disclosed *after* a dependency was already merged, which the PR-triggered check alone would miss. Third-party actions are pinned to a commit SHA in the real workflow files.

---

# 5. Migration Rollback Verification

Referenced as a requirement in 04 §15.1; the CI job is `migrations` in §3: it applies the roles script, then runs `make migrate-roundtrip` (up → down → up) and the schema suite (`make db-test`).

Running unconditionally keeps the job simple; a path filter (`backend/migrations/**`, `deploy/db/**`) may be added later as an optimization if the job becomes slow — never at the cost of skipping it on a migration change.

A migration that has been tagged in a release is never edited; the roundtrip also guards that rule by failing when a previously applied migration file's checksum changes (the migration tool records and compares it).

---

# 6. Module CI (Forward-Looking)

Modules are in-tree today (10 §4.1), so their tests, the isolation lint, the `attach_updated_at` check and the route-declaration check already run under the same `lint` and `test` jobs as core (18 §6 explains why `go test ./...` picks them up automatically). Once `module-sdk` is published as an independently versioned package and modules can live in separate repositories, an out-of-tree module's own CI is expected to run the same shape of pipeline against a pinned `module-sdk` version — a template workflow for that day belongs in `packages/module-sdk/` itself once it exists, not invented speculatively here.

---

# 7. Release Process

```text
Tag pushed (vX.Y.Z, semver)
     │
     ▼
Full pipeline (§2) runs against the tag
     │
     ▼
docker build & push, tagged both :X.Y.Z and :latest (backend image carries api, worker and migrate)
     │
     ▼
Changelog generated from merged PR titles/labels since the last tag
     │
     ▼
GitHub Release created, changelog attached, migration list for this
release called out explicitly (so an operator upgrading knows what
`migrate up` is about to apply — and that it must run, with the migrator role,
BEFORE the new api/worker versions start)
```

A breaking API change (08 §2.1) or a breaking module manifest change is a major version bump — nothing else in this documentation series overrides semver's ordinary meaning. Images are signed and an SBOM is attached to the release (tooling chosen in the release milestone).

---

# 8. Branch Protection

```text
main is protected:
  - lint, test, migrations, build, docker-smoke, and contract-check are all required checks
  - at least one review approval required
  - no direct pushes, no force-push, no bypass — including for maintainers
```

The last line matters specifically for an open-source project: a maintainer bypassing their own CI on their own PR is exactly how "the pipeline is authoritative" quietly stops being true.

---

# 9. Testing Requirements

```text
A PR that fails any required check cannot be merged, verified by attempting
  to merge a deliberately-broken test PR against branch protection
docker-smoke actually fails when a service's healthcheck is broken (verified
  once by deliberately breaking one, confirming red, then fixing it)
contract-check fails when generated output (swagger, ts-sdk, module registries) is
  edited by hand without regenerating it from source (08 §9, 07 §5.2)
migrations fails when a .down.sql does not cleanly reverse its .up.sql
the production image build fails if the build context omits go.mod/modules (guards 15 §3)
```
