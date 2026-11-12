# Was koch ich heute? 🍲

Weboberfläche für die günstige Rezeptsuche: Eine KI recherchiert Rezepte im Web, sucht die Zutaten in den
Online-Shops bzw. Online-Angeboten von REWE, EDEKA, ALDI SÜD/Nord, Lidl, PENNY und Kaufland und vergleicht
die Preise – pro Shop und als günstigster Mix über alle Shops.

- **Frontend:** React 19 + TypeScript mit Vite (`frontend/`)
- **Backend:** Go (nur Standardbibliothek + Anthropic Go SDK) (`backend/`)
- **KI:** austauschbar über ein Interface. Eingebaut sind
  - **Claude** (Anthropic) mit serverseitiger Web-Suche (`web_search`/`web_fetch`),
  - **Mistral** über die Conversations API mit dem eingebauten Werkzeug `web_search`,
  - ein **Demo-Modus** mit Beispieldaten, wenn kein API-Key gesetzt ist.

## Schnellstart

### Mit Docker (kein Go/Node nötig)

```bash
cp .env.example .env   # API-Key eintragen; ohne Key: Demo-Modus
docker compose up -d --build
```

Dann <http://localhost:8080> öffnen. Ohne Compose:

```bash
docker build --network host -t was-koch-ich-heute .
docker run -d -p 8080:8080 --env-file .env --name was-koch-ich-heute was-koch-ich-heute
```

Das Image wird in drei Stufen gebaut (Node baut das Frontend, Go das Backend) und enthält am Ende nur
das statische Go-Binary plus Frontend auf `distroless` (ca. 25 MB, läuft als Nicht-root-Benutzer).

Der Build nutzt das Netzwerk des Hosts (`--network host` bzw. `network: host` in der Compose-Datei).
Unter Windows/WSL hängen `npm ci` und `go mod download` im normalen Docker-Netz sonst oft minutenlang.

### Lokal entwickeln

Voraussetzungen: Go ≥ 1.25, Node.js ≥ 20.

```bash
cp .env.example .env              # optional
export ANTHROPIC_API_KEY=sk-ant-… # oder: export MISTRAL_API_KEY=…  (ohne Key: Demo-Modus)
make dev
```

Dann <http://localhost:5173> öffnen. Vite leitet `/api` an das Go-Backend auf Port 8080 weiter.

Produktion (ein Binary, das auch das Frontend ausliefert):

```bash
make build
STATIC_DIR=frontend/dist ./bin/server
```

## Ablauf

1. **Rezeptsuche** – `POST /api/recipes/search`: Claude sucht auf deutschsprachigen Rezeptseiten,
   rechnet auf die Portionenzahl um und markiert Grundzutaten (Salz, Öl …).
2. **Zutaten wählen** – Was schon im Haus ist, wird abgehakt.
3. **Preisvergleich** – `POST /api/prices/compare`: Pro Shop läuft parallel eine Recherche, deren
   Web-Suche auf die Domains des Shops beschränkt ist. Die Antwort wird als NDJSON gestreamt, damit
   jedes Shop-Ergebnis sofort erscheint. Packungsanzahl, Kosten und Summen rechnet das Go-Backend
   (`internal/pricing`), nicht die KI.

Ergebnisse werden im Speicher gecacht (Rezepte 24 h, Preise 6 h), um API-Kosten zu sparen.

## Projektstruktur

```
backend/
  cmd/server/            Einstiegspunkt, Konfiguration über Umgebungsvariablen
  internal/api/          HTTP-Routen, NDJSON-Streaming, Cache, Auslieferung des Frontends
  internal/research/     Researcher-Interface, Claude-, Mistral- und Demo-Implementierung,
                         gemeinsame Prompts, Shop-Liste
  internal/pricing/      Einheiten, Packungsanzahl, Preisvergleich
  internal/model/        gemeinsame Datentypen
frontend/
  src/api.ts             API-Client inkl. NDJSON-Stream-Reader
  src/components/        Suche, Shop-Auswahl, Rezeptliste, Detail, Preisvergleich
```

## Weiteren KI-Anbieter anschließen

`backend/internal/research/research.go` definiert:

```go
type Researcher interface {
    Name() string
    SearchRecipes(ctx, model.RecipeQuery) ([]model.Recipe, error)
    FindPrices(ctx, model.Store, []model.Ingredient, postalCode string) (model.StorePrices, error)
}
```

Neue Implementierung schreiben, in `research.New` registrieren und mit `AI_PROVIDER=<name>` auswählen.
Prompts und JSON-Parser in `prompts.go` sind für alle Anbieter gleich; `mistral.go` zeigt, wie ein Anbieter
ohne Go-SDK per REST angebunden wird.

## Konfiguration

| Variable | Bedeutung | Standard |
|---|---|---|
| `AI_PROVIDER` | `claude`, `mistral` oder `demo` | erster Anbieter mit Key (Claude, dann Mistral), sonst `demo` |
| `ANTHROPIC_API_KEY` | API-Key für Claude | – |
| `CLAUDE_MODEL` | Modell-ID | `claude-opus-5-5` |
| `CLAUDE_EFFORT` | `low` … `max` – weniger Aufwand ist schneller und günstiger | `medium` |
| `MISTRAL_API_KEY` | API-Key für Mistral | – |
| `MISTRAL_MODEL` | Mistral-Modell | `mistral-medium-latest` |
| `MISTRAL_BASE_URL` | API-Endpunkt, z. B. `https://api.eu.mistral.ai` | `https://api.mistral.ai` |
| `MISTRAL_REASONING_EFFORT` | optional, nur für Reasoning-Modelle | – |
| `MISTRAL_WEB_SEARCH` | `standard` oder `premium` | `standard` |
| `PRICE_PARALLEL` | gleichzeitige Shop-Recherchen | `4` |
| `ADDR` | Listen-Adresse | `:8080` |
| `STATIC_DIR` | Pfad zum gebauten Frontend | – |

## Grenzen

- Viele Supermarkt-Webshops laden Preise per JavaScript oder zeigen sie erst nach Wahl eines Marktes.
  Die KI findet deshalb nicht immer einen Preis; jedes Angebot trägt eine Vertrauensstufe
  (`hoch` = direkt von der Shopseite, `mittel` = Prospekt/Suchvorschau, `niedrig` = Schätzung).
- EDEKA und ALDI haben keinen bundesweiten Lieferdienst-Shop; dort stammen Preise meist aus
  Online-Sortiment und Prospekten.
- Die Mistral-Websuche lässt sich (anders als bei Claude) nicht technisch auf die Shop-Domains beschränken.
  Das Modell wird per Prompt dorthin gelenkt; Produktlinks auf fremde Seiten verwirft das Backend und
  stuft die Vertrauensstufe herab.
- Eine Preisrecherche kostet pro Shop mehrere Web-Suchen. `CLAUDE_EFFORT=low` und weniger Shops senken Kosten und Dauer.
