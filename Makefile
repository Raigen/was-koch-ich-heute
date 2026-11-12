.PHONY: dev backend frontend build test docker docker-up

# Backend (Port 8080) und Vite-Dev-Server (Port 5173) parallel starten
dev:
	@$(MAKE) -j2 backend frontend

backend:
	cd backend && go run ./cmd/server

frontend:
	cd frontend && npm install && npm run dev

# Ein einzelnes Binary bauen, das auch das Frontend ausliefert
build:
	cd frontend && npm ci && npm run build
	cd backend && go build -o ../bin/server ./cmd/server
	@echo "Start: STATIC_DIR=frontend/dist ./bin/server"

test:
	cd backend && go test ./...
	cd frontend && npm run typecheck

# Docker-Image bauen bzw. mit docker compose starten
docker:
	docker build -t was-koch-ich-heute .

docker-up:
	docker compose up -d --build
