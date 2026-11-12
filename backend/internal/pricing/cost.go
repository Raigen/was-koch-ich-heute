package pricing

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

// ModelPrice holds the list prices of one model in US dollars, as published by
// the providers: tokens per million, web searches per thousand.
type ModelPrice struct {
	Input            float64 `json:"input"`
	Output           float64 `json:"output"`
	CacheRead        float64 `json:"cacheRead"`
	CacheWrite       float64 `json:"cacheWrite"`
	WebSearchPer1000 float64 `json:"webSearchPer1000"`
	WebFetchPer1000  float64 `json:"webFetchPer1000"`
}

// PriceTable converts AI usage into an estimated cost in euros.
type PriceTable struct {
	// EURPerUSD is the exchange rate used to convert the dollar prices.
	EURPerUSD float64 `json:"eurPerUsd"`
	// Models maps a model ID (or an ID prefix, see Lookup) to its prices.
	Models map[string]ModelPrice `json:"models"`
}

// DefaultPriceTable returns the built-in prices (USD list prices, checked on
// 2026-11-12). They can be overridden or extended with PRICE_TABLE.
func DefaultPriceTable() PriceTable {
	return PriceTable{
		EURPerUSD: 0.86,
		Models: map[string]ModelPrice{
			// Anthropic: platform.claude.com/docs/en/about-claude/pricing.
			// Web search costs $10 per 1,000 searches, web fetch is free.
			// Cache writes use the 5-minute price.
			"claude-fable-5-2":  {Input: 8, Output: 40, CacheRead: 0.20, CacheWrite: 10, WebSearchPer1000: 10},
			"claude-opus-5-5":   {Input: 4, Output: 20, CacheRead: 0.20, CacheWrite: 5, WebSearchPer1000: 10},
			"claude-opus-5":     {Input: 5, Output: 25, CacheRead: 0.50, CacheWrite: 6.25, WebSearchPer1000: 10},
			"claude-sonnet-5-5": {Input: 2, Output: 10, CacheRead: 0.10, CacheWrite: 2.50, WebSearchPer1000: 10},
			"claude-sonnet-5":   {Input: 2, Output: 10, CacheRead: 0.20, CacheWrite: 2.50, WebSearchPer1000: 10},
			// Prices for prompts up to 100,000 tokens.
			"claude-haiku-5-5": {Input: 0.10, Output: 0.50, CacheRead: 0.01, CacheWrite: 0.125, WebSearchPer1000: 10},
			// Mistral: docs.mistral.ai/inference/pricing and the model pages.
			// Cached input costs 10 % of the input price. The web search price
			// is not listed in the docs; $30 per 1,000 calls is the price on
			// mistral.ai/pricing at the time of writing (unverified).
			"mistral-medium-latest": {Input: 1.5, Output: 7.5, CacheRead: 0.15, WebSearchPer1000: 30},
			"mistral-medium-3-5":    {Input: 1.5, Output: 7.5, CacheRead: 0.15, WebSearchPer1000: 30},
			"mistral-large-4":       {Input: 0.7, Output: 2.1, CacheRead: 0.07, WebSearchPer1000: 30},
			"mistral-small-2603":    {Input: 0.15, Output: 0.6, CacheRead: 0.015, WebSearchPer1000: 30},
		},
	}
}

// LoadPriceTable returns the default price table, overridden by spec. spec
// is either inline JSON (starting with "{") or the path to a JSON file in the
// same format as PriceTable. Model entries in spec replace or extend the
// defaults; eurPerUsd replaces the default rate if set.
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
	var custom PriceTable
	if err := json.Unmarshal(raw, &custom); err != nil {
		return t, fmt.Errorf("parse price table: %w", err)
	}
	if custom.EURPerUSD < 0 {
		return t, fmt.Errorf("price table: eurPerUsd must not be negative")
	}
	if custom.EURPerUSD > 0 {
		t.EURPerUSD = custom.EURPerUSD
	}
	for id, p := range custom.Models {
		t.Models[strings.ToLower(strings.TrimSpace(id))] = p
	}
	return t, nil
}

// Lookup finds the prices for a model. An exact match wins; otherwise the
// longest entry that is a prefix of the model ID followed by "-" or "@"
// (e.g. dated snapshots such as "claude-opus-5-5-20261101").
func (t PriceTable) Lookup(modelID string) (ModelPrice, bool) {
	id := strings.ToLower(strings.TrimSpace(modelID))
	if id == "" {
		return ModelPrice{}, false
	}
	if p, ok := t.Models[id]; ok {
		return p, true
	}
	best, found := "", false
	for k := range t.Models {
		if len(k) > len(best) && len(id) > len(k) && strings.HasPrefix(id, k) && (id[len(k)] == '-' || id[len(k)] == '@') {
			best, found = k, true
		}
	}
	return t.Models[best], found
}

// Apply sets the estimated cost of u. Cache hits and usage without any
// tokens or tool calls cost nothing.
func (t PriceTable) Apply(u *model.Usage) {
	if u.Cached || (u.TotalTokens() == 0 && u.WebSearches == 0 && u.WebFetches == 0) {
		u.CostEUR, u.CostKnown = 0, true
		return
	}
	p, ok := t.Lookup(u.Model)
	if !ok {
		u.CostEUR, u.CostKnown = 0, false
		return
	}
	usd := (float64(u.InputTokens)*p.Input +
		float64(u.OutputTokens)*p.Output +
		float64(u.CacheReadTokens)*p.CacheRead +
		float64(u.CacheWriteTokens)*p.CacheWrite) / 1e6
	usd += (float64(u.WebSearches)*p.WebSearchPer1000 + float64(u.WebFetches)*p.WebFetchPer1000) / 1000
	u.CostEUR = math.Round(usd*t.EURPerUSD*1e4) / 1e4
	u.CostKnown = true
}
