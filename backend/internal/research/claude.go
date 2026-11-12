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

const (
	claudeRecipeTools = "Nutze das Werkzeug web_search und öffne vielversprechende Treffer bei Bedarf mit web_fetch, um die Zutatenliste exakt zu übernehmen."
	claudePriceTools  = "Nutze web_search (die Suche ist auf die Domains des Supermarkts beschränkt) und bei Bedarf web_fetch für Produktseiten."
)

func germanLocation() anthropic.BetaUserLocationParam {
	return anthropic.BetaUserLocationParam{Country: anthropic.String("DE"), Timezone: anthropic.String("Europe/Berlin")}
}

// SearchRecipes recherchiert Rezepte über die Web-Suche.
func (c *Claude) SearchRecipes(ctx context.Context, q model.RecipeQuery) ([]model.Recipe, error) {
	tools := []anthropic.BetaToolUnionParam{
		{OfWebSearchTool20260209: &anthropic.BetaWebSearchTool20260209Param{MaxUses: anthropic.Int(8), UserLocation: germanLocation()}},
		{OfWebFetchTool20260209: &anthropic.BetaWebFetchTool20260209Param{MaxUses: anthropic.Int(6), MaxContentTokens: anthropic.Int(20000)}},
	}
	text, err := c.run(ctx, recipeSystemPrompt(claudeRecipeTools), recipeUserPrompt(q), tools)
	if err != nil {
		return nil, err
	}
	return parseRecipes(text)
}

// FindPrices recherchiert die Preise der Zutaten bei einem Händler.
func (c *Claude) FindPrices(ctx context.Context, store model.Store, ingredients []model.Ingredient, postalCode string) (model.StorePrices, error) {
	tools := []anthropic.BetaToolUnionParam{
		{OfWebSearchTool20260209: &anthropic.BetaWebSearchTool20260209Param{
			MaxUses:        anthropic.Int(int64(min(4+2*len(ingredients), 20))),
			AllowedDomains: store.Domains,
			UserLocation:   germanLocation(),
		}},
		{OfWebFetchTool20260209: &anthropic.BetaWebFetchTool20260209Param{
			MaxUses:          anthropic.Int(int64(min(2+len(ingredients), 12))),
			MaxContentTokens: anthropic.Int(15000),
			AllowedDomains:   store.Domains,
		}},
	}
	text, err := c.run(ctx, priceSystemPrompt(claudePriceTools), priceUserPrompt(store, ingredients, postalCode), tools)
	if err != nil {
		return model.StorePrices{StoreID: store.ID}, err
	}
	return parsePrices(store, text)
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

// Sicherstellen, dass Claude die Schnittstelle erfüllt.
var _ Researcher = (*Claude)(nil)
