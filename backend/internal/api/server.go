// Package api stellt die HTTP-Schnittstelle für das React-Frontend bereit.
package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// Server bündelt Researcher, Cache und Routen.
type Server struct {
	researcher  research.Researcher
	cache       *ttlCache
	parallel    int
	staticDir   string
	recipeTTL   time.Duration
	priceTTL    time.Duration
	callTimeout time.Duration
}

// Options konfiguriert den Server.
type Options struct {
	// StaticDir ist das Build-Verzeichnis des Frontends (optional).
	StaticDir string
	// Parallel begrenzt gleichzeitige Shop-Recherchen.
	Parallel int
}

func New(r research.Researcher, opts Options) *Server {
	if opts.Parallel < 1 {
		opts.Parallel = 4
	}
	return &Server{
		researcher:  r,
		cache:       newTTLCache(),
		parallel:    opts.Parallel,
		staticDir:   opts.StaticDir,
		recipeTTL:   24 * time.Hour,
		priceTTL:    6 * time.Hour,
		callTimeout: 8 * time.Minute,
	}
}

// Handler liefert den HTTP-Handler mit allen Routen.
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
		writeJSON(w, http.StatusOK, map[string]any{"recipes": v, "cached": true})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.callTimeout)
	defer cancel()
	recipes, err := s.researcher.SearchRecipes(ctx, q)
	if err != nil {
		slog.Error("Rezeptsuche fehlgeschlagen", "err", err)
		writeError(w, http.StatusBadGateway, err.Error())
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
	s.cache.set(cacheKey, recipes, s.recipeTTL)
	writeJSON(w, http.StatusOK, map[string]any{"recipes": recipes, "cached": false})
}

// priceEvent ist eine Zeile im NDJSON-Stream von /api/prices/compare.
type priceEvent struct {
	Type       string             `json:"type"` // "store" | "summary" | "error"
	Store      *model.StorePrices `json:"store,omitempty"`
	Comparison *model.Comparison  `json:"comparison,omitempty"`
	Message    string             `json:"message,omitempty"`
}

// handlePriceCompare recherchiert die Preise parallel in allen gewählten
// Shops und streamt jedes Ergebnis als NDJSON-Zeile, sobald es vorliegt.
// Zum Schluss folgt eine Zusammenfassung mit dem Preisvergleich.
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
	emit := func(ev priceEvent) {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = enc.Encode(ev)
		if flusher != nil {
			flusher.Flush()
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.callTimeout)
	defer cancel()

	results := make([]model.StorePrices, len(stores))
	sem := make(chan struct{}, s.parallel)
	var wg sync.WaitGroup
	for i, st := range stores {
		wg.Add(1)
		go func(i int, st model.Store) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			cacheKey := "prices:" + hashKey(struct {
				S string
				I []model.Ingredient
				P string
			}{st.ID, ingredients, plz})
			var sp model.StorePrices
			if v, ok := s.cache.get(cacheKey); ok {
				sp = v.(model.StorePrices)
				sp.Cached = true
			} else {
				var err error
				sp, err = s.researcher.FindPrices(ctx, st, ingredients, plz)
				sp.StoreID = st.ID
				if err != nil {
					slog.Error("Preisrecherche fehlgeschlagen", "store", st.ID, "err", err)
					sp.Error = err.Error()
				} else {
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
			results[i] = sp
			emit(priceEvent{Type: "store", Store: &sp})
		}(i, st)
	}
	wg.Wait()

	cmp := pricing.Compare(ingredients, results)
	emit(priceEvent{Type: "summary", Comparison: &cmp})
}

// ---------------------------------------------------------------------------
// Hilfsfunktionen

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
			slog.Info("request", "method", r.Method, "path", r.URL.Path, "dauer", time.Since(start).Round(time.Millisecond))
		}
	})
}

// spaHandler liefert Dateien aus dir aus und fällt für unbekannte Pfade auf
// index.html zurück, damit clientseitiges Routing funktioniert.
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
// Einfacher In-Memory-Cache mit Ablaufzeit, um teure KI-Recherchen zu sparen.

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
		// Grob aufräumen: abgelaufene zuerst, sonst die ältesten.
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
