// Package mealietest provides a fake Mealie server for tests. Its responses
// are recorded in the shape of the Mealie v3 API (see testdata/).
package mealietest

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
)

//go:embed testdata/search.json
var searchJSON []byte

//go:embed testdata/recipe.json
var recipeJSON []byte

const (
	// Token is the API token the fake accepts.
	Token = "test-mealie-token"
	// RecipeID is the ID of the recipe in testdata/recipe.json.
	RecipeID = "5f0c8a52-6a7e-4d3b-9a43-0d6c2a1e7b11"
	// GroupSlug is the slug returned by /api/groups/self.
	GroupSlug = "home"
)

// ImageBytes is the body of the fake recipe image.
var ImageBytes = []byte("RIFF\x00\x00\x00\x00WEBPfake")

// Fake is a running fake Mealie server.
type Fake struct {
	*httptest.Server

	mu      sync.Mutex
	queries []url.Values
}

// Queries returns the query parameters of all /api/recipes list requests.
func (f *Fake) Queries() []url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]url.Values(nil), f.queries...)
}

// New starts a fake Mealie server. The caller must call Close.
func New() *Fake {
	f := &Fake{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/recipes", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.queries = append(f.queries, r.URL.Query())
		f.mu.Unlock()
		search := strings.ToLower(r.URL.Query().Get("search"))
		var page map[string]any
		_ = json.Unmarshal(searchJSON, &page)
		var items []any
		for _, it := range page["items"].([]any) {
			name := strings.ToLower(it.(map[string]any)["name"].(string))
			if strings.Contains(name, search) {
				items = append(items, it)
			}
		}
		if items == nil {
			items = []any{}
		}
		page["items"] = items
		page["total"] = len(items)
		writeJSON(w, page)
	})
	mux.HandleFunc("GET /api/recipes/{slug}", func(w http.ResponseWriter, r *http.Request) {
		if s := r.PathValue("slug"); s != "linsen-dal" && s != RecipeID {
			http.Error(w, `{"detail":"Not Found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(recipeJSON)
	})
	mux.HandleFunc("GET /api/groups/self", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"id": "a3d9c1f2-0000-4000-8000-000000000003", "name": "Home", "slug": GroupSlug})
	})
	mux.HandleFunc("GET /api/media/recipes/{id}/images/{file}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != RecipeID || r.PathValue("file") != "min-original.webp" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/webp")
		_, _ = w.Write(ImageBytes)
	})

	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+Token {
			http.Error(w, `{"detail":"Could not validate credentials"}`, http.StatusUnauthorized)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	return f
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
