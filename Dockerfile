# syntax=docker/dockerfile:1

# Die Build-Stufen laufen immer auf der Architektur des Build-Rechners
# ($BUILDPLATFORM). Go cross-kompiliert für die Zielarchitektur; so wird
# z. B. auf einem Apple-Silicon-Mac ein amd64-Image ohne langsame
# QEMU-Emulation gebaut.

# ---- 1. Frontend bauen (React/Vite) -----------------------------------------
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

# ---- 2. Backend bauen (Go, statisch gelinkt) --------------------------------
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS backend
ARG TARGETOS TARGETARCH
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend/ ./
# Der Build-Cache bleibt zwischen Builds erhalten: Nur der erste Build
# kompiliert das (große) Anthropic-SDK, danach dauert es Sekunden.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- 3. Schlankes Laufzeit-Image --------------------------------------------
# distroless/static enthält CA-Zertifikate (für HTTPS zu Claude/Mistral) und
# läuft als Nicht-root-Benutzer.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=backend /out/server /app/server
COPY --from=frontend /src/frontend/dist /app/web

ENV ADDR=:8080 \
    STATIC_DIR=/app/web
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/server"]
