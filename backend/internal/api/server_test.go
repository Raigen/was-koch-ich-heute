package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
	"github.com/raigen/was-koch-ich-heute/backend/internal/research"
)

func newTestServer() *httptest.Server {
	d := research.NewDemo()
	d.Delay = 0
	return httptest.NewServer(New(d, Options{}).Handler())
}

func TestRecipeSearch(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/recipes/search", "application/json", strings.NewReader(`{"query":"vegan curry","servings":2}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Recipes []model.Recipe
		Cached  bool
		Usage   *model.Usage
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Recipes) == 0 || out.Recipes[0].ID != "demo-dal" || out.Recipes[0].Servings != 2 {
		t.Fatalf("unexpected recipes: %+v", out.Recipes)
	}
	if out.Usage == nil || out.Usage.Provider != "demo" || out.Usage.CostEUR != 0 || !out.Usage.CostKnown || out.Usage.Cached {
		t.Errorf("unexpected usage: %+v", out.Usage)
	}

	// The same search again comes from the cache and is marked as such.
	resp3, err := http.Post(ts.URL+"/api/recipes/search", "application/json", strings.NewReader(`{"query":"vegan curry","servings":2}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	out.Usage = nil
	if err := json.NewDecoder(resp3.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if !out.Cached || out.Usage == nil || !out.Usage.Cached || out.Usage.Provider != "demo" {
		t.Errorf("cache hit: cached=%v usage=%+v", out.Cached, out.Usage)
	}

	resp2, _ := http.Post(ts.URL+"/api/recipes/search", "application/json", strings.NewReader(`{"query":"  "}`))
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("empty query: status %d, want 400", resp2.StatusCode)
	}
}

func TestPriceCompareStream(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	body := `{"ingredients":[{"name":"Spaghetti","quantity":500,"unit":"g"},{"name":"Zwiebel","quantity":2,"unit":"Stück"}],"storeIds":["rewe","aldi-sued","does-not-exist"],"postalCode":"10115"}`
	resp, err := http.Post(ts.URL+"/api/prices/compare", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var stores, summaries int
	var last priceEvent
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		var ev priceEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("invalid NDJSON line %q: %v", sc.Text(), err)
		}
		switch ev.Type {
		case "store":
			stores++
			if ev.Store.Usage == nil || ev.Store.Usage.Provider != "demo" {
				t.Errorf("store event without usage: %+v", ev.Store)
			}
		case "summary":
			summaries++
		}
		last = ev
	}
	if stores != 2 || summaries != 1 || last.Type != "summary" {
		t.Fatalf("stores=%d summaries=%d last=%s", stores, summaries, last.Type)
	}
	if len(last.Comparison.StoreTotals) != 2 || last.Comparison.MixTotal <= 0 {
		t.Errorf("unexpected comparison: %+v", last.Comparison)
	}
	if u := last.Comparison.Usage; u == nil || u.CostEUR != 0 || !u.CostKnown {
		t.Errorf("summary without total usage: %+v", u)
	}
}

// slowResearcher hangs for one shop until the context expires and counts calls.
type slowResearcher struct {
	research.Demo
	slowStore string
	calls     map[string]int
	mu        sync.Mutex
}

func (s *slowResearcher) FindPrices(ctx context.Context, st model.Store, ings []model.Ingredient, plz string) (model.StorePrices, model.Usage, error) {
	s.mu.Lock()
	s.calls[st.ID]++
	s.mu.Unlock()
	if st.ID == s.slowStore {
		<-ctx.Done()
		return model.StorePrices{}, model.Usage{}, ctx.Err()
	}
	return s.Demo.FindPrices(ctx, st, ings, plz)
}

func TestPriceCompareTimeoutHeartbeatAndRetry(t *testing.T) {
	r := &slowResearcher{slowStore: "edeka", calls: map[string]int{}}
	ts := httptest.NewServer(New(r, Options{StoreTimeout: 1500 * time.Millisecond, Heartbeat: 50 * time.Millisecond}).Handler())
	defer ts.Close()

	run := func() (map[string]model.StorePrices, int) {
		body := `{"ingredients":[{"name":"Reis","quantity":250,"unit":"g"}],"storeIds":["rewe","edeka"]}`
		resp, err := http.Post(ts.URL+"/api/prices/compare", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		stores, pings := map[string]model.StorePrices{}, 0
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			var ev priceEvent
			if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
				t.Fatal(err)
			}
			switch ev.Type {
			case "store":
				stores[ev.Store.StoreID] = *ev.Store
			case "ping":
				pings++
			}
		}
		return stores, pings
	}

	stores, pings := run()
	if !stores["edeka"].TimedOut || !strings.Contains(stores["edeka"].Error, "Zeitüberschreitung") {
		t.Errorf("edeka should report a timeout: %+v", stores["edeka"])
	}
	// No usage reported before the timeout: the cost is unknown, not 0 €.
	if u := stores["edeka"].Usage; u == nil || u.CostKnown {
		t.Errorf("timed-out shop must have an unknown cost: %+v", u)
	}
	if stores["rewe"].Error != "" || len(stores["rewe"].Offers) == 0 {
		t.Errorf("rewe should still return results: %+v", stores["rewe"])
	}
	if pings < 2 {
		t.Errorf("expected keep-alive pings in the stream, got %d", pings)
	}

	// Retry: rewe comes from the cache, only edeka is researched again.
	stores, _ = run()
	if !stores["rewe"].Cached || r.calls["rewe"] != 1 || r.calls["edeka"] != 2 {
		t.Errorf("retry: rewe cached=%v, calls=%v", stores["rewe"].Cached, r.calls)
	}
	if u := stores["rewe"].Usage; u == nil || !u.Cached || u.CostEUR != 0 {
		t.Errorf("retry: cached shop should report free usage: %+v", u)
	}
}

// billedResearcher fails recipe searches after a billed API call and lets
// edeka time out after a billed API call.
type billedResearcher struct{ research.Demo }

func (b *billedResearcher) SearchRecipes(ctx context.Context, q model.RecipeQuery) ([]model.Recipe, model.Usage, error) {
	u := model.Usage{Provider: "claude", Model: "claude-opus-5-5", Requests: 1, InputTokens: 1000, OutputTokens: 100}
	return nil, u, errors.New("die Anfrage wurde vom Modell abgelehnt")
}

func (b *billedResearcher) FindPrices(ctx context.Context, st model.Store, ings []model.Ingredient, plz string) (model.StorePrices, model.Usage, error) {
	u := model.Usage{Provider: "claude", Model: "claude-opus-5-5", Requests: 1, InputTokens: 1000, WebSearches: 2}
	if st.ID == "edeka" {
		<-ctx.Done()
		return model.StorePrices{}, u, ctx.Err()
	}
	return model.StorePrices{StoreID: st.ID}, u, nil
}

func TestUsageOnFailure(t *testing.T) {
	ts := httptest.NewServer(New(&billedResearcher{}, Options{StoreTimeout: 200 * time.Millisecond}).Handler())
	defer ts.Close()

	// A failed recipe search still returns its (billed) usage.
	resp, err := http.Post(ts.URL+"/api/recipes/search", "application/json", strings.NewReader(`{"query":"dal"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var failed struct {
		Error string
		Usage *model.Usage
	}
	if err := json.NewDecoder(resp.Body).Decode(&failed); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadGateway || failed.Error == "" || failed.Usage == nil || !failed.Usage.CostKnown || failed.Usage.CostEUR <= 0 {
		t.Errorf("failed search: status %d, %+v", resp.StatusCode, failed)
	}

	// A shop cut off by the timeout after a billed call has an incomplete
	// cost (a lower bound), and so has the total.
	body := `{"ingredients":[{"name":"Reis","quantity":250,"unit":"g"}],"storeIds":["rewe","edeka"]}`
	resp2, err := http.Post(ts.URL+"/api/prices/compare", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	stores := map[string]*model.Usage{}
	var total *model.Usage
	sc := bufio.NewScanner(resp2.Body)
	for sc.Scan() {
		var ev priceEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatal(err)
		}
		switch ev.Type {
		case "store":
			stores[ev.Store.StoreID] = ev.Store.Usage
		case "summary":
			total = ev.Comparison.Usage
		}
	}
	if u := stores["rewe"]; u == nil || u.Incomplete || !u.CostKnown {
		t.Errorf("rewe usage = %+v", u)
	}
	if u := stores["edeka"]; u == nil || !u.Incomplete || !u.CostKnown || u.CostEUR <= 0 {
		t.Errorf("timed-out edeka usage should be an incomplete lower bound: %+v", u)
	}
	if total == nil || !total.Incomplete || stores["rewe"] == nil || total.CostEUR <= stores["rewe"].CostEUR {
		t.Errorf("total = %+v", total)
	}
}
