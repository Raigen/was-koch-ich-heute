package research

import (
	"fmt"
	"strings"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

// Prompts and response parsers shared by all AI providers. Only the hint about
// the available search tools differs per provider.
//
// The prompts are English, but the model is told to write all user-facing
// values in German: the app targets German supermarkets and has a German UI.
// The enum values "einfach|mittel|anspruchsvoll" and "hoch|mittel|niedrig"
// are displayed as-is in the UI (and "confidence" is used as a CSS class), so
// they stay German.

const jsonInstruction = `At the end, respond only with a JSON code block (fenced with three backticks and "json") in exactly this format:
`

const germanOutput = `Write all text values (titles, descriptions, ingredient names, units, steps, notes) in German.`

func recipeSystemPrompt(toolHint string) string {
	return `You are a research assistant for a recipe app in Germany.
Search for real, well-rated recipes on German-language websites
(e.g. chefkoch.de, lecker.de, eatsmarter.de, kitchenstories.com, essen-und-trinken.de).
` + toolHint + `

Prefer inexpensive recipes with ingredients available in regular supermarkets.
Scale ingredient quantities to the requested number of servings.
Use these units where possible: g, kg, ml, l, Stück, EL, TL, Prise, Bund, Dose, Packung.
Mark staples almost everyone has at home (salt, pepper, sugar, oil, small amounts of flour, water) with "pantry": true.
Do not invent sources: sourceUrl must be a page you actually found during research.
` + germanOutput + `

` + jsonInstruction + `{"recipes":[{"title":"","description":"","sourceUrl":"","sourceName":"","servings":4,"totalTimeMinutes":30,
"difficulty":"einfach|mittel|anspruchsvoll","estimatedCostEur":0.0,
"ingredients":[{"name":"","quantity":0,"unit":"","pantry":false}],"steps":[""]}]}`
}

func priceSystemPrompt(toolHint string) string {
	return `You are a grocery price research assistant for Germany.
You receive a supermarket and a list of ingredients. For each ingredient, find the cheapest matching product
in this supermarket's online shop or online offers.
` + toolHint + `

Rules:
- Prefer store brands and the basic range (e.g. "ja!", "GUT&GÜNSTIG", "Milsani") where they fit.
- Current promotions are allowed; mark them with "isPromotion": true.
- Give the price in euros for ONE package and the package size as a number plus unit (g, kg, ml, l, Stück).
- "packagesNeeded" is the number of packages required for the given recipe quantity (at least 1).
- "confidence": "hoch" if the price was shown directly on the shop page; "mittel" if it comes from a flyer
  or a search preview; "niedrig" if it is an estimate.
- Never invent prices without marking them. If you cannot find an ingredient, put it into "notFound".
- "url" must be a page you actually found; otherwise leave it empty.
- Use the ingredient names exactly as given in "ingredient".
` + germanOutput + `

` + jsonInstruction + `{"offers":[{"ingredient":"","productName":"","price":0.0,"packageAmount":0,"packageUnit":"",
"packagesNeeded":1,"unitPrice":"","url":"","isPromotion":false,"confidence":"hoch|mittel|niedrig","note":""}],
"notFound":[""]}`
}

func recipeUserPrompt(q model.RecipeQuery) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Find %d recipes for: %s\n", q.MaxRecipes, q.Query)
	fmt.Fprintf(&b, "Servings: %d\n", q.Servings)
	if q.MaxBudget > 0 {
		fmt.Fprintf(&b, "Budget: at most about %.2f EUR for all servings combined\n", q.MaxBudget)
	}
	if q.Diet != "" {
		fmt.Fprintf(&b, "Diet: %s\n", q.Diet)
	}
	if q.ExcludeText != "" {
		fmt.Fprintf(&b, "Do not use: %s\n", q.ExcludeText)
	}
	return b.String()
}

func priceUserPrompt(store model.Store, ingredients []model.Ingredient, postalCode string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Supermarket: %s (websites: %s)\n", store.Name, strings.Join(store.Domains, ", "))
	if postalCode != "" {
		fmt.Fprintf(&b, "Postal code for regional prices and offers: %s\n", postalCode)
	}
	b.WriteString("Ingredients (recipe quantities):\n")
	for _, ing := range ingredients {
		fmt.Fprintf(&b, "- %s: %s %s\n", ing.Name, trimFloat(ing.Quantity), ing.Unit)
	}
	return b.String()
}

// The parse errors are shown in the German UI, hence German messages.
func parseRecipes(text string) ([]model.Recipe, error) {
	var out struct {
		Recipes []model.Recipe `json:"recipes"`
	}
	if err := extractJSON(text, &out); err != nil {
		return nil, fmt.Errorf("Rezeptantwort konnte nicht gelesen werden: %w", err)
	}
	return out.Recipes, nil
}

func parsePrices(store model.Store, text string) (model.StorePrices, error) {
	var out struct {
		Offers   []model.Offer `json:"offers"`
		NotFound []string      `json:"notFound"`
	}
	if err := extractJSON(text, &out); err != nil {
		return model.StorePrices{StoreID: store.ID}, fmt.Errorf("Preisantwort konnte nicht gelesen werden: %w", err)
	}
	for i := range out.Offers {
		out.Offers[i].StoreID = store.ID
	}
	return model.StorePrices{StoreID: store.ID, Offers: out.Offers, NotFound: compact(out.NotFound)}, nil
}

func trimFloat(f float64) string {
	if f == 0 {
		return ""
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", f), "0"), ".")
}

func compact(in []string) []string {
	out := in[:0]
	for _, s := range in {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}
