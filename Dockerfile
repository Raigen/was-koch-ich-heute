# syntax=docker/dockerfile:1

# ---- 1. Frontend bauen (React/Vite) -----------------------------------------
FROM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# ---- 2. Backend bauen (Go, statisch gelinkt) --------------------------------
FROM golang:1.25-alpine AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

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
