package research

import (
	"fmt"
	"strings"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

// Gemeinsame Prompts und Antwort-Parser für alle KI-Anbieter. Nur der Hinweis,
// welche Such-Werkzeuge es gibt, unterscheidet sich je Anbieter.

const jsonInstruction = `Antworte am Ende ausschließlich mit einem JSON-Codeblock (markiert mit drei Backticks und "json") in genau diesem Format:
`

func recipeSystemPrompt(toolHint string) string {
	return `Du bist ein Rechercheassistent für eine Rezept-App in Deutschland.
Suche nach echten, gut bewerteten Rezepten auf deutschsprachigen Seiten
(z. B. chefkoch.de, lecker.de, eatsmarter.de, kitchenstories.com, essen-und-trinken.de).
` + toolHint + `

Achte auf preiswerte Rezepte mit Zutaten, die es in normalen Supermärkten gibt.
Rechne die Zutatenmengen auf die gewünschte Portionenzahl um.
Verwende für Mengen möglichst die Einheiten g, kg, ml, l, Stück, EL, TL, Prise, Bund, Dose, Packung.
Markiere Grundzutaten, die fast jeder zuhause hat (Salz, Pfeffer, Zucker, Öl, Mehl in kleinen Mengen, Wasser), mit "pantry": true.
Erfinde keine Quellen: sourceUrl muss eine Seite sein, die du in der Recherche gefunden hast.

` + jsonInstruction + `{"recipes":[{"title":"","description":"","sourceUrl":"","sourceName":"","servings":4,"totalTimeMinutes":30,
"difficulty":"einfach|mittel|anspruchsvoll","estimatedCostEur":0.0,
"ingredients":[{"name":"","quantity":0,"unit":"","pantry":false}],"steps":[""]}]}`
}

func priceSystemPrompt(toolHint string) string {
	return `Du bist ein Preisrecherche-Assistent für Lebensmittel in Deutschland.
Du bekommst einen Supermarkt und eine Zutatenliste. Finde für jede Zutat das günstigste passende Produkt
im Online-Shop bzw. im Online-Angebot dieses Supermarkts.
` + toolHint + `

Regeln:
- Bevorzuge Eigenmarken und Grundsortiment (z. B. "ja!", "GUT&GÜNSTIG", "Milsani"), sofern sie passen.
- Aktuelle Angebote sind erlaubt, markiere sie mit "isPromotion": true.
- Gib den Preis in Euro für EINE Packung an und die Packungsgröße als Zahl plus Einheit (g, kg, ml, l, Stück).
- "packagesNeeded" ist die Anzahl Packungen, die für die angegebene Rezeptmenge nötig ist (mindestens 1).
- "confidence": "hoch", wenn der Preis direkt auf der Shopseite stand; "mittel", wenn er aus einem Prospekt
  oder einer Suchvorschau stammt; "niedrig", wenn es eine Schätzung ist.
- Erfinde keine Preise ohne Kennzeichnung. Wenn du eine Zutat nicht findest, setze sie in "notFound".
- "url" muss eine Seite sein, die du tatsächlich gefunden hast, sonst leer lassen.

` + jsonInstruction + `{"offers":[{"ingredient":"","productName":"","price":0.0,"packageAmount":0,"packageUnit":"",
"packagesNeeded":1,"unitPrice":"","url":"","isPromotion":false,"confidence":"hoch|mittel|niedrig","note":""}],
"notFound":[""]}`
}

func recipeUserPrompt(q model.RecipeQuery) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Suche %d Rezepte für: %s\n", q.MaxRecipes, q.Query)
	fmt.Fprintf(&b, "Portionen: %d\n", q.Servings)
	if q.MaxBudget > 0 {
		fmt.Fprintf(&b, "Budget: höchstens ca. %.2f € für alle Portionen zusammen\n", q.MaxBudget)
	}
	if q.Diet != "" {
		fmt.Fprintf(&b, "Ernährungsform: %s\n", q.Diet)
	}
	if q.ExcludeText != "" {
		fmt.Fprintf(&b, "Nicht verwenden: %s\n", q.ExcludeText)
	}
	return b.String()
}

func priceUserPrompt(store model.Store, ingredients []model.Ingredient, postalCode string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Supermarkt: %s (Websites: %s)\n", store.Name, strings.Join(store.Domains, ", "))
	if postalCode != "" {
		fmt.Fprintf(&b, "Postleitzahl für regionale Preise und Angebote: %s\n", postalCode)
	}
	b.WriteString("Zutaten (Rezeptmengen):\n")
	for _, ing := range ingredients {
		fmt.Fprintf(&b, "- %s: %s %s\n", ing.Name, trimFloat(ing.Quantity), ing.Unit)
	}
	return b.String()
}

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
	s := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", f), "0"), ".")
	return strings.Replace(s, ".", ",", 1)
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
