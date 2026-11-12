// Package model enthält die Datentypen, die zwischen Backend und Frontend
// ausgetauscht werden.
package model

// Store beschreibt einen Lebensmittelhändler mit Online-Shop bzw. Online-Prospekt.
type Store struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Domains []string `json:"domains"`
	Color   string   `json:"color"`
}

// Ingredient ist eine Zutat eines Rezepts.
type Ingredient struct {
	Name     string  `json:"name"`
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
	// Pantry markiert Grundzutaten (Salz, Pfeffer, Öl …), die meist schon im Haus sind.
	Pantry bool `json:"pantry"`
}

// Recipe ist ein recherchiertes Rezept.
type Recipe struct {
	ID               string       `json:"id"`
	Title            string       `json:"title"`
	Description      string       `json:"description"`
	SourceURL        string       `json:"sourceUrl"`
	SourceName       string       `json:"sourceName"`
	Servings         int          `json:"servings"`
	TotalTimeMinutes int          `json:"totalTimeMinutes"`
	Difficulty       string       `json:"difficulty"`
	Ingredients      []Ingredient `json:"ingredients"`
	Steps            []string     `json:"steps"`
	// EstimatedCostEUR ist eine grobe KI-Schätzung, bevor echte Preise recherchiert wurden.
	EstimatedCostEUR float64 `json:"estimatedCostEur"`
}

// RecipeQuery ist die Suchanfrage für Rezepte.
type RecipeQuery struct {
	Query       string  `json:"query"`
	Servings    int     `json:"servings"`
	MaxBudget   float64 `json:"maxBudget"`
	Diet        string  `json:"diet"`
	MaxRecipes  int     `json:"maxRecipes"`
	ExcludeText string  `json:"exclude"`
}

// Offer ist ein konkretes Produkt in einem Shop, das zu einer Zutat passt.
type Offer struct {
	StoreID       string  `json:"storeId"`
	Ingredient    string  `json:"ingredient"`
	ProductName   string  `json:"productName"`
	Price         float64 `json:"price"`
	PackageAmount float64 `json:"packageAmount"`
	PackageUnit   string  `json:"packageUnit"`
	// PackagesNeeded wird im Backend aus Rezeptmenge und Packungsgröße berechnet,
	// wenn die Einheiten vergleichbar sind; sonst gilt der Wert der KI.
	PackagesNeeded int     `json:"packagesNeeded"`
	Cost           float64 `json:"cost"`
	UnitPrice      string  `json:"unitPrice"`
	URL            string  `json:"url"`
	IsPromotion    bool    `json:"isPromotion"`
	Confidence     string  `json:"confidence"`
	Note           string  `json:"note"`
}

// StorePrices ist das Rechercheergebnis für einen Shop.
type StorePrices struct {
	StoreID  string   `json:"storeId"`
	Offers   []Offer  `json:"offers"`
	NotFound []string `json:"notFound"`
	Error    string   `json:"error,omitempty"`
	Cached   bool     `json:"cached"`
}

// PriceRequest fragt Preise für eine Zutatenliste in mehreren Shops an.
type PriceRequest struct {
	Ingredients []Ingredient `json:"ingredients"`
	StoreIDs    []string     `json:"storeIds"`
	PostalCode  string       `json:"postalCode"`
}

// StoreTotal ist die Summe eines Warenkorbs in einem einzelnen Shop.
type StoreTotal struct {
	StoreID string   `json:"storeId"`
	Total   float64  `json:"total"`
	Found   int      `json:"found"`
	Missing []string `json:"missing"`
}

// MixItem ist eine Position im günstigsten, shopübergreifenden Warenkorb.
type MixItem struct {
	Ingredient string `json:"ingredient"`
	Offer      *Offer `json:"offer"`
}

// Comparison fasst die Preise aller Shops zusammen.
type Comparison struct {
	StoreTotals []StoreTotal `json:"storeTotals"`
	// CheapestStoreID ist der günstigste Shop unter denen mit den meisten gefundenen Zutaten.
	CheapestStoreID string    `json:"cheapestStoreId"`
	Mix             []MixItem `json:"mix"`
	MixTotal        float64   `json:"mixTotal"`
	MixMissing      []string  `json:"mixMissing"`
	IngredientCount int       `json:"ingredientCount"`
}
