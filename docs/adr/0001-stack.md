# ADR-0001 — Stack dan versi

**Status:** Diterima (versi patch dipatok saat M00)
**Tanggal:** 2026-09-30

## Keputusan
| Area | Pilihan |
|---|---|
| Go | stable terbaru; `go` + `toolchain` dipatok di go.mod; CLI dev dipatok lewat `tool` directive |
| HTTP router | chi v5 |
| PostgreSQL driver | pgx v5 + pgxpool |
| Query | sqlc (target pgx/v5) + SQL tulisan tangan; tanpa ORM |
| Migrasi | golang-migrate sebagai library (`cmd/migrate`, URL `pgx5://`) |
| PostgreSQL | 18.x (`postgres:18`) — `uuidv7()` native |
| Redis | 8.x (`redis:8`); kode hanya memakai perintah dasar sehingga kompatibel dengan Valkey |
| Redis client | go-redis v9 |
| WebSocket | github.com/coder/websocket |
| Logging | log/slog (JSON) |
| Telemetry | OpenTelemetry + Prometheus |
| Config | caarlos0/env v11 |
| Validasi | go-playground/validator v10 |
| UUID | google/uuid (NewV7) lewat pkg/id |
| Password | x/crypto/argon2 (Argon2id) |
| Email | SMTP (go-mail); dev: Mailpit |
| API docs | swaggo/swag → swagger.json → packages/ts-sdk |
| Tes | testify, testcontainers-go, Vitest, Playwright |
| Frontend | Svelte 5 (runes) + Vite + TypeScript strict, SPA murni, same-origin; router/UI kit/i18n via ADR di M10 |
| Node | LTS aktif (24) |

## Catatan lisensi
Redis 8 dirilis dengan opsi lisensi RSALv2/SSPLv1/AGPLv3; Valkey adalah fork berlisensi BSD. Jelaskan pilihan di README.

## Versi patok (diisi M00, lihat ADR-0002)
- Go: 1.26.5 (di go.mod)
- Node: 24 (`.nvmrc`; target 24.21.0)
- PostgreSQL: 18.x — tag image `postgres:18`; patch persis TERBUKA (ADR-0002)
- Redis: 8.x — tag image `redis:8`; patch persis TERBUKA (ADR-0002)
