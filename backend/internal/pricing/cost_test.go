package pricing

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestApplyCost(t *testing.T) {
	table := PriceTable{EURPerUSD: 0.5, Models: map[string]ModelPrice{
		"claude-opus-5-5": price(4, 20, 0.2, 5, 10),
	}}
	u := model.Usage{Provider: "claude", Model: "claude-opus-5-5", Requests: 1, InputTokens: 1_000_000, OutputTokens: 100_000,
		CacheReadTokens: 1_000_000, CacheWriteTokens: 200_000, WebSearches: 6}
	table.Apply(&u)
	// USD: 4 + 2 + 0.2 + 1 + 0.06 = 7.26 -> EUR 3.63
	if !u.CostKnown || !near(u.CostEUR, 3.63) {
		t.Errorf("cost = %v (known %v), want 3.63", u.CostEUR, u.CostKnown)
	}

	// Dated snapshot of a known model.
	for _, id := range []string{"claude-opus-5-5-20261101", "claude-opus-5-5@20261101"} {
		snap := model.Usage{Provider: "claude", Model: id, Requests: 1, OutputTokens: 1_000_000}
		table.Apply(&snap)
		if !snap.CostKnown || snap.CostEUR != 10 {
			t.Errorf("%s: cost = %v (known %v)", id, snap.CostEUR, snap.CostKnown)
		}
	}

	for _, id := range []string{"some-other-model", "claude-opus-5-5-1", "claude-opus-5-5-fast"} {
		unknown := model.Usage{Provider: "claude", Model: id, Requests: 1, InputTokens: 10}
		table.Apply(&unknown)
		if unknown.CostKnown || unknown.CostEUR != 0 {
			t.Errorf("%s should have an unknown cost: %+v", id, unknown)
		}
	}

	cached := model.Usage{Provider: "claude", Model: "some-other-model", Cached: true}
	table.Apply(&cached)
	if !cached.CostKnown || cached.CostEUR != 0 {
		t.Errorf("cache hit should be free: %+v", cached)
	}
	demo := model.Usage{Provider: "demo", Model: "demo"}
	table.Apply(&demo)
	if !demo.CostKnown || demo.CostEUR != 0 {
		t.Errorf("demo should be free: %+v", demo)
	}

	// No usage reported at all (timeout before the first response): unknown,
	// not a confident zero.
	none := model.Usage{Provider: "claude", Model: "claude-opus-5-5"}
	table.Apply(&none)
	if none.CostKnown {
		t.Errorf("missing usage must not be priced as zero: %+v", none)
	}
}

// TestApplyPerCall checks that tokens are priced per model call: a refusal
// fallback with another model, and Haiku's per-request long-context price.
func TestApplyPerCall(t *testing.T) {
	table := DefaultPriceTable()
	table.EURPerUSD = 1
	u := model.Usage{Provider: "claude", Model: "claude-opus-5-5", Requests: 1, InputTokens: 2_000_000, OutputTokens: 0,
		Calls: []model.ModelCall{
			{Model: "claude-opus-5-5", InputTokens: 1_000_000},
			{Model: "claude-opus-5", InputTokens: 1_000_000},
		}}
	table.Apply(&u)
	if !u.CostKnown || !near(u.CostEUR, 4+5) {
		t.Errorf("per-model cost = %v (known %v), want 9", u.CostEUR, u.CostKnown)
	}

	// Two Haiku calls: one below and one above 100,000 prompt tokens.
	h := model.Usage{Provider: "claude", Model: "claude-haiku-5-5", Requests: 2, InputTokens: 150_000, OutputTokens: 2_000_000,
		Calls: []model.ModelCall{
			{Model: "claude-haiku-5-5", InputTokens: 50_000, OutputTokens: 1_000_000},
			{Model: "claude-haiku-5-5", InputTokens: 60_000, CacheReadTokens: 50_000, OutputTokens: 1_000_000},
		}}
	h.CacheReadTokens = 50_000
	table.Apply(&h)
	want := (50_000*0.10+1_000_000*0.50)/1e6 + (60_000*0.50+50_000*0.05+1_000_000*2.50)/1e6
	if !h.CostKnown || !near(h.CostEUR, math.Round(want*1e4)/1e4) {
		t.Errorf("haiku cost = %v, want %v", h.CostEUR, want)
	}

	unknown := model.Usage{Provider: "claude", Model: "claude-opus-5-5", Requests: 1, InputTokens: 2,
		Calls: []model.ModelCall{{Model: "claude-opus-5-5", InputTokens: 1}, {Model: "claude-unknown", InputTokens: 1}}}
	table.Apply(&unknown)
	if unknown.CostKnown {
		t.Error("a call with an unknown model makes the cost unknown")
	}
}

func TestLookup(t *testing.T) {
	table := DefaultPriceTable()
	p, ok := table.Lookup("claude-opus-5-5")
	if !ok || p.Input != 4 {
		t.Errorf("claude-opus-5-5 = %+v, %v", p, ok)
	}
	p, ok = table.Lookup("claude-opus-5")
	if !ok || p.Input != 5 {
		t.Errorf("claude-opus-5 = %+v, %v", p, ok)
	}
	// A future version must not be priced with a similar model's row.
	for _, id := range []string{"claude-opus-5-6", "claude-opus-55", "claude-sonnet-5-7"} {
		if _, ok := table.Lookup(id); ok {
			t.Errorf("%s must not match", id)
		}
	}
	if _, ok := table.Lookup("mistral-medium-latest"); !ok {
		t.Error("default Mistral model should have a price")
	}
}

func TestLoadPriceTable(t *testing.T) {
	inline, err := LoadPriceTable(`{"eurPerUsd": 0.9, "models": {"My-Model": {"input": 1, "output": 2,
		"longContext": {"abovePromptTokens": 1000, "input": 3}}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if inline.EURPerUSD != 0.9 {
		t.Errorf("eurPerUsd = %v", inline.EURPerUSD)
	}
	if p, ok := inline.Lookup("my-model"); !ok || p.Output != 2 || p.LongContext == nil || p.LongContext.Input != 3 {
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

	for spec, wantErr := range map[string]string{
		`{not json`:                                        "parse",
		`{"eurPerUsd": -1}`:                                "eurPerUsd",
		`{"models": {"x": {"input": -5}}}`:                 "negative",
		`{"models": {"x": {"webSearchPer1k": 30}}}`:        "unknown field",
		`{"modells": {}}`:                                  "unknown field",
		`{"models": {"  ": {"input": 1}}}`:                 "empty model ID",
		`{"models": {"x": {"longContext": {"input": 1}}}}`: "abovePromptTokens",
		`{"models": {"x": {"input": 1}}} {"models": {}}`:   "unexpected data",
		filepath.Join(t.TempDir(), "missing.json"):         "read",
	} {
		_, err := LoadPriceTable(spec)
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("LoadPriceTable(%s) = %v, want error containing %q", spec, err, wantErr)
		}
	}
}

func TestSumUsage(t *testing.T) {
	a := model.Usage{Provider: "claude", Model: "m", Requests: 2, InputTokens: 10, WebSearches: 1, CostEUR: 0.1, CostKnown: true}
	b := model.Usage{Provider: "claude", Model: "m", Cached: true, CostKnown: true}
	sum := model.SumUsage(a, b)
	if sum.Requests != 2 || sum.InputTokens != 10 || sum.WebSearches != 1 || sum.CostEUR != 0.1 || !sum.CostKnown || sum.Cached || sum.Incomplete {
		t.Errorf("sum = %+v", sum)
	}
	if !model.SumUsage(b, b).Cached {
		t.Error("a sum of cache hits is cached")
	}
	// A part with unknown cost (e.g. a timed-out shop) keeps the known cost
	// but marks the total as incomplete.
	partial := model.SumUsage(a, model.Usage{Provider: "claude"})
	if !partial.CostKnown || partial.CostEUR != 0.1 || !partial.Incomplete {
		t.Errorf("partial sum = %+v", partial)
	}
	if model.SumUsage(a, model.Usage{Provider: "claude", CostKnown: true, CostEUR: 0.2, Incomplete: true}).Incomplete != true {
		t.Error("an incomplete part makes the total incomplete")
	}
	if u := model.SumUsage(model.Usage{Provider: "claude"}); u.CostKnown || u.Incomplete {
		t.Errorf("all unknown: %+v", u)
	}
}
