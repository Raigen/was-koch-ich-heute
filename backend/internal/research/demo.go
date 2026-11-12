package research

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"time"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

// Demo returns fixed sample data without calling an AI, so the UI can be
// tried without an API key. The prices are NOT real. Recipe and product data
// are German because they are displayed in the German UI.
type Demo struct {
	// Delay simulates the research time per shop.
	Delay time.Duration
}

func NewDemo() *Demo { return &Demo{Delay: 600 * time.Millisecond} }

func (d *Demo) Name() string { return "Demo (Beispieldaten, keine echten Preise)" }

type demoRecipe struct {
	recipe model.Recipe
	tags   []string
}

func ing(name string, qty float64, unit string, pantry bool) model.Ingredient {
	return model.Ingredient{Name: name, Quantity: qty, Unit: unit, Pantry: pantry}
}

// All quantities are for 4 servings.
var demoRecipes = []demoRecipe{
	{tags: []string{"pasta", "nudeln", "italienisch", "hackfleisch", "schnell"}, recipe: model.Recipe{
		ID: "demo-bolognese", Title: "Spaghetti Bolognese", Description: "Klassiker mit Rinderhack und Tomaten – günstig und familientauglich.",
		SourceName: "Demo", Servings: 4, TotalTimeMinutes: 45, Difficulty: "einfach", EstimatedCostEUR: 8.5,
		Ingredients: []model.Ingredient{ing("Spaghetti", 500, "g", false), ing("Rinderhackfleisch", 400, "g", false), ing("Passierte Tomaten", 500, "g", false),
			ing("Zwiebel", 2, "Stück", false), ing("Knoblauch", 2, "Zehe", false), ing("Karotte", 1, "Stück", false), ing("Olivenöl", 2, "EL", true), ing("Salz", 1, "Prise", true)},
		Steps: []string{"Zwiebel, Knoblauch und Karotte fein würfeln.", "Hackfleisch in Öl krümelig braten, Gemüse zugeben.", "Tomaten zugeben und 30 Minuten köcheln lassen.", "Spaghetti kochen und mit der Soße servieren."},
	}},
	{tags: []string{"vegetarisch", "vegan", "linsen", "indisch", "curry", "günstig"}, recipe: model.Recipe{
		ID: "demo-dal", Title: "Rotes Linsen-Dal", Description: "Cremiges Curry aus roten Linsen und Kokosmilch – vegan und sehr günstig.",
		SourceName: "Demo", Servings: 4, TotalTimeMinutes: 30, Difficulty: "einfach", EstimatedCostEUR: 5.2,
		Ingredients: []model.Ingredient{ing("Rote Linsen", 300, "g", false), ing("Kokosmilch", 400, "ml", false), ing("Gehackte Tomaten", 400, "g", false),
			ing("Zwiebel", 1, "Stück", false), ing("Reis", 250, "g", false), ing("Currypulver", 2, "TL", true), ing("Salz", 1, "Prise", true)},
		Steps: []string{"Zwiebel anschwitzen, Curry kurz mitrösten.", "Linsen, Tomaten und Kokosmilch zugeben, 20 Minuten köcheln.", "Mit Reis servieren."},
	}},
	{tags: []string{"suppe", "kartoffel", "vegetarisch", "eintopf", "günstig"}, recipe: model.Recipe{
		ID: "demo-kartoffelsuppe", Title: "Kartoffelsuppe", Description: "Deftige Suppe mit Kartoffeln, Lauch und Würstchen.",
		SourceName: "Demo", Servings: 4, TotalTimeMinutes: 40, Difficulty: "einfach", EstimatedCostEUR: 6.0,
		Ingredients: []model.Ingredient{ing("Kartoffeln", 1000, "g", false), ing("Lauch", 1, "Stück", false), ing("Karotte", 2, "Stück", false),
			ing("Wiener Würstchen", 4, "Stück", false), ing("Gemüsebrühe", 1, "l", true), ing("Schmand", 200, "g", false)},
		Steps: []string{"Gemüse schälen und würfeln.", "In Brühe 20 Minuten garen und grob pürieren.", "Würstchen in Scheiben zugeben, mit Schmand verfeinern."},
	}},
	{tags: []string{"chili", "vegan", "bohnen", "mexikanisch", "scharf"}, recipe: model.Recipe{
		ID: "demo-chili", Title: "Chili sin Carne", Description: "Bohnen, Mais und Paprika in würziger Tomatensoße.",
		SourceName: "Demo", Servings: 4, TotalTimeMinutes: 35, Difficulty: "einfach", EstimatedCostEUR: 6.4,
		Ingredients: []model.Ingredient{ing("Kidneybohnen", 2, "Dose", false), ing("Mais", 1, "Dose", false), ing("Paprika", 2, "Stück", false),
			ing("Gehackte Tomaten", 800, "g", false), ing("Zwiebel", 1, "Stück", false), ing("Reis", 250, "g", false), ing("Chilipulver", 1, "TL", true)},
		Steps: []string{"Zwiebel und Paprika anbraten.", "Tomaten, Bohnen und Mais zugeben, 20 Minuten köcheln.", "Mit Reis servieren."},
	}},
	{tags: []string{"pfannkuchen", "süß", "vegetarisch", "schnell", "kinder"}, recipe: model.Recipe{
		ID: "demo-pfannkuchen", Title: "Pfannkuchen mit Apfelmus", Description: "Schnelles Essen für die ganze Familie.",
		SourceName: "Demo", Servings: 4, TotalTimeMinutes: 25, Difficulty: "einfach", EstimatedCostEUR: 3.9,
		Ingredients: []model.Ingredient{ing("Weizenmehl", 300, "g", false), ing("Milch", 500, "ml", false), ing("Eier", 4, "Stück", false),
			ing("Apfelmus", 720, "ml", false), ing("Butter", 30, "g", false), ing("Zucker", 1, "EL", true)},
		Steps: []string{"Mehl, Milch und Eier zu einem glatten Teig verrühren.", "In Butter dünne Pfannkuchen ausbacken.", "Mit Apfelmus servieren."},
	}},
}

func (d *Demo) SearchRecipes(ctx context.Context, q model.RecipeQuery) ([]model.Recipe, error) {
	if err := sleep(ctx, d.Delay); err != nil {
		return nil, err
	}
	words := strings.Fields(strings.ToLower(q.Query + " " + q.Diet))
	type scored struct {
		r     model.Recipe
		score int
	}
	var list []scored
	for _, dr := range demoRecipes {
		hay := strings.ToLower(dr.recipe.Title + " " + dr.recipe.Description + " " + strings.Join(dr.tags, " "))
		s := 0
		for _, w := range words {
			if strings.Contains(hay, w) {
				s++
			}
		}
		list = append(list, scored{scaleRecipe(dr.recipe, q.Servings), s})
	}
	// Stable sort by number of matches (simple insertion sort).
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].score > list[j-1].score; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
	out := []model.Recipe{}
	for _, s := range list {
		if len(out) >= q.MaxRecipes {
			break
		}
		out = append(out, s.r)
	}
	return out, nil
}

func scaleRecipe(r model.Recipe, servings int) model.Recipe {
	if servings <= 0 || servings == r.Servings {
		return r
	}
	f := float64(servings) / float64(r.Servings)
	r.Ingredients = append([]model.Ingredient(nil), r.Ingredients...)
	for i := range r.Ingredients {
		if !r.Ingredients[i].Pantry {
			r.Ingredients[i].Quantity = math.Round(r.Ingredients[i].Quantity*f*10) / 10
		}
	}
	r.EstimatedCostEUR = math.Round(r.EstimatedCostEUR*f*100) / 100
	r.Servings = servings
	return r
}

type demoProduct struct {
	product string
	price   float64
	amount  float64
	unit    string
}

var demoCatalog = map[string]demoProduct{
	"spaghetti":         {"Spaghetti", 0.99, 500, "g"},
	"rinderhackfleisch": {"Rinderhackfleisch", 3.79, 400, "g"},
	"passierte tomaten": {"Passierte Tomaten", 0.69, 500, "g"},
	"gehackte tomaten":  {"Gehackte Tomaten", 0.59, 400, "g"},
	"zwiebel":           {"Speisezwiebeln 1 kg", 1.29, 10, "Stück"},
	"knoblauch":         {"Knoblauch 3 Knollen", 0.99, 30, "Zehe"},
	"karotte":           {"Karotten 1 kg", 0.99, 10, "Stück"},
	"olivenöl":          {"Olivenöl", 6.49, 500, "ml"},
	"salz":              {"Speisesalz", 0.39, 500, "g"},
	"rote linsen":       {"Rote Linsen", 1.79, 500, "g"},
	"kokosmilch":        {"Kokosmilch", 1.19, 400, "ml"},
	"reis":              {"Langkornreis", 1.29, 1000, "g"},
	"currypulver":       {"Currypulver", 1.09, 50, "g"},
	"kartoffeln":        {"Speisekartoffeln festkochend", 2.49, 2500, "g"},
	"lauch":             {"Lauch", 0.79, 1, "Stück"},
	"wiener würstchen":  {"Wiener Würstchen", 2.29, 6, "Stück"},
	"gemüsebrühe":       {"Gemüsebrühe instant", 1.49, 6, "l"},
	"schmand":           {"Schmand", 0.79, 200, "g"},
	"kidneybohnen":      {"Kidneybohnen", 0.69, 1, "Dose"},
	"mais":              {"Gemüsemais", 0.79, 1, "Dose"},
	"paprika":           {"Paprika rot", 0.69, 1, "Stück"},
	"chilipulver":       {"Chilipulver", 0.99, 40, "g"},
	"weizenmehl":        {"Weizenmehl Type 405", 0.59, 1000, "g"},
	"milch":             {"Frische Vollmilch", 1.09, 1000, "ml"},
	"eier":              {"Eier Bodenhaltung", 2.19, 10, "Stück"},
	"apfelmus":          {"Apfelmus", 0.99, 720, "ml"},
	"butter":            {"Deutsche Markenbutter", 2.29, 250, "g"},
	"zucker":            {"Zucker", 0.99, 1000, "g"},
}

var storeFactor = map[string]float64{
	"rewe": 1.12, "edeka": 1.15, "aldi-sued": 0.95, "aldi-nord": 0.96, "lidl": 0.97, "penny": 0.99, "kaufland": 1.0,
}

func (d *Demo) FindPrices(ctx context.Context, store model.Store, ingredients []model.Ingredient, postalCode string) (model.StorePrices, error) {
	if err := sleep(ctx, d.Delay+time.Duration(hash(store.ID)%900)*time.Millisecond); err != nil {
		return model.StorePrices{StoreID: store.ID}, err
	}
	res := model.StorePrices{StoreID: store.ID}
	for _, in := range ingredients {
		p, ok := demoCatalog[strings.ToLower(in.Name)]
		// Every shop "misses" a few ingredients so the UI can show that case.
		if !ok || hash(store.ID+in.Name)%11 == 0 {
			res.NotFound = append(res.NotFound, in.Name)
			continue
		}
		noise := 0.9 + float64(hash(store.ID+"|"+in.Name)%25)/100
		promo := hash(in.Name+store.ID+"promo")%7 == 0
		price := p.price * storeFactor[store.ID] * noise
		if promo {
			price *= 0.75
		}
		price = math.Round(price*100) / 100
		res.Offers = append(res.Offers, model.Offer{
			StoreID: store.ID, Ingredient: in.Name, ProductName: p.product + " (" + store.Name + ")",
			Price: price, PackageAmount: p.amount, PackageUnit: p.unit, PackagesNeeded: 1,
			IsPromotion: promo, Confidence: "niedrig", Note: "Demo-Daten – kein echter Preis",
		})
	}
	return res, nil
}

func hash(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

var _ Researcher = (*Demo)(nil)
