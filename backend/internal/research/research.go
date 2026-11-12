// Package research kapselt die KI-gestützte Web-Recherche hinter einer
// austauschbaren Schnittstelle.
package research

import (
	"context"
	"fmt"
	"strings"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

// Researcher ist die Schnittstelle, die jeder KI-Anbieter implementiert.
// Neue Anbieter (z. B. ein anderes LLM oder ein lokales Modell) müssen nur
// diese beiden Methoden bereitstellen und in New registriert werden.
type Researcher interface {
	// Name liefert einen kurzen Anzeigenamen des Anbieters.
	Name() string
	// SearchRecipes recherchiert passende Rezepte im Web.
	SearchRecipes(ctx context.Context, q model.RecipeQuery) ([]model.Recipe, error)
	// FindPrices sucht die Zutaten im Online-Angebot eines Shops.
	FindPrices(ctx context.Context, store model.Store, ingredients []model.Ingredient, postalCode string) (model.StorePrices, error)
}

// Config steuert, welcher Anbieter verwendet wird.
type Config struct {
	Provider string // "claude", "demo" oder leer (automatisch)
	APIKey   string
	Model    string
	Effort   string
}

// New erzeugt den konfigurierten Researcher. Ohne Anbieterangabe wird Claude
// verwendet, sofern ein API-Key gesetzt ist, sonst der Demo-Modus.
func New(cfg Config) (Researcher, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	if provider == "" {
		if cfg.APIKey != "" {
			provider = "claude"
		} else {
			provider = "demo"
		}
	}
	switch provider {
	case "claude", "anthropic":
		return NewClaude(cfg), nil
	case "demo", "mock":
		return NewDemo(), nil
	default:
		return nil, fmt.Errorf("unbekannter KI-Anbieter %q (erlaubt: claude, demo)", cfg.Provider)
	}
}

// Stores ist die Liste der unterstützten Händler.
var Stores = []model.Store{
	{ID: "rewe", Name: "REWE", Domains: []string{"rewe.de", "shop.rewe.de"}, Color: "#cc071e"},
	{ID: "edeka", Name: "EDEKA", Domains: []string{"edeka.de", "edeka24.de"}, Color: "#1a4b9b"},
	{ID: "aldi-sued", Name: "ALDI SÜD", Domains: []string{"aldi-sued.de"}, Color: "#00005f"},
	{ID: "aldi-nord", Name: "ALDI Nord", Domains: []string{"aldi-nord.de"}, Color: "#00b4dc"},
	{ID: "lidl", Name: "Lidl", Domains: []string{"lidl.de"}, Color: "#0050aa"},
	{ID: "penny", Name: "PENNY", Domains: []string{"penny.de"}, Color: "#cd1414"},
	{ID: "kaufland", Name: "Kaufland", Domains: []string{"kaufland.de"}, Color: "#e10915"},
}

// StoreByID sucht einen Händler anhand seiner ID.
func StoreByID(id string) (model.Store, bool) {
	for _, s := range Stores {
		if s.ID == id {
			return s, true
		}
	}
	return model.Store{}, false
}
