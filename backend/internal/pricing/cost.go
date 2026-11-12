package pricing

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

// TokenPrices are token prices in US dollars per million tokens.
type TokenPrices struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
}

// LongContextPrice holds the higher token prices that apply to a request
// whose prompt (all input tokens including cache reads and writes) exceeds
// AbovePromptTokens.
type LongContextPrice struct {
	AbovePromptTokens int64 `json:"abovePromptTokens"`
	TokenPrices
}

// ModelPrice holds the list prices of one model in US dollars, as published by
// the providers: tokens per million, tool calls per thousand.
type ModelPrice struct {
	TokenPrices
	WebSearchPer1000 float64           `json:"webSearchPer1000"`
	WebFetchPer1000  float64           `json:"webFetchPer1000"`
	LongContext      *LongContextPrice `json:"longContext,omitempty"`
}

// tokens returns the token prices for a request with the given prompt length.
func (p ModelPrice) tokens(promptTokens int64) TokenPrices {
	if p.LongContext != nil && promptTokens > p.LongContext.AbovePromptTokens {
		return p.LongContext.TokenPrices
	}
	return p.TokenPrices
}

// PriceTable converts AI usage into an estimated cost in euros.
type PriceTable struct {
	// EURPerUSD is the exchange rate used to convert the dollar prices.
	EURPerUSD float64 `json:"eurPerUsd"`
	// Models maps a model ID to its prices (see Lookup).
	Models map[string]ModelPrice `json:"models"`
}

func price(in, out, cacheRead, cacheWrite, webSearchPer1000 float64) ModelPrice {
	return ModelPrice{TokenPrices: TokenPrices{in, out, cacheRead, cacheWrite}, WebSearchPer1000: webSearchPer1000}
}

// DefaultPriceTable returns the built-in prices (USD list prices, checked on
// 2026-11-12). They can be overridden or extended with PRICE_TABLE.
func DefaultPriceTable() PriceTable {
	haiku := price(0.10, 0.50, 0.01, 0.125, 10)
	haiku.LongContext = &LongContextPrice{AbovePromptTokens: 100_000, TokenPrices: TokenPrices{0.50, 2.50, 0.05, 0.625}}
	return PriceTable{
		// Static approximation, not a live rate.
		EURPerUSD: 0.86,
		Models: map[string]ModelPrice{
			// Anthropic: platform.claude.com/docs/en/about-claude/pricing.
			// Web search costs $10 per 1,000 searches, web fetch is free.
			// Cache writes use the 5-minute price.
			"claude-fable-5-2":  price(8, 40, 0.20, 10, 10),
			"claude-opus-5-5":   price(4, 20, 0.20, 5, 10),
			"claude-opus-5":     price(5, 25, 0.50, 6.25, 10),
			"claude-sonnet-5-5": price(2, 10, 0.10, 2.50, 10),
			"claude-sonnet-5":   price(2, 10, 0.20, 2.50, 10),
			// Haiku 5.5 is priced per request by prompt length.
			"claude-haiku-5-5": haiku,
			// Mistral: docs.mistral.ai/inference/pricing and the model pages.
			// Cached input costs 10 % of the input price. The web search price
			// is not in the docs; $30 per 1,000 calls is unverified.
			"mistral-medium-latest": price(1.5, 7.5, 0.15, 0, 30),
			"mistral-medium-3-5":    price(1.5, 7.5, 0.15, 0, 30),
			"mistral-large-4":       price(0.7, 2.1, 0.07, 0, 30),
			"mistral-small-2603":    price(0.15, 0.6, 0.015, 0, 30),
		},
	}
}

// LoadPriceTable returns the default price table, overridden by spec. spec
// is either inline JSON (starting with "{") or the path to a JSON file in the
// same format as PriceTable. Model entries in spec replace or extend the
// defaults; eurPerUsd replaces the default rate if set. Unknown keys and
// negative prices are rejected, so that a typo does not silently price a
// component at zero.
func LoadPriceTable(spec string) (PriceTable, error) {
	t := DefaultPriceTable()
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return t, nil
	}
	raw := []byte(spec)
	if !strings.HasPrefix(spec, "{") {
		var err error
		if raw, err = os.ReadFile(spec); err != nil {
			return t, fmt.Errorf("read price table: %w", err)
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var custom PriceTable
	if err := dec.Decode(&custom); err != nil {
		return t, fmt.Errorf("parse price table: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return t, errors.New("parse price table: unexpected data after the JSON object")
	}
	if custom.EURPerUSD < 0 || math.IsNaN(custom.EURPerUSD) {
		return t, errors.New("price table: eurPerUsd must not be negative")
	}
	if custom.EURPerUSD > 0 {
		t.EURPerUSD = custom.EURPerUSD
	}
	for id, p := range custom.Models {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" {
			return t, errors.New("price table: empty model ID")
		}
		if err := p.validate(); err != nil {
			return t, fmt.Errorf("price table: model %q: %w", id, err)
		}
		t.Models[id] = p
	}
	return t, nil
}

func (p ModelPrice) validate() error {
	values := []float64{p.Input, p.Output, p.CacheRead, p.CacheWrite, p.WebSearchPer1000, p.WebFetchPer1000}
	if lc := p.LongContext; lc != nil {
		if lc.AbovePromptTokens <= 0 {
			return errors.New("longContext.abovePromptTokens must be positive")
		}
		values = append(values, lc.Input, lc.Output, lc.CacheRead, lc.CacheWrite)
	}
	for _, v := range values {
		if v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("prices must not be negative")
		}
	}
	return nil
}

// Lookup finds the prices for a model: an exact match, or a dated snapshot of
// a listed model ("claude-opus-5-5-20261101", "claude-opus-5-5@20261101").
// Other IDs, e.g. a newer model version that merely shares a prefix with a
// listed one, are not found, so their cost is shown as unknown instead of
// being priced with another model's row.
func (t PriceTable) Lookup(modelID string) (ModelPrice, bool) {
	id := strings.ToLower(strings.TrimSpace(modelID))
	if id == "" {
		return ModelPrice{}, false
	}
	if p, ok := t.Models[id]; ok {
		return p, true
	}
	if i := strings.IndexByte(id, '@'); i > 0 {
		p, ok := t.Models[id[:i]]
		return p, ok
	}
	if len(id) > 9 && id[len(id)-9] == '-' && isDigits(id[len(id)-8:]) {
		p, ok := t.Models[id[:len(id)-9]]
		return p, ok
	}
	return ModelPrice{}, false
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// Apply sets the estimated cost of u.
//   - Cache hits and the demo provider cost nothing.
//   - Without any reported usage (e.g. a timeout before the first response),
//     the cost is unknown: money may have been spent that nobody reported.
//   - Tokens are priced per model call (u.Calls) with the call's own model and
//     prompt length; without calls, the summed tokens are priced with u.Model.
//   - Web searches and fetches are priced with u.Model.
//
// If any model has no price, the cost is unknown.
func (t PriceTable) Apply(u *model.Usage) {
	u.CostEUR = 0
	if u.Cached || u.Provider == "demo" {
		u.CostKnown = true
		return
	}
	if u.Requests == 0 && u.TotalTokens() == 0 && u.WebSearches == 0 && u.WebFetches == 0 {
		u.CostKnown = false
		return
	}
	main, ok := t.Lookup(u.Model)
	if !ok && (u.WebSearches > 0 || u.WebFetches > 0 || len(u.Calls) == 0) {
		u.CostKnown = false
		return
	}
	calls := u.Calls
	if len(calls) == 0 {
		calls = []model.ModelCall{{Model: u.Model, InputTokens: u.InputTokens, OutputTokens: u.OutputTokens,
			CacheReadTokens: u.CacheReadTokens, CacheWriteTokens: u.CacheWriteTokens}}
	}
	var usd float64
	for _, c := range calls {
		p, ok := t.Lookup(c.Model)
		if !ok {
			u.CostKnown = false
			return
		}
		tp := p.tokens(c.PromptTokens())
		usd += (float64(c.InputTokens)*tp.Input + float64(c.OutputTokens)*tp.Output +
			float64(c.CacheReadTokens)*tp.CacheRead + float64(c.CacheWriteTokens)*tp.CacheWrite) / 1e6
	}
	usd += (float64(u.WebSearches)*main.WebSearchPer1000 + float64(u.WebFetches)*main.WebFetchPer1000) / 1000
	u.CostEUR = math.Round(usd*t.EURPerUSD*1e4) / 1e4
	u.CostKnown = true
}
