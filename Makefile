# Makefile — satu-satunya antarmuka untuk kontributor, modul, dan CI (docs/16).
# CATATAN: baris resep HARUS diawali TAB, bukan spasi.
SHELL := /bin/bash
.DEFAULT_GOAL := help

# Muat .env (bila ada) dan ekspor ke semua resep: proses native membaca variabel yang sama
# dengan container (docs/15 §5). Di CI .env tidak ada; nilai datang dari environment job.
ifneq (,$(wildcard .env))
include .env
export
endif

# Alat Go dipatok lewat `tool` directive di go.mod; jalankan sebagai `go tool <nama>`.
GO_TOOL_PACKAGES := \
	github.com/air-verse/air \
	github.com/golangci/golangci-lint/v2/cmd/golangci-lint \
	github.com/sqlc-dev/sqlc/cmd/sqlc \
	github.com/swaggo/swag/cmd/swag \
	golang.org/x/tools/cmd/goimports

.PHONY: help setup install tools tools-pin infra-up dev run-api run-worker frontend \
	test test-backend test-frontend test-e2e lint format generate modules-sync \
	migrate-up migrate-down migrate-roundtrip db-test bootstrap-admin \
	build docker-up docker-down docker-build smoke verify clean \
	module-create handover-pack repo-zip

help:            ## Tampilkan bantuan ini
	@grep -E '^[a-zA-Z_-]+:.*## ' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*## "}; {printf "%-22s %s\n", $$1, $$2}'

# ---------- 2.1 Onboarding ----------
setup:           ## Setup pertama kali: deps, .env, datastore, migrasi
	test -f .env || cp .env.example .env
	$(MAKE) install
	$(MAKE) infra-up
	$(MAKE) migrate-up

install:         ## Pasang dependensi backend dan frontend
	go mod download
	npm ci

tools:           ## Pastikan alat dev yang dipatok bisa dijalankan (go tool ...)
	go tool golangci-lint --version
	go tool swag --version
	go tool sqlc version
	go tool air -v

tools-pin:       ## SEKALI (atau saat upgrade): pasang/patok alat dev ke go.mod + go.sum lalu rapikan
	go get -tool $(addsuffix @latest,$(GO_TOOL_PACKAGES))
	go mod tidy

# ---------- 2.2 Menjalankan ----------
infra-up:        ## Jalankan postgres + redis + mailpit (pengembangan native), tunggu sehat
	docker compose up -d --wait postgres redis mailpit

dev:             ## infra + migrasi, lalu api + worker + frontend native dalam SATU terminal
	$(MAKE) infra-up
	$(MAKE) migrate-up
	bash ./scripts/dev.sh

run-api:         ## Jalankan API native dengan hot reload
	go tool air -c .air.api.toml

run-worker:      ## Jalankan worker native dengan hot reload
	go tool air -c .air.worker.toml

frontend:        ## Jalankan dev server Svelte native
	npm run dev -w apps/web

# ---------- 2.3 Tes & kualitas ----------
test:            ## Seluruh tes: backend (core + module-sdk + semua modul) dan frontend
	$(MAKE) test-backend
	$(MAKE) test-frontend

test-backend:    ## Tes Go unit + integrasi (butuh Docker untuk testcontainers)
	go test ./... -race -count=1

test-frontend:   ## Tes komponen/logika frontend
	npm test -w apps/web

test-e2e:        ## Tes end-to-end Playwright terhadap stack yang berjalan (mulai M10)
	npm run e2e -w apps/web

lint:            ## Lint backend dan frontend + pemeriksaan struktural
	go tool golangci-lint run ./...
	npm run lint -w apps/web
	bash ./scripts/lint-structure.sh

format:          ## Format otomatis backend dan frontend
	gofmt -w backend packages modules
	go tool goimports -w backend packages modules
	npm run format -w apps/web

# `generate` dan `modules-sync` bertahap (ADR-0003): generator baru punya masukan di milestone
# yang disebut di bawah. Sebelum itu target tetap ada, berhasil, dan tidak menghasilkan diff.
generate:        ## Regenerasi OpenAPI, SDK TypeScript, kode sqlc, registri modul (bertahap)
	@if [ -f sqlc.yaml ]; then go tool sqlc generate; else echo "sqlc: dilewati (belum ada sqlc.yaml; dimulai M02, docs/21)"; fi
	@echo "swag: dilewati (anotasi handler pertama datang di M02, docs/21)"
	npm run generate -w packages/ts-sdk
	$(MAKE) modules-sync

modules-sync:    ## Regenerasi registri modul dari modules/*/module.yaml (modulegen dimulai M14)
	@if [ -d backend/cmd/modulegen ]; then go run ./backend/cmd/modulegen; else echo "modules-sync: dilewati (backend/cmd/modulegen belum ada; docs/21)"; fi

# ---------- 2.4 Database ----------
migrate-up:      ## Terapkan semua migrasi inti yang tertunda (MIGRATION_DATABASE_URL)
	go run ./backend/cmd/migrate up

migrate-down:    ## Mundurkan SATU migrasi inti
	go run ./backend/cmd/migrate down 1

migrate-roundtrip: ## up -> down(semua) -> up pada database SEMENTARA (dibuat & dihapus sendiri)
	go run ./backend/cmd/migrate roundtrip

db-test:         ## Tes constraint/trigger/grant skema (docs/04 §17; mulai M01)
	@if find backend/migrations -name '*_test.go' 2>/dev/null | grep -q .; then \
		go test ./backend/migrations/... -count=1; \
	else \
		echo "db-test: dilewati (belum ada tes skema di backend/migrations; dimulai M01, docs/21)"; \
	fi

bootstrap-admin: ## Buat Super Admin pertama: make bootstrap-admin EMAIL=you@example.org (mulai M03)
	@test -n "$(EMAIL)" || (echo "usage: make bootstrap-admin EMAIL=<email>" && exit 1)
	@echo "bootstrap-admin belum diimplementasikan (milestone M03, docs/21)" && exit 1

# ---------- 2.5 Build ----------
build:           ## Kompilasi binary produksi dan bundle frontend
	CGO_ENABLED=0 go build -trimpath -o bin/api     ./backend/cmd/api
	CGO_ENABLED=0 go build -trimpath -o bin/worker  ./backend/cmd/worker
	CGO_ENABLED=0 go build -trimpath -o bin/migrate ./backend/cmd/migrate
	npm run build -w apps/web

# ---------- 2.6 Docker ----------
docker-up:       ## Jalankan stack PENUH di Docker (profile full)
	docker compose --profile full up -d --build

docker-down:     ## Hentikan dan hapus semua container compose (data dipertahankan)
	docker compose --profile full down

docker-build:    ## Build image produksi (build context = root repo)
	docker build -f deploy/docker/backend.Dockerfile --target production -t platform-backend:local .
	docker build -f deploy/docker/web.Dockerfile --target production -t platform-web:local .

# ---------- 2.7 Smoke & verifikasi ----------
smoke:           ## Jalankan semua smoke script milestone terhadap stack yang berjalan
	@for f in scripts/smoke/m*.sh; do echo "== $$f"; bash $$f || exit 1; done

verify:          ## Gerbang tunggal: lint + test + migrate-roundtrip + build + smoke
	@test -f .env || { cp .env.example .env && echo "verify: .env dibuat dari .env.example (nilai pengembangan)"; }
	@test -f node_modules/.package-lock.json || { echo "verify: node_modules belum ada/lengkap; menjalankan npm ci"; npm ci; }
	$(MAKE) lint
	$(MAKE) test
	$(MAKE) migrate-roundtrip
	$(MAKE) build
	bash ./scripts/verify-smoke.sh

# ---------- 2.8 Bersih-bersih ----------
clean:           ## Hapus artefak build dan container BESERTA volume data
	rm -rf bin/ apps/web/dist/
	docker compose --profile full down -v

# ---------- 2.9 Scaffolding modul (modulegen dimulai M14) ----------
module-create:   ## Scaffold modul baru: make module-create NAME=announcements
	@test -n "$(NAME)" || (echo "usage: make module-create NAME=<module-code>" && exit 1)
	@test -d backend/cmd/modulegen || (echo "module-create belum tersedia (backend/cmd/modulegen datang di M14, docs/21)" && exit 1)
	go run ./backend/cmd/modulegen scaffold $(NAME)
	$(MAKE) modules-sync

# ---------- 2.10 Handover ----------
handover-pack:   ## Kemas berkas untuk chat baru: make handover-pack M=03
	@test -n "$(M)" || (echo "usage: make handover-pack M=<NN>" && exit 1)
	bash ./scripts/handover-pack.sh $(M)

repo-zip:        ## Zip repo lengkap (tanpa node_modules/bin/dist/.env/data) untuk chat berikutnya
	bash ./scripts/repo-zip.sh
