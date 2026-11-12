package pricing

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

func TestApplyCost(t *testing.T) {
	table := PriceTable{EURPerUSD: 0.5, Models: map[string]ModelPrice{
		"claude-opus-5-5": {Input: 4, Output: 20, CacheRead: 0.2, CacheWrite: 5, WebSearchPer1000: 10},
	}}
	u := model.Usage{Model: "claude-opus-5-5", InputTokens: 1_000_000, OutputTokens: 100_000,
		CacheReadTokens: 1_000_000, CacheWriteTokens: 200_000, WebSearches: 6}
	table.Apply(&u)
	// USD: 4 + 2 + 0.2 + 1 + 0.06 = 7.26 -> EUR 3.63
	if !u.CostKnown || math.Abs(u.CostEUR-3.63) > 1e-9 {
		t.Errorf("cost = %v (known %v), want 3.63", u.CostEUR, u.CostKnown)
	}

	// Dated snapshot of a known model: prefix match.
	snap := model.Usage{Model: "claude-opus-5-5-20261101", OutputTokens: 1_000_000}
	table.Apply(&snap)
	if !snap.CostKnown || snap.CostEUR != 10 {
		t.Errorf("snapshot cost = %v (known %v)", snap.CostEUR, snap.CostKnown)
	}

	unknown := model.Usage{Model: "some-other-model", InputTokens: 10}
	table.Apply(&unknown)
	if unknown.CostKnown || unknown.CostEUR != 0 {
		t.Errorf("unknown model should have an unknown cost: %+v", unknown)
	}

	cached := model.Usage{Model: "some-other-model", Cached: true}
	table.Apply(&cached)
	if !cached.CostKnown || cached.CostEUR != 0 {
		t.Errorf("cache hit should be free: %+v", cached)
	}
}

func TestLookupPrefersLongestPrefix(t *testing.T) {
	table := DefaultPriceTable()
	p, ok := table.Lookup("claude-opus-5-5")
	if !ok || p.Input != 4 {
		t.Errorf("claude-opus-5-5 = %+v, %v", p, ok)
	}
	p, ok = table.Lookup("claude-opus-5")
	if !ok || p.Input != 5 {
		t.Errorf("claude-opus-5 = %+v, %v", p, ok)
	}
	if _, ok := table.Lookup("claude-opus-55"); ok {
		t.Error("a prefix without separator must not match")
	}
	if _, ok := table.Lookup("mistral-medium-latest"); !ok {
		t.Error("default Mistral model should have a price")
	}
}

func TestLoadPriceTable(t *testing.T) {
	inline, err := LoadPriceTable(`{"eurPerUsd": 0.9, "models": {"My-Model": {"input": 1, "output": 2}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if inline.EURPerUSD != 0.9 {
		t.Errorf("eurPerUsd = %v", inline.EURPerUSD)
	}
	if p, ok := inline.Lookup("my-model"); !ok || p.Output != 2 {
		t.Errorf("custom model = %+v, %v", p, ok)
	}
	if _, ok := inline.Lookup("claude-opus-5-5"); !ok {
		t.Error("defaults should be kept when extending the table")
	}

	path := filepath.Join(t.TempDir(), "prices.json")
	if err := os.WriteFile(path, []byte(`{"models": {"claude-opus-5-5": {"input": 1, "output": 1}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := LoadPriceTable(path)
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := file.Lookup("claude-opus-5-5"); p.Input != 1 {
		t.Errorf("file entry should override the default: %+v", p)
	}
	if file.EURPerUSD != DefaultPriceTable().EURPerUSD {
		t.Errorf("default rate should be kept: %v", file.EURPerUSD)
	}

	if _, err := LoadPriceTable("{not json"); err == nil {
		t.Error("expected an error for invalid JSON")
	}
	if _, err := LoadPriceTable(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("expected an error for a missing file")
	}
}

func TestSumUsage(t *testing.T) {
	a := model.Usage{Provider: "claude", Model: "m", Requests: 2, InputTokens: 10, WebSearches: 1, CostEUR: 0.1, CostKnown: true}
	b := model.Usage{Provider: "claude", Model: "m", Cached: true, CostKnown: true}
	sum := model.SumUsage(a, b)
	if sum.Requests != 2 || sum.InputTokens != 10 || sum.WebSearches != 1 || sum.CostEUR != 0.1 || !sum.CostKnown || sum.Cached {
		t.Errorf("sum = %+v", sum)
	}
	if !model.SumUsage(b, b).Cached {
		t.Error("a sum of cache hits is cached")
	}
	if model.SumUsage(a, model.Usage{}).CostKnown {
		t.Error("an unknown part makes the total cost unknown")
	}
}
