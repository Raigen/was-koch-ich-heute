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
	// maxContinuations limits how often a response paused by the server-side
	// web search (stop_reason "pause_turn") is resumed.
	maxContinuations = 5
)

// Claude researches via the Anthropic API using the server-side tools
// web_search and web_fetch.
type Claude struct {
	client anthropic.Client
	model  string
	effort anthropic.BetaOutputConfigEffort
}

// NewClaude creates a Claude researcher. If cfg.APIKey is empty, the SDK
// reads credentials from the environment (ANTHROPIC_API_KEY etc.).
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
	claudeRecipeTools = "Use the web_search tool and, if needed, open promising results with web_fetch to copy the exact ingredient list."
	claudePriceTools  = "Use web_search (the search is restricted to the supermarket's domains) and, if needed, web_fetch for product pages."
)

func germanLocation() anthropic.BetaUserLocationParam {
	return anthropic.BetaUserLocationParam{Country: anthropic.String("DE"), Timezone: anthropic.String("Europe/Berlin")}
}

// SearchRecipes researches recipes via web search.
func (c *Claude) SearchRecipes(ctx context.Context, q model.RecipeQuery) ([]model.Recipe, model.Usage, error) {
	tools := []anthropic.BetaToolUnionParam{
		{OfWebSearchTool20260209: &anthropic.BetaWebSearchTool20260209Param{MaxUses: anthropic.Int(8), UserLocation: germanLocation()}},
		{OfWebFetchTool20260209: &anthropic.BetaWebFetchTool20260209Param{MaxUses: anthropic.Int(6), MaxContentTokens: anthropic.Int(20000)}},
	}
	text, usage, err := c.run(ctx, recipeSystemPrompt(claudeRecipeTools), recipeUserPrompt(q), tools)
	if err != nil {
		return nil, usage, err
	}
	recipes, err := parseRecipes(text)
	return recipes, usage, err
}

// FindPrices researches the ingredient prices at one retailer.
func (c *Claude) FindPrices(ctx context.Context, store model.Store, ingredients []model.Ingredient, postalCode string) (model.StorePrices, model.Usage, error) {
	tools := []anthropic.BetaToolUnionParam{
		{OfWebSearchTool20260209: &anthropic.BetaWebSearchTool20260209Param{
			MaxUses:        anthropic.Int(int64(min(3+len(ingredients), 12))),
			AllowedDomains: store.Domains,
			UserLocation:   germanLocation(),
		}},
		{OfWebFetchTool20260209: &anthropic.BetaWebFetchTool20260209Param{
			MaxUses:          anthropic.Int(int64(min(2+len(ingredients)/2, 6))),
			MaxContentTokens: anthropic.Int(15000),
			AllowedDomains:   store.Domains,
		}},
	}
	text, usage, err := c.run(ctx, priceSystemPrompt(claudePriceTools), priceUserPrompt(store, ingredients, postalCode), tools)
	if err != nil {
		return model.StorePrices{StoreID: store.ID}, usage, err
	}
	sp, err := parsePrices(store, text)
	return sp, usage, err
}

// addClaudeUsage adds the usage of one API response to u. Every response,
// including paused ones, is billed separately, so the counts are summed.
func addClaudeUsage(u *model.Usage, resp *anthropic.BetaMessage) {
	if resp.Model != "" {
		u.Model = string(resp.Model)
	}
	u.Add(model.Usage{
		Requests:         1,
		InputTokens:      resp.Usage.InputTokens,
		OutputTokens:     resp.Usage.OutputTokens,
		CacheReadTokens:  resp.Usage.CacheReadInputTokens,
		CacheWriteTokens: resp.Usage.CacheCreationInputTokens,
		WebSearches:      int(resp.Usage.ServerToolUse.WebSearchRequests),
		WebFetches:       int(resp.Usage.ServerToolUse.WebFetchRequests),
	})
}

// run sends a request with server-side tools, resumes paused responses and
// returns the full text of the final response together with the usage summed
// over all responses.
//
// Returned errors are shown in the German UI, hence German messages.
func (c *Claude) run(ctx context.Context, system, prompt string, tools []anthropic.BetaToolUnionParam) (string, model.Usage, error) {
	usage := model.Usage{Provider: "claude", Model: c.model}
	params := anthropic.BetaMessageNewParams{
		Model:     c.model,
		MaxTokens: 16000,
		System:    []anthropic.BetaTextBlockParam{{Text: system}},
		Tools:     tools,
		Messages: []anthropic.BetaMessageParam{
			anthropic.NewBetaUserMessage(anthropic.NewBetaTextBlock(prompt)),
		},
		OutputConfig: anthropic.BetaOutputConfigParam{Effort: c.effort},
		// If a safety classifier declines the request, a suitable fallback
		// model answers it within the same call.
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
					return "", usage, fmt.Errorf("Claude API: Zugang verweigert – ANTHROPIC_API_KEY prüfen (%d)", apiErr.StatusCode)
				case 429:
					return "", usage, fmt.Errorf("Claude API: Rate-Limit erreicht, bitte später erneut versuchen")
				}
			}
			return "", usage, fmt.Errorf("Claude API: %w", err)
		}
		addClaudeUsage(&usage, resp)

		switch resp.StopReason {
		case anthropic.BetaStopReasonPauseTurn:
			if i >= maxContinuations {
				return "", usage, errors.New("Recherche hat zu lange gedauert (zu viele Fortsetzungen)")
			}
			// Resume the server-side tool loop: append the response and resend.
			params.Messages = append(params.Messages, resp.ToParam())
			continue
		case anthropic.BetaStopReasonRefusal:
			return "", usage, errors.New("die Anfrage wurde vom Modell abgelehnt")
		case anthropic.BetaStopReasonMaxTokens:
			// Still try to parse the (possibly complete) JSON.
		}

		var sb strings.Builder
		for _, block := range resp.Content {
			if t, ok := block.AsAny().(anthropic.BetaTextBlock); ok {
				sb.WriteString(t.Text)
			}
		}
		return sb.String(), usage, nil
	}
}

// Ensure Claude implements the interface.
var _ Researcher = (*Claude)(nil)
