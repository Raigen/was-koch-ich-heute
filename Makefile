.PHONY: dev backend frontend build test docker docker-up

# Start the backend (port 8080) and the Vite dev server (port 5173) in parallel
dev:
	@$(MAKE) -j2 backend frontend

backend:
	cd backend && go run ./cmd/server

frontend:
	cd frontend && npm install && npm run dev

# Build a single binary that also serves the frontend
build:
	cd frontend && npm ci && npm run build
	cd backend && go build -o ../bin/server ./cmd/server
	@echo "Run: STATIC_DIR=frontend/dist ./bin/server"

test:
	cd backend && go test ./...
	cd frontend && npm run typecheck

# Build the Docker image / start with docker compose
docker:
	docker build --network host -t was-koch-ich-heute .

docker-up:
	docker compose up -d --build
