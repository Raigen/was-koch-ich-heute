// Package model contains the data types exchanged between backend and frontend.
// Keep in sync with frontend/src/types.ts.
package model

// Store describes a grocery retailer with an online shop or online flyer.
type Store struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Domains []string `json:"domains"`
	Color   string   `json:"color"`
}

// Ingredient is an ingredient of a recipe.
type Ingredient struct {
	Name     string  `json:"name"`
	Quantity float64 `json:"quantity"`
	Unit     string  `json:"unit"`
	// Pantry marks staples (salt, pepper, oil …) that people usually have at home.
	Pantry bool `json:"pantry"`
}

// Recipe is a researched recipe.
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
	// EstimatedCostEUR is a rough AI estimate made before real prices are researched.
	EstimatedCostEUR float64 `json:"estimatedCostEur"`
}

// RecipeQuery is a recipe search request.
type RecipeQuery struct {
	Query       string  `json:"query"`
	Servings    int     `json:"servings"`
	MaxBudget   float64 `json:"maxBudget"`
	Diet        string  `json:"diet"`
	MaxRecipes  int     `json:"maxRecipes"`
	ExcludeText string  `json:"exclude"`
}

// Offer is a concrete product in a shop that matches an ingredient.
type Offer struct {
	StoreID       string  `json:"storeId"`
	Ingredient    string  `json:"ingredient"`
	ProductName   string  `json:"productName"`
	Price         float64 `json:"price"`
	PackageAmount float64 `json:"packageAmount"`
	PackageUnit   string  `json:"packageUnit"`
	// PackagesNeeded is computed in the backend from the recipe quantity and the
	// package size when the units are comparable; otherwise the AI's value is used.
	PackagesNeeded int     `json:"packagesNeeded"`
	Cost           float64 `json:"cost"`
	UnitPrice      string  `json:"unitPrice"`
	URL            string  `json:"url"`
	IsPromotion    bool    `json:"isPromotion"`
	Confidence     string  `json:"confidence"`
	Note           string  `json:"note"`
}

// StorePrices is the research result for one shop.
type StorePrices struct {
	StoreID  string   `json:"storeId"`
	Offers   []Offer  `json:"offers"`
	NotFound []string `json:"notFound"`
	Error    string   `json:"error,omitempty"`
	TimedOut bool     `json:"timedOut,omitempty"`
	Cached   bool     `json:"cached"`
}

// PriceRequest asks for prices of an ingredient list in several shops.
type PriceRequest struct {
	Ingredients []Ingredient `json:"ingredients"`
	StoreIDs    []string     `json:"storeIds"`
	PostalCode  string       `json:"postalCode"`
}

// StoreTotal is the basket total in a single shop.
type StoreTotal struct {
	StoreID string   `json:"storeId"`
	Total   float64  `json:"total"`
	Found   int      `json:"found"`
	Missing []string `json:"missing"`
}

// MixItem is one line of the cheapest basket across all shops.
type MixItem struct {
	Ingredient string `json:"ingredient"`
	Offer      *Offer `json:"offer"`
}

// Comparison summarizes the prices of all shops.
type Comparison struct {
	StoreTotals []StoreTotal `json:"storeTotals"`
	// CheapestStoreID is the cheapest shop among those with the most ingredients found.
	CheapestStoreID string    `json:"cheapestStoreId"`
	Mix             []MixItem `json:"mix"`
	MixTotal        float64   `json:"mixTotal"`
	MixMissing      []string  `json:"mixMissing"`
	IngredientCount int       `json:"ingredientCount"`
}
