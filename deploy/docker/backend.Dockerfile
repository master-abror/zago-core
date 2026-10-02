# Build context = ROOT repo (docs/15 §3): butuh go.mod, backend/, packages/module-sdk, modules/.
ARG GO_VERSION=1.26.5
FROM golang:${GO_VERSION}-alpine AS base
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

FROM base AS dev
# Source di-bind-mount oleh docker-compose.yml; air (dipatok lewat `tool` di go.mod) mengompilasi ulang saat berubah.
CMD ["go", "tool", "air", "-c", ".air.api.toml"]

FROM base AS build
COPY backend ./backend
COPY packages ./packages
COPY modules ./modules
RUN CGO_ENABLED=0 go build -trimpath -o /out/api     ./backend/cmd/api \
 && CGO_ENABLED=0 go build -trimpath -o /out/worker  ./backend/cmd/worker \
 && CGO_ENABLED=0 go build -trimpath -o /out/migrate ./backend/cmd/migrate

FROM gcr.io/distroless/static:nonroot AS production
COPY --from=build /out/api /out/worker /out/migrate /
COPY backend/migrations /migrations
USER nonroot:nonroot
ENTRYPOINT ["/api"]
