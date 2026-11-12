package research

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

const (
	defaultClaudeModel = "claude-opus-5-5"
	// maxContinuations begrenzt, wie oft eine durch die serverseitige
	// Web-Suche pausierte Antwort (stop_reason "pause_turn") fortgesetzt wird.
	maxContinuations = 5
)

// Claude recherchiert mit der Anthropic API und den serverseitigen Werkzeugen
// web_search und web_fetch.
type Claude struct {
	client anthropic.Client
	model  string
	effort anthropic.BetaOutputConfigEffort
}

// NewClaude erzeugt einen Claude-Researcher. Ist cfg.APIKey leer, liest das
// SDK die Zugangsdaten aus der Umgebung (ANTHROPIC_API_KEY o. ä.).
func NewClaude(cfg Config) *Claude {
	opts := []option.RequestOption{option.WithRequestTimeout(6 * time.Minute)}
	if cfg.APIKey != "" {
		opts = append(opts, option.WithAPIKey(cfg.APIKey))
	}
	m := cfg.Model
	if m == "" {
		m = defaultClaudeModel
	}
	effort := anthropic.BetaOutputConfigEffort(strings.ToLower(cfg.Effort))
	if effort == "" {
		effort = anthropic.BetaOutputConfigEffortMedium
	}
	return &Claude{client: anthropic.NewClient(opts...), model: m, effort: effort}
}

func (c *Claude) Name() string { return "Claude (" + c.model + ")" }

const recipeSystemPrompt = `Du bist ein Rechercheassistent für eine Rezept-App in Deutschland.
Suche mit dem Werkzeug web_search nach echten, gut bewerteten Rezepten auf deutschsprachigen Seiten
(z. B. chefkoch.de, lecker.de, eatsmarter.de, kitchenstories.com, essen-und-trinken.de) und öffne
vielversprechende Treffer bei Bedarf mit web_fetch, um die Zutatenliste exakt zu übernehmen.

Achte auf preiswerte Rezepte mit Zutaten, die es in normalen Supermärkten gibt.
Rechne die Zutatenmengen auf die gewünschte Portionenzahl um.
Verwende für Mengen möglichst die Einheiten g, kg, ml, l, Stück, EL, TL, Prise, Bund, Dose, Packung.
Markiere Grundzutaten, die fast jeder zuhause hat (Salz, Pfeffer, Zucker, Öl, Mehl in kleinen Mengen, Wasser), mit "pantry": true.
Erfinde keine Quellen: sourceUrl muss eine Seite sein, die du in der Recherche gefunden hast.

Antworte am Ende ausschließlich mit einem JSON-Codeblock (markiert mit drei Backticks und "json") in genau diesem Format:
{"recipes":[{"title":"","description":"","sourceUrl":"","sourceName":"","servings":4,"totalTimeMinutes":30,
"difficulty":"einfach|mittel|anspruchsvoll","estimatedCostEur":0.0,
"ingredients":[{"name":"","quantity":0,"unit":"","pantry":false}],"steps":[""]}]}`

// SearchRecipes recherchiert Rezepte über die Web-Suche.
func (c *Claude) SearchRecipes(ctx context.Context, q model.RecipeQuery) ([]model.Recipe, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Suche %d Rezepte für: %s\n", q.MaxRecipes, q.Query)
	fmt.Fprintf(&b, "Portionen: %d\n", q.Servings)
	if q.MaxBudget > 0 {
		fmt.Fprintf(&b, "Budget: höchstens ca. %.2f € für alle Portionen zusammen\n", q.MaxBudget)
	}
	if q.Diet != "" {
		fmt.Fprintf(&b, "Ernährungsform: %s\n", q.Diet)
	}
	if q.ExcludeText != "" {
		fmt.Fprintf(&b, "Nicht verwenden: %s\n", q.ExcludeText)
	}

	tools := []anthropic.BetaToolUnionParam{
		{OfWebSearchTool20260209: &anthropic.BetaWebSearchTool20260209Param{
			MaxUses:      anthropic.Int(8),
			UserLocation: anthropic.BetaUserLocationParam{Country: anthropic.String("DE"), Timezone: anthropic.String("Europe/Berlin")},
		}},
		{OfWebFetchTool20260209: &anthropic.BetaWebFetchTool20260209Param{
			MaxUses:          anthropic.Int(6),
			MaxContentTokens: anthropic.Int(20000),
		}},
	}

	text, err := c.run(ctx, recipeSystemPrompt, b.String(), tools)
	if err != nil {
		return nil, err
	}
	var out struct {
		Recipes []model.Recipe `json:"recipes"`
	}
	if err := extractJSON(text, &out); err != nil {
		return nil, fmt.Errorf("Rezeptantwort konnte nicht gelesen werden: %w", err)
	}
	return out.Recipes, nil
}

const priceSystemPrompt = `Du bist ein Preisrecherche-Assistent für Lebensmittel in Deutschland.
Du bekommst einen Supermarkt und eine Zutatenliste. Finde für jede Zutat das günstigste passende Produkt
im Online-Shop bzw. im Online-Angebot dieses Supermarkts. Nutze web_search (die Suche ist auf die Domains
des Supermarkts beschränkt) und bei Bedarf web_fetch für Produktseiten.

Regeln:
- Bevorzuge Eigenmarken und Grundsortiment (z. B. "ja!", "GUT&GÜNSTIG", "Milsani"), sofern sie passen.
- Aktuelle Angebote sind erlaubt, markiere sie mit "isPromotion": true.
- Gib den Preis in Euro für EINE Packung an und die Packungsgröße als Zahl plus Einheit (g, kg, ml, l, Stück).
- "packagesNeeded" ist die Anzahl Packungen, die für die angegebene Rezeptmenge nötig ist (mindestens 1).
- "confidence": "hoch", wenn der Preis direkt auf der Shopseite stand; "mittel", wenn er aus einem Prospekt
  oder einer Suchvorschau stammt; "niedrig", wenn es eine Schätzung ist.
- Erfinde keine Preise ohne Kennzeichnung. Wenn du eine Zutat nicht findest, setze sie in "notFound".
- "url" muss eine Seite sein, die du tatsächlich gefunden hast, sonst leer lassen.

Antworte am Ende ausschließlich mit einem JSON-Codeblock (markiert mit drei Backticks und "json") in genau diesem Format:
{"offers":[{"ingredient":"","productName":"","price":0.0,"packageAmount":0,"packageUnit":"",
"packagesNeeded":1,"unitPrice":"","url":"","isPromotion":false,"confidence":"hoch|mittel|niedrig","note":""}],
"notFound":[""]}`

// FindPrices recherchiert die Preise der Zutaten bei einem Händler.
func (c *Claude) FindPrices(ctx context.Context, store model.Store, ingredients []model.Ingredient, postalCode string) (model.StorePrices, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "Supermarkt: %s (Websites: %s)\n", store.Name, strings.Join(store.Domains, ", "))
	if postalCode != "" {
		fmt.Fprintf(&b, "Postleitzahl für regionale Preise und Angebote: %s\n", postalCode)
	}
	b.WriteString("Zutaten (Rezeptmengen):\n")
	for _, ing := range ingredients {
		fmt.Fprintf(&b, "- %s: %s %s\n", ing.Name, trimFloat(ing.Quantity), ing.Unit)
	}

	tools := []anthropic.BetaToolUnionParam{
		{OfWebSearchTool20260209: &anthropic.BetaWebSearchTool20260209Param{
			MaxUses:        anthropic.Int(int64(min(4+2*len(ingredients), 20))),
			AllowedDomains: store.Domains,
			UserLocation:   anthropic.BetaUserLocationParam{Country: anthropic.String("DE"), Timezone: anthropic.String("Europe/Berlin")},
		}},
		{OfWebFetchTool20260209: &anthropic.BetaWebFetchTool20260209Param{
			MaxUses:          anthropic.Int(int64(min(2+len(ingredients), 12))),
			MaxContentTokens: anthropic.Int(15000),
			AllowedDomains:   store.Domains,
		}},
	}

	text, err := c.run(ctx, priceSystemPrompt, b.String(), tools)
	if err != nil {
		return model.StorePrices{StoreID: store.ID}, err
	}
	var out struct {
		Offers   []model.Offer `json:"offers"`
		NotFound []string      `json:"notFound"`
	}
	if err := extractJSON(text, &out); err != nil {
		return model.StorePrices{StoreID: store.ID}, fmt.Errorf("Preisantwort konnte nicht gelesen werden: %w", err)
	}
	for i := range out.Offers {
		out.Offers[i].StoreID = store.ID
	}
	return model.StorePrices{StoreID: store.ID, Offers: out.Offers, NotFound: compact(out.NotFound)}, nil
}

// run führt eine Anfrage mit serverseitigen Werkzeugen aus, setzt pausierte
// Antworten fort und liefert den gesamten Text der letzten Antwort.
func (c *Claude) run(ctx context.Context, system, prompt string, tools []anthropic.BetaToolUnionParam) (string, error) {
	params := anthropic.BetaMessageNewParams{
		Model:     c.model,
		MaxTokens: 16000,
		System:    []anthropic.BetaTextBlockParam{{Text: system}},
		Tools:     tools,
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(prompt)),
		},
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: c.effort},
		// Lehnt ein Sicherheitsfilter die Anfrage ab, beantwortet automatisch
		// ein passendes Ausweichmodell sie im selben Aufruf.
		Fallbacks: anthropic.BetaFallbacksParamOfDefault(),
		Betas:     []anthropic.AnthropicBeta{anthropic.AnthropicBetaServerSideFallback2026_07_01},
	}

	for i := 0; ; i++ {
		resp, err := c.client.Beta.Messages.New(ctx, params)
		if err != nil {
			var apiErr *anthropic.Error
			if errors.As(err, &apiErr) {
				switch apiErr.StatusCode {
				case 401, 403:
					return "", fmt.Errorf("Claude API: Zugang verweigert – ANTHROPIC_API_KEY prüfen (%d)", apiErr.StatusCode)
				case 429:
					return "", fmt.Errorf("Claude API: Rate-Limit erreicht, bitte später erneut versuchen")
				}
			}
			return "", fmt.Errorf("Claude API: %w", err)
		}

		switch resp.StopReason {
		case anthropic.BetaStopReasonPauseTurn:
			if i >= maxContinuations {
				return "", errors.New("Recherche hat zu lange gedauert (zu viele Fortsetzungen)")
			}
			// Server-seitige Werkzeugschleife fortsetzen: Antwort anhängen und erneut senden.
			params.Messages = append(params.Messages, resp.ToParam())
			continue
		case anthropic.BetaStopReasonRefusal:
			return "", errors.New("die Anfrage wurde vom Modell abgelehnt")
		case anthropic.BetaStopReasonMaxTokens:
			// Trotzdem versuchen, das (evtl. vollständige) JSON zu lesen.
		}

		var sb strings.Builder
		for _, block := range resp.Content {
			if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
				sb.WriteString(t.Text)
			}
		}
		return sb.String(), nil
	}
}

func trimFloat(f float64) string {
	if f == 0 {
		return ""
	}
	s := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", f), "0"), ".")
	return strings.Replace(s, ".", ",", 1)
}

func compact(in []string) []string {
	out := in[:0]
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// Sicherstellen, dass Claude die Schnittstelle erfüllt.
var _ Researcher = (*Claude)(nil)
