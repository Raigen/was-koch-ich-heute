# AGENTS.md

## Project
"Was koch ich heute?" ("What should I cook today?"): find recipes via AI web research, look up the
ingredients in German supermarket web shops (REWE, EDEKA, ALDI, Lidl, PENNY, Kaufland) and compare prices.
- Frontend: React 19 + TypeScript + Vite (`frontend/`)
- Backend: Go 1.25, standard library + Anthropic Go SDK only (`backend/`)
- This repository is a public portfolio project: keep code, docs and history clean and readable.

## Commands
- Backend: `cd backend && go vet ./... && go test -race ./...`
- Frontend: `cd frontend && npm run typecheck && npm run build`
- Both: `make test`
- Run locally: `make dev` (backend on :8080, Vite on :5173 proxying `/api`)
- Docker: `docker compose up -d --build` (the build uses `network: host`; needed on WSL)
- Backend tests and the frontend type check must pass before every commit.
- Run `gofmt` on changed Go files.
- CI (`.github/workflows/ci.yml`) runs the same checks plus a Docker build and smoke test on every
  pull request; keep it green.

## Language
- Everything in the repository is English: code, comments, docs, tests, log messages, commit
  messages, issues and pull requests.
- Exception: **user-facing text stays German**, because the app targets German supermarkets.
  This includes:
  - UI strings in `frontend/src`,
  - error messages returned to the client (`writeError`, researcher errors shown in the UI),
  - demo data,
  - AI output values.
- AI prompts are English but instruct the model to write all values in German
  (`internal/research/prompts.go`). The enum values `hoch|mittel|niedrig` (confidence) and
  `einfach|mittel|anspruchsvoll` (difficulty) are displayed as-is and used as CSS classes: do not translate them.

## Architecture
- `internal/research`: the `Researcher` interface with Claude, Mistral and demo implementations.
  - Prompts and JSON parsers in `prompts.go` are shared by all providers.
  - New providers implement the interface and are registered in `research.New`.
  - Both research methods also return `model.Usage` (tokens, web searches), summed over all API calls;
    the cost is computed from the price table in `internal/pricing/cost.go`, never by the provider.
- `internal/pricing`: package counts, costs and totals.
  - **Never let the AI do arithmetic.** The AI returns raw data (price, package size); the backend computes everything else.
- `internal/api`: HTTP routes.
  - `/api/prices/compare` streams NDJSON events: `store`, `summary`, `ping`.
  - Keep that format stable: the frontend parses it line by line.
- `internal/model/model.go` and `frontend/src/types.ts` mirror each other. **Always change both.**

## Conventions
- Avoid new dependencies.
  - Backend: prefer the standard library.
  - Frontend: no UI framework; plain CSS with the custom properties in `styles.css`, and support dark mode.
- Demo mode (no API key) must always work; it is how reviewers try the app.
- Configuration only via environment variables. Document new variables in `.env.example` and in the
  README configuration table.

## AI providers
- **Claude**
  - Default model `claude-opus-5-5`, web tools `web_search_20260209` / `web_fetch_20260209`, search
    restricted to the shop's domains.
  - `pause_turn` responses are resumed, and server-side refusal fallbacks are enabled.
  - Do not change model IDs or tool versions without checking the current Anthropic docs.
- **Mistral**
  - Conversations API via plain REST (there is no official Go SDK).
  - Its web search cannot be restricted to domains, so links to other sites are dropped in the backend.
- **Tests never call real APIs** (that costs money). Use fake servers (`httptest`) as in
  `research_test.go` and `mistral_test.go`.

## Cost and shops
- Every price comparison costs several web searches per shop. Do not raise the `MaxUses` limits or
  bypass the cache without a reason.
- The backend never fetches shop pages itself (no own scraping). Data comes only from the AI
  providers' web tools or open data sources.
- Time limits:
  - `STORE_TIMEOUT` applies per shop, and the stream sends a `ping` every 15 s.
  - Long-running work must not block the stream.

## Security
- Never commit API keys or `.env`. Never log keys or include them in error messages.
- Validate and limit user input (lengths and counts, see `handleRecipeSearch` / `handlePriceCompare`).

## Git
- Work on a branch and open a pull request to `main`.
- Planned work is tracked in the GitHub issues.
