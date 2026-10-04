# Build context = ROOT repo (docs/15 §3): butuh go.mod, backend/, packages/module-sdk, modules/.
# Cache modul dan cache build memakai BuildKit cache mount: TIDAK menjadi layer image (ADR-0003).
# `go mod download` sengaja tidak dipakai: ia akan menarik seluruh dependensi alat dev
# (golangci-lint, sqlc, swag) ke dalam image dan membuat build berjalan puluhan menit.
ARG GO_VERSION=1.26.5
FROM golang:${GO_VERSION}-alpine AS base
WORKDIR /app
COPY go.mod go.sum ./

FROM base AS dev
# air dibangun SEKALI ke dalam image (dipatok lewat `tool` di go.mod). Source di-bind-mount oleh
# docker-compose.yml; modul aplikasi diunduh saat container pertama berjalan ke volume go_mod_cache.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o /usr/local/bin/air github.com/air-verse/air
CMD ["air", "-c", ".air.api.toml"]

FROM base AS build
COPY backend ./backend
COPY packages ./packages
COPY modules ./modules
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -o /out/api     ./backend/cmd/api \
 && CGO_ENABLED=0 go build -trimpath -o /out/worker  ./backend/cmd/worker \
 && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./backend/cmd/migrate

FROM gcr.io/distroless/static:nonroot AS production
COPY --from=build /out/api /out/worker /out/migrate /
COPY backend/migrations /migrations
USER nonroot:nonroot
ENTRYPOINT ["/api"]
