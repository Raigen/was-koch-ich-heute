// Package api provides the HTTP interface for the React frontend.
//
// Error messages returned to the client are shown in the German UI and are
// therefore German; logs are English.
package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
	"github.com/raigen/was-koch-ich-heute/backend/internal/pricing"
	"github.com/raigen/was-koch-ich-heute/backend/internal/research"
)

// Server bundles researcher, cache and routes.
type Server struct {
	researcher  research.Researcher
	cache       *ttlCache
	parallel    int
	staticDir   string
	recipeTTL   time.Duration
	priceTTL    time.Duration
	callTimeout time.Duration
	// storeTimeout limits the research of a single shop so that a hanging shop
	// does not hold up the whole comparison.
	storeTimeout time.Duration
	// heartbeat is the interval of "ping" lines in the price stream. They keep
	// the connection open through reverse proxies (nginx default: 60 s).
	heartbeat time.Duration
	// prices converts AI usage into an estimated cost.
	prices pricing.PriceTable
}

// Options configures the server.
type Options struct {
	// StaticDir is the frontend build directory (optional).
	StaticDir string
	// Parallel limits concurrent shop research.
	Parallel int
	// StoreTimeout is the maximum research time per shop (default 3 min).
	StoreTimeout time.Duration
	// Heartbeat is the interval of keep-alive lines in the stream (default 15 s).
	Heartbeat time.Duration
	// Prices is the price table for cost estimates (default: built-in prices).
	Prices *pricing.PriceTable
}

func New(r research.Researcher, opts Options) *Server {
	if opts.Parallel < 1 {
		opts.Parallel = 4
	}
	if opts.StoreTimeout <= 0 {
		opts.StoreTimeout = 3 * time.Minute
	}
	if opts.Heartbeat <= 0 {
		opts.Heartbeat = 15 * time.Second
	}
	prices := pricing.DefaultPriceTable()
	if opts.Prices != nil {
		prices = *opts.Prices
	}
	return &Server{
		prices:       prices,
		storeTimeout: opts.StoreTimeout,
		heartbeat:    opts.Heartbeat,
		researcher:   r,
		cache:        newTTLCache(),
		parallel:     opts.Parallel,
		staticDir:    opts.StaticDir,
		recipeTTL:    24 * time.Hour,
		priceTTL:     6 * time.Hour,
		callTimeout:  8 * time.Minute,
	}
}

// Handler returns the HTTP handler with all routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/config", s.handleConfig)
	mux.HandleFunc("GET /api/stores", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, research.Stores)
	})
	mux.HandleFunc("POST /api/recipes/search", s.handleRecipeSearch)
	mux.HandleFunc("POST /api/prices/compare", s.handlePriceCompare)
	if s.staticDir != "" {
		mux.Handle("/", spaHandler(s.staticDir))
	}
	return logRequests(mux)
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	_, demo := s.researcher.(*research.Demo)
	writeJSON(w, http.StatusOK, map[string]any{"provider": s.researcher.Name(), "demo": demo})
}

func (s *Server) handleRecipeSearch(w http.ResponseWriter, r *http.Request) {
	var q model.RecipeQuery
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&q); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Anfrage: "+err.Error())
		return
	}
	q.Query = strings.TrimSpace(q.Query)
	if q.Query == "" {
		writeError(w, http.StatusBadRequest, "Bitte einen Suchbegriff angeben.")
		return
	}
	if len(q.Query) > 300 {
		writeError(w, http.StatusBadRequest, "Suchbegriff ist zu lang.")
		return
	}
	if q.Servings < 1 || q.Servings > 20 {
		q.Servings = 4
	}
	if q.MaxRecipes < 1 || q.MaxRecipes > 8 {
		q.MaxRecipes = 4
	}

	cacheKey := "recipes:" + hashKey(q)
	if v, ok := s.cache.get(cacheKey); ok {
		entry := v.(recipeCacheEntry)
		writeJSON(w, http.StatusOK, map[string]any{"recipes": entry.recipes, "cached": true, "usage": cachedUsage(entry.usage)})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.callTimeout)
	defer cancel()
	recipes, usage, err := s.researcher.SearchRecipes(ctx, q)
	s.priceUsage(ctx, &usage, err)
	logUsage("recipe search", "", usage)
	if err != nil {
		slog.Error("recipe search failed", "err", err)
		// A failed research can still have been billed, so the usage is
		// returned with the error, like for failed shops in the price stream.
		writeJSON(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "usage": usage})
		return
	}
	for i := range recipes {
		if recipes[i].ID == "" {
			recipes[i].ID = fmt.Sprintf("r%d-%s", i, hashKey(recipes[i].Title + recipes[i].SourceURL)[:8])
		}
		if recipes[i].Servings == 0 {
			recipes[i].Servings = q.Servings
		}
	}
	s.cache.set(cacheKey, recipeCacheEntry{recipes, usage}, s.recipeTTL)
	writeJSON(w, http.StatusOK, map[string]any{"recipes": recipes, "cached": false, "usage": usage})
}

// recipeCacheEntry is a cached recipe search result. The usage of the
// original research is kept for its provider and model.
type recipeCacheEntry struct {
	recipes []model.Recipe
	usage   model.Usage
}

// priceUsage computes the cost of a research call's usage.
//   - A request cut off by a timeout or cancellation reports no usage
//     although it may have been billed: the cost is marked incomplete (a
//     lower bound), or unknown if no response arrived at all (see
//     pricing.PriceTable.Apply).
//   - If the provider certainly rejected the failing request before billing
//     (research.IsNotBilled, e.g. an invalid key or a rate limit), the usage
//     reported so far is complete; without any usage the cost is 0.
func (s *Server) priceUsage(ctx context.Context, u *model.Usage, err error) {
	s.prices.Apply(u)
	if err == nil || u.Cached || u.Provider == "demo" {
		return
	}
	if research.IsNotBilled(err) && ctx.Err() == nil {
		if !u.CostKnown && u.Requests == 0 && u.TotalTokens() == 0 && u.WebSearches == 0 && u.WebFetches == 0 {
			u.CostEUR, u.CostKnown = 0, true
		}
		return
	}
	if ctx.Err() != nil && u.CostKnown {
		u.Incomplete = true
	}
}

// cachedUsage returns the usage of a cache hit: no new AI calls, no cost.
func cachedUsage(original model.Usage) model.Usage {
	return model.Usage{Provider: original.Provider, Model: original.Model, Cached: true, CostKnown: true}
}

// logUsage logs the AI usage of a research call (no user data, no keys).
func logUsage(what, store string, u model.Usage) {
	if u.Requests == 0 {
		return
	}
	slog.Info("ai usage", "call", what, "store", store, "provider", u.Provider, "model", u.Model,
		"requests", u.Requests, "input_tokens", u.InputTokens, "output_tokens", u.OutputTokens,
		"cache_read_tokens", u.CacheReadTokens, "cache_write_tokens", u.CacheWriteTokens,
		"web_searches", u.WebSearches, "web_fetches", u.WebFetches, "cost_eur", u.CostEUR, "cost_known", u.CostKnown, "incomplete", u.Incomplete)
}

// priceEvent is one line in the NDJSON stream of /api/prices/compare.
type priceEvent struct {
	Type       string             `json:"type"` // "store" | "summary" | "error" | "ping"
	Store      *model.StorePrices `json:"store,omitempty"`
	Comparison *model.Comparison  `json:"comparison,omitempty"`
	Message    string             `json:"message,omitempty"`
}

// handlePriceCompare researches prices in all selected shops in parallel and
// streams each result as an NDJSON line as soon as it is ready, followed by a
// summary with the price comparison.
func (s *Server) handlePriceCompare(w http.ResponseWriter, r *http.Request) {
	var req model.PriceRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Ungültige Anfrage: "+err.Error())
		return
	}
	ingredients := req.Ingredients[:0]
	for _, in := range req.Ingredients {
		in.Name = strings.TrimSpace(in.Name)
		if in.Name != "" {
			ingredients = append(ingredients, in)
		}
	}
	if len(ingredients) == 0 {
		writeError(w, http.StatusBadRequest, "Keine Zutaten angegeben.")
		return
	}
	if len(ingredients) > 30 {
		writeError(w, http.StatusBadRequest, "Maximal 30 Zutaten pro Vergleich.")
		return
	}
	var stores []model.Store
	for _, id := range req.StoreIDs {
		if st, ok := research.StoreByID(id); ok {
			stores = append(stores, st)
		}
	}
	if len(stores) == 0 {
		writeError(w, http.StatusBadRequest, "Bitte mindestens einen Shop auswählen.")
		return
	}
	plz := strings.TrimSpace(req.PostalCode)
	if len(plz) > 10 {
		plz = plz[:10]
	}

	flusher, _ := w.(http.Flusher)
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	enc := json.NewEncoder(w)
	var writeMu sync.Mutex
	finished := false // no writes after the handler has returned
	emit := func(ev priceEvent) {
		writeMu.Lock()
		defer writeMu.Unlock()
		if finished {
			return
		}
		_ = enc.Encode(ev)
		if flusher != nil {
			flusher.Flush()
		}
	}

	// Send keep-alive lines while research is running.
	done := make(chan struct{})
	defer func() {
		close(done)
		writeMu.Lock()
		finished = true
		writeMu.Unlock()
	}()
	go func() {
		t := time.NewTicker(s.heartbeat)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-r.Context().Done():
				return
			case <-t.C:
				emit(priceEvent{Type: "ping"})
			}
		}
	}()

	results := make([]model.StorePrices, len(stores))
	sem := make(chan struct{}, s.parallel)
	var wg sync.WaitGroup
	for i, st := range stores {
		wg.Add(1)
		go func(i int, st model.Store) {
			defer wg.Done()
			sp := s.storePrices(r.Context(), sem, st, ingredients, plz)
			results[i] = sp
			emit(priceEvent{Type: "store", Store: &sp})
		}(i, st)
	}
	wg.Wait()

	cmp := pricing.Compare(ingredients, results)
	var usages []model.Usage
	for _, sp := range results {
		if sp.Usage != nil {
			usages = append(usages, *sp.Usage)
		}
	}
	if len(usages) > 0 {
		total := model.SumUsage(usages...)
		cmp.Usage = &total
	}
	emit(priceEvent{Type: "summary", Comparison: &cmp})
}

// storePrices returns a shop's prices from the cache or researches them with
// their own time limit. Cached results do not wait for a free research slot,
// so they come back instantly on a retry.
func (s *Server) storePrices(ctx context.Context, sem chan struct{}, st model.Store, ingredients []model.Ingredient, plz string) model.StorePrices {
	cacheKey := "prices:" + hashKey(struct {
		S string
		I []model.Ingredient
		P string
	}{st.ID, ingredients, plz})

	var sp model.StorePrices
	if v, ok := s.cache.get(cacheKey); ok {
		sp = v.(model.StorePrices)
		sp.Cached = true
		var original model.Usage
		if sp.Usage != nil {
			original = *sp.Usage
		}
		u := cachedUsage(original)
		sp.Usage = &u
	} else {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		case <-ctx.Done():
			return model.StorePrices{StoreID: st.ID, Error: "Abgebrochen", Offers: []model.Offer{}, NotFound: []string{}}
		}
		storeCtx, cancel := context.WithTimeout(ctx, s.storeTimeout)
		defer cancel()

		var (
			usage model.Usage
			err   error
		)
		sp, usage, err = s.researcher.FindPrices(storeCtx, st, ingredients, plz)
		sp.StoreID = st.ID
		// Usage is reported for failed research too: the calls made so far
		// were billed.
		s.priceUsage(storeCtx, &usage, err)
		sp.Usage = &usage
		logUsage("price research", st.ID, usage)
		switch {
		case err != nil && errors.Is(storeCtx.Err(), context.DeadlineExceeded):
			slog.Warn("price research timed out", "store", st.ID, "limit", s.storeTimeout)
			sp.Error = fmt.Sprintf("Zeitüberschreitung: keine Antwort nach %s", formatDuration(s.storeTimeout))
			sp.TimedOut = true
		case err != nil:
			slog.Error("price research failed", "store", st.ID, "err", err)
			sp.Error = err.Error()
		default:
			pricing.Enrich(&sp, ingredients)
			s.cache.set(cacheKey, sp, s.priceTTL)
		}
	}
	if sp.Offers == nil {
		sp.Offers = []model.Offer{}
	}
	if sp.NotFound == nil {
		sp.NotFound = []string{}
	}
	return sp
}

func formatDuration(d time.Duration) string {
	if d%time.Minute == 0 {
		return fmt.Sprintf("%d min", int(d/time.Minute))
	}
	if d%time.Second == 0 {
		return fmt.Sprintf("%d s", int(d/time.Second))
	}
	return d.String()
}

// ---------------------------------------------------------------------------
// Helpers

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func hashKey(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/api/") {
			slog.Info("request", "method", r.Method, "path", r.URL.Path, "duration", time.Since(start).Round(time.Millisecond))
		}
	})
}

// spaHandler serves files from dir and falls back to index.html for unknown
// paths so that client-side routing works.
func spaHandler(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			http.ServeFile(w, r, filepath.Join(dir, "index.html"))
			return
		}
		fs.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// Simple in-memory cache with expiry to avoid repeating expensive AI research.

type cacheEntry struct {
	value   any
	expires time.Time
}

type ttlCache struct {
	mu    sync.Mutex
	items map[string]cacheEntry
}

func newTTLCache() *ttlCache { return &ttlCache{items: map[string]cacheEntry{}} }

func (c *ttlCache) get(k string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[k]
	if !ok || time.Now().After(e.expires) {
		delete(c.items, k)
		return nil, false
	}
	return e.value, true
}

func (c *ttlCache) set(k string, v any, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) > 1000 {
		// Rough cleanup: drop the half that expires soonest.
		keys := make([]string, 0, len(c.items))
		for key := range c.items {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool { return c.items[keys[i]].expires.Before(c.items[keys[j]].expires) })
		for _, key := range keys[:len(keys)/2] {
			delete(c.items, key)
		}
	}
	c.items[k] = cacheEntry{value: v, expires: time.Now().Add(ttl)}
}
