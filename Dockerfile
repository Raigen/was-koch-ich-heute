# syntax=docker/dockerfile:1

# The build stages always run on the build machine's architecture
# ($BUILDPLATFORM). Go cross-compiles for the target architecture, so e.g.
# an amd64 image is built on an Apple Silicon Mac without slow QEMU emulation.

# ---- 1. Build the frontend (React/Vite) --------------------------------------
FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

# ---- 2. Build the backend (Go, statically linked) ---------------------------
FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS backend
ARG TARGETOS TARGETARCH
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend/ ./
# The build cache persists between builds: only the first build compiles the
# (large) Anthropic SDK; afterwards it takes seconds.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# ---- 3. Slim runtime image --------------------------------------------------
# distroless/static contains CA certificates (for HTTPS to Claude/Mistral) and
# runs as a non-root user.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=backend /out/server /app/server
COPY --from=frontend /src/frontend/dist /app/web

ENV ADDR=:8080 \
    STATIC_DIR=/app/web
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/server"]
