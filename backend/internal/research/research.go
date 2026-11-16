// Package research wraps AI-powered web research behind a pluggable interface.
package research

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

// Researcher is the interface every AI provider implements. A new provider
// (another LLM, a local model, …) only has to provide these methods and be
// registered in New.
//
// Both research methods also return the AI usage they caused (tokens, web
// searches, model), summed over all API calls. Usage is returned even
// together with an error, because a failed research can still cost money.
// The cost is computed later from the price table (see pricing.PriceTable).
type Researcher interface {
	// Name returns a short display name of the provider.
	Name() string
	// SearchRecipes researches matching recipes on the web.
	SearchRecipes(ctx context.Context, q model.RecipeQuery) ([]model.Recipe, model.Usage, error)
	// FindPrices looks up the ingredients in a shop's online assortment.
	FindPrices(ctx context.Context, store model.Store, ingredients []model.Ingredient, postalCode string) (model.StorePrices, model.Usage, error)
}

// notBilledError marks an error where the provider certainly rejected the
// request before doing (and billing) any work, e.g. an invalid API key or a
// rate limit (HTTP 4xx), or where no request was sent at all.
type notBilledError struct{ err error }

func (e notBilledError) Error() string { return e.err.Error() }
func (e notBilledError) Unwrap() error { return e.err }

// notBilled wraps err as an error that caused no AI cost.
func notBilled(err error) error { return notBilledError{err} }

// IsNotBilled reports whether err certainly caused no AI cost. For other
// errors without reported usage (timeouts, network errors), the cost is
// unknown.
func IsNotBilled(err error) bool {
	var nb notBilledError
	return errors.As(err, &nb)
}

// rejectedBeforeBilling reports whether an HTTP status means that the API
// rejected the request without processing it. 408 (timeout) and 5xx errors
// are excluded: work may have been done.
func rejectedBeforeBilling(status int) bool {
	return status >= 400 && status < 500 && status != 408
}

// Config selects and configures the provider.
type Config struct {
	Provider string // "claude", "mistral", "demo" or empty (automatic)

	// Claude (Anthropic)
	APIKey string
	Model  string
	Effort string

	// Mistral
	MistralAPIKey          string
	MistralModel           string
	MistralBaseURL         string // e.g. https://api.eu.mistral.ai
	MistralReasoningEffort string
	MistralWebSearch       string // "standard" or "premium"
}

// New creates the configured Researcher. Without an explicit provider, the
// first provider with an API key is used (Claude, then Mistral), otherwise
// demo mode.
func New(cfg Config) (Researcher, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		switch {
		case cfg.APIKey != "":
			provider = "claude"
		case cfg.MistralAPIKey != "":
			provider = "mistral"
		default:
			provider = "demo"
		}
	}
	switch provider {
	case "claude", "anthropic":
		return NewClaude(cfg), nil
	case "mistral":
		if cfg.MistralAPIKey == "" {
			return nil, fmt.Errorf("AI_PROVIDER=mistral requires MISTRAL_API_KEY")
		}
		return NewMistral(cfg), nil
	case "demo", "mock":
		return NewDemo(), nil
	default:
		return nil, fmt.Errorf("unknown AI provider %q (allowed: claude, mistral, demo)", cfg.Provider)
	}
}

// Stores is the list of supported retailers.
var Stores = []model.Store{
	{ID: "rewe", Name: "REWE", Domains: []string{"rewe.de", "shop.rewe.de"}, Color: "#cc071e"},
	{ID: "edeka", Name: "EDEKA", Domains: []string{"edeka.de", "edeka24.de"}, Color: "#1a4b9b"},
	{ID: "aldi-sued", Name: "ALDI SÜD", Domains: []string{"aldi-sued.de"}, Color: "#00005f"},
	{ID: "aldi-nord", Name: "ALDI Nord", Domains: []string{"aldi-nord.de"}, Color: "#00b4dc"},
	{ID: "lidl", Name: "Lidl", Domains: []string{"lidl.de"}, Color: "#0050aa"},
	{ID: "penny", Name: "PENNY", Domains: []string{"penny.de"}, Color: "#cd1414"},
	{ID: "kaufland", Name: "Kaufland", Domains: []string{"kaufland.de"}, Color: "#e10915"},
}

// StoreByID looks up a retailer by its ID.
func StoreByID(id string) (model.Store, bool) {
	for _, s := range Stores {
		if s.ID == id {
			return s, true
		}
	}
	return model.Store{}, false
}
