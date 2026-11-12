package mealie

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

// Page is Mealie's pagination response for GET /api/recipes. Unlike the
// recipe fields, the pagination fields are snake_case.
type Page struct {
	Page       int             `json:"page"`
	PerPage    int             `json:"per_page"`
	Total      int             `json:"total"`
	TotalPages int             `json:"total_pages"`
	Items      []RecipeSummary `json:"items"`
}

// RecipeSummary is the subset of Mealie's RecipeSummary used by this app.
type RecipeSummary struct {
	ID                  string     `json:"id"`
	Slug                string     `json:"slug"`
	Name                string     `json:"name"`
	Description         flexString `json:"description"`
	Image               flexString `json:"image"`
	RecipeServings      float64    `json:"recipeServings"`
	RecipeYieldQuantity float64    `json:"recipeYieldQuantity"`
	RecipeYield         flexString `json:"recipeYield"`
	TotalTime           flexString `json:"totalTime"`
	PrepTime            flexString `json:"prepTime"`
	PerformTime         flexString `json:"performTime"`
	TotalTimeSeconds    float64    `json:"totalTimeSeconds"`
	PrepTimeSeconds     float64    `json:"prepTimeSeconds"`
	PerformTimeSeconds  float64    `json:"performTimeSeconds"`
	OrgURL              flexString `json:"orgURL"`
}

// Recipe is the subset of Mealie's RecipeOut used by this app.
type Recipe struct {
	RecipeSummary
	RecipeIngredient   []Ingredient `json:"recipeIngredient"`
	RecipeInstructions []Step       `json:"recipeInstructions"`
}

// Ingredient is a Mealie recipe ingredient. Unit and Food are only set when
// the ingredient was parsed in Mealie; otherwise only Note/Display hold text.
type Ingredient struct {
	Quantity     float64    `json:"quantity"`
	Unit         *Unit      `json:"unit"`
	Food         *Food      `json:"food"`
	Note         flexString `json:"note"`
	Display      flexString `json:"display"`
	OriginalText flexString `json:"originalText"`
}

// Unit is a Mealie ingredient unit.
type Unit struct {
	Name            string     `json:"name"`
	Abbreviation    flexString `json:"abbreviation"`
	UseAbbreviation bool       `json:"useAbbreviation"`
}

// Food is a Mealie food.
type Food struct {
	Name string `json:"name"`
}

// Step is a Mealie instruction step. Title is an optional section heading.
type Step struct {
	Title flexString `json:"title"`
	Text  flexString `json:"text"`
}

// flexString accepts a JSON string, number or null. Mealie stores some free
// text fields (e.g. recipeYield) that older data may hold as numbers.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case bytes.Equal(b, []byte("null")):
		*f = ""
	case len(b) > 0 && b[0] == '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexString(s)
	case len(b) > 0 && (b[0] == '-' || (b[0] >= '0' && b[0] <= '9')):
		*f = flexString(b)
	default:
		// Objects, arrays or booleans carry no usable text.
		*f = ""
	}
	return nil
}

func (f flexString) String() string { return strings.TrimSpace(string(f)) }

// ---------------------------------------------------------------------------
// Mapping to the app model

// Summary maps a search result. ImageURL is left to the caller because it
// points to the app's own image proxy route.
func Summary(r RecipeSummary) model.MealieRecipeSummary {
	return model.MealieRecipeSummary{
		Slug:             r.Slug,
		Title:            strings.TrimSpace(r.Name),
		Description:      r.Description.String(),
		Servings:         int(math.Round(baseServings(r))),
		TotalTimeMinutes: totalMinutes(r),
	}
}

// ToRecipe maps a full Mealie recipe and scales the ingredient quantities to
// servings. If the recipe has no servings information, quantities stay
// unchanged and Servings is 0. ImageURL and MealieURL are set by the caller.
func ToRecipe(r Recipe, servings int) model.Recipe {
	out := model.Recipe{
		ID:               "mealie-" + r.Slug,
		Title:            strings.TrimSpace(r.Name),
		Description:      r.Description.String(),
		TotalTimeMinutes: totalMinutes(r.RecipeSummary),
		Ingredients:      []model.Ingredient{},
		Steps:            []string{},
	}
	if src := r.OrgURL.String(); src != "" {
		if u, err := url.Parse(src); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
			out.SourceURL = u.String()
			out.SourceName = strings.TrimPrefix(u.Hostname(), "www.")
		}
	}

	factor := 1.0
	if base := baseServings(r.RecipeSummary); base > 0 {
		out.Servings = int(math.Round(base))
		if servings > 0 {
			factor = float64(servings) / base
			out.Servings = servings
		}
	}

	for _, in := range r.RecipeIngredient {
		ing, ok := mapIngredient(in)
		if !ok {
			continue
		}
		ing.Quantity = roundQuantity(ing.Quantity * factor)
		out.Ingredients = append(out.Ingredients, ing)
	}
	for _, st := range r.RecipeInstructions {
		text := st.Text.String()
		if text == "" {
			continue
		}
		if title := st.Title.String(); title != "" {
			text = title + ": " + text
		}
		out.Steps = append(out.Steps, text)
	}
	return out
}

// baseServings returns the number of servings the recipe quantities refer to,
// or 0 if unknown.
func baseServings(r RecipeSummary) float64 {
	if r.RecipeServings > 0 {
		return r.RecipeServings
	}
	if r.RecipeYieldQuantity > 0 {
		return r.RecipeYieldQuantity
	}
	// Older recipes only have free text such as "4 Portionen".
	if m := leadingNumber.FindString(r.RecipeYield.String()); m != "" {
		if v, err := strconv.ParseFloat(strings.ReplaceAll(m, ",", "."), 64); err == nil && v > 0 {
			return v
		}
	}
	return 0
}

var leadingNumber = regexp.MustCompile(`^\d+(?:[.,]\d+)?`)

func totalMinutes(r RecipeSummary) int {
	if r.TotalTimeSeconds > 0 {
		return int(math.Round(r.TotalTimeSeconds / 60))
	}
	if s := r.PrepTimeSeconds + r.PerformTimeSeconds; s > 0 {
		return int(math.Round(s / 60))
	}
	if m := parseMinutes(r.TotalTime.String()); m > 0 {
		return m
	}
	return parseMinutes(r.PrepTime.String()) + parseMinutes(r.PerformTime.String())
}

var durationPart = regexp.MustCompile(`(\d+(?:[.,]\d+)?)\s*([a-zäöü]*)`)

// parseMinutes reads free-text or ISO 8601 durations such as "1 Stunde 30
// Minuten", "45 min", "1h30" or "PT1H30M". Unknown formats return 0.
func parseMinutes(s string) int {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0
	}
	s = strings.TrimPrefix(strings.TrimPrefix(s, "p"), "t")
	total := 0.0
	for _, m := range durationPart.FindAllStringSubmatch(s, -1) {
		v, err := strconv.ParseFloat(strings.ReplaceAll(m[1], ",", "."), 64)
		if err != nil {
			continue
		}
		switch m[2] {
		case "h", "hr", "hrs", "hour", "hours", "std", "stunde", "stunden":
			total += v * 60
		case "", "m", "min", "mins", "minute", "minutes", "minuten":
			total += v
		}
	}
	return int(math.Round(total))
}

// mapIngredient converts one Mealie ingredient. ok is false for empty lines.
func mapIngredient(in Ingredient) (model.Ingredient, bool) {
	var ing model.Ingredient
	switch {
	case in.Food != nil && strings.TrimSpace(in.Food.Name) != "":
		// Parsed in Mealie: structured quantity, unit and food.
		ing = model.Ingredient{Name: strings.TrimSpace(in.Food.Name), Quantity: in.Quantity, Unit: unitLabel(in.Unit)}
	default:
		text := in.Note.String()
		if text == "" {
			text = in.Display.String()
		}
		if text == "" {
			text = in.OriginalText.String()
		}
		if in.Quantity > 0 || in.Unit != nil {
			// Quantity and unit are structured, only the food is free text.
			ing = model.Ingredient{Name: cleanName(text), Quantity: in.Quantity, Unit: unitLabel(in.Unit)}
		} else {
			ing = parseFreeText(text)
		}
	}
	if ing.Name == "" {
		return model.Ingredient{}, false
	}
	if ing.Quantity < 0 || math.IsNaN(ing.Quantity) || math.IsInf(ing.Quantity, 0) {
		ing.Quantity = 0
	}
	if ing.Quantity > 0 && ing.Unit == "" {
		// A plain count ("2 Zwiebeln"); the AI providers use "Stück" for it.
		ing.Unit = "Stück"
	}
	ing.Pantry = IsPantry(ing.Name)
	return ing, true
}

func unitLabel(u *Unit) string {
	if u == nil {
		return ""
	}
	label := u.Abbreviation.String()
	if label == "" {
		label = strings.TrimSpace(u.Name)
	}
	return canonicalUnit(label)
}

// unitAliases maps common German and English unit spellings to the short
// German forms used elsewhere in the app (and understood by internal/pricing).
var unitAliases = map[string]string{
	"g": "g", "gr": "g", "gramm": "g", "gram": "g", "grams": "g",
	"kg": "kg", "kilo": "kg", "kilogramm": "kg", "kilogram": "kg", "kilograms": "kg",
	"mg": "mg", "milligramm": "mg",
	"ml": "ml", "milliliter": "ml", "millilitre": "ml", "milliliters": "ml",
	"cl": "cl", "zentiliter": "cl",
	"dl": "dl", "deziliter": "dl",
	"l": "l", "liter": "l", "litre": "l", "liters": "l",
	"el": "EL", "essl": "EL", "esslöffel": "EL", "tablespoon": "EL", "tablespoons": "EL", "tbsp": "EL", "tbs": "EL",
	"tl": "TL", "teel": "TL", "teelöffel": "TL", "teaspoon": "TL", "teaspoons": "TL", "tsp": "TL",
	"stück": "Stück", "stk": "Stück", "st": "Stück", "piece": "Stück", "pieces": "Stück",
	"prise": "Prise", "prisen": "Prise", "pinch": "Prise",
	"zehe": "Zehe", "zehen": "Zehe", "clove": "Zehe", "cloves": "Zehe",
	"dose": "Dose", "dosen": "Dose", "can": "Dose", "cans": "Dose",
	"bund": "Bund", "bunch": "Bund",
	"packung": "Packung", "packungen": "Packung", "pck": "Packung", "pkg": "Packung", "päckchen": "Päckchen",
	"becher": "Becher",
	"tasse":  "Tasse", "tassen": "Tasse", "cup": "Tasse", "cups": "Tasse",
	"scheibe": "Scheibe", "scheiben": "Scheibe", "slice": "Scheibe", "slices": "Scheibe",
	"zweig": "Zweig", "zweige": "Zweig", "sprig": "Zweig", "sprigs": "Zweig",
	"handvoll": "Handvoll", "handful": "Handvoll",
	"msp": "Msp.", "messerspitze": "Msp.",
	"glas": "Glas", "gläser": "Glas",
	"knolle": "Knolle", "knollen": "Knolle",
	"stange": "Stange", "stangen": "Stange",
	"blatt": "Blatt", "blätter": "Blatt",
	"würfel": "Würfel",
}

func canonicalUnit(u string) string {
	key := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(u)), ".")
	if c, ok := unitAliases[key]; ok {
		return c
	}
	return strings.TrimSpace(u)
}

// Free-text ingredient lines such as "500 g Mehl", "1 1/2 EL Öl", "½ Zwiebel,
// gewürfelt" or "2-3 Möhren".
var (
	fractionChars = map[rune]float64{'½': 0.5, '⅓': 1.0 / 3, '⅔': 2.0 / 3, '¼': 0.25, '¾': 0.75, '⅛': 0.125, '⅕': 0.2}
	numberPattern = `(?:\d+\s+\d+\s*/\s*\d+|\d+\s*/\s*\d+|\d+(?:[.,]\d+)?\s*[½⅓⅔¼¾⅛⅕]?|[½⅓⅔¼¾⅛⅕])`
	quantityRe    = regexp.MustCompile(`^(` + numberPattern + `)(?:\s*(?:-|–|bis|to)\s*(` + numberPattern + `))?`)
	unitWordRe    = regexp.MustCompile(`^\s*([\p{L}]+\.?)`)
	parensRe      = regexp.MustCompile(`\s*\([^)]*\)`)
)

// parseFreeText splits an unparsed ingredient line into quantity, unit and
// name with simple rules. It is deliberately conservative: anything it does
// not recognise stays in the name.
func parseFreeText(text string) model.Ingredient {
	s := strings.TrimSpace(text)
	var ing model.Ingredient
	if m := quantityRe.FindStringSubmatch(s); m != nil {
		q := parseNumber(m[1])
		if m[2] != "" {
			// For a range, buy enough for the upper bound.
			if hi := parseNumber(m[2]); hi > q {
				q = hi
			}
		}
		ing.Quantity = q
		s = s[len(m[0]):]
		if um := unitWordRe.FindStringSubmatch(s); um != nil {
			key := strings.TrimSuffix(strings.ToLower(um[1]), ".")
			if c, ok := unitAliases[key]; ok {
				ing.Unit = c
				s = s[len(um[0]):]
			}
		}
	}
	ing.Name = cleanName(s)
	return ing
}

func parseNumber(s string) float64 {
	s = strings.TrimSpace(s)
	total := 0.0
	// Trailing or lone unicode fraction, e.g. "1½" or "½".
	if r := []rune(s); len(r) > 0 {
		if f, ok := fractionChars[r[len(r)-1]]; ok {
			total += f
			s = strings.TrimSpace(string(r[:len(r)-1]))
		}
	}
	if s == "" {
		return total
	}
	fields := strings.Fields(strings.ReplaceAll(s, " /", "/"))
	for _, f := range fields {
		f = strings.ReplaceAll(f, " ", "")
		if num, den, ok := strings.Cut(f, "/"); ok {
			n, err1 := strconv.ParseFloat(strings.TrimSpace(num), 64)
			d, err2 := strconv.ParseFloat(strings.TrimSpace(den), 64)
			if err1 == nil && err2 == nil && d != 0 {
				total += n / d
			}
			continue
		}
		if v, err := strconv.ParseFloat(strings.ReplaceAll(f, ",", "."), 64); err == nil {
			total += v
		}
	}
	return total
}

// cleanName removes preparation notes so that the name works as a shop
// search term: "Zwiebeln (rot), fein gewürfelt" → "Zwiebeln".
func cleanName(s string) string {
	s = parensRe.ReplaceAllString(s, "")
	if before, _, ok := strings.Cut(s, ","); ok && strings.TrimSpace(before) != "" {
		s = before
	}
	s = strings.TrimFunc(s, func(r rune) bool { return unicode.IsSpace(r) || r == '-' || r == ':' })
	if r := []rune(s); len(r) > 120 {
		s = strings.TrimSpace(string(r[:120]))
	}
	return s
}

func roundQuantity(q float64) float64 {
	if q >= 10 {
		return math.Round(q)
	}
	return math.Round(q*100) / 100
}

// ---------------------------------------------------------------------------
// Staples

// pantrySuffixes are German staples that people usually have at home; a name
// matches if one of its words equals or ends with an entry ("Meersalz",
// "Olivenöl").
var pantrySuffixes = []string{"salz", "pfeffer", "öl"}

// pantryExact are words that only count as a staple on their own, because
// compounds are often specific products ("Vanillezucker", "Kokoswasser").
var pantryExact = map[string]bool{"zucker": true, "wasser": true, "salt": true, "sugar": true, "water": true}

// pantryPhrases are English staples matched against the whole name.
var pantryPhrases = map[string]bool{
	"pepper": true, "black pepper": true, "salt and pepper": true, "oil": true, "olive oil": true,
	"vegetable oil": true, "sunflower oil": true, "canola oil": true, "cooking oil": true,
}

// IsPantry reports whether an ingredient is a staple (salt, pepper, oil,
// sugar, water) that is unchecked by default in the shopping list.
func IsPantry(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if pantryPhrases[n] {
		return true
	}
	words := strings.FieldsFunc(n, func(r rune) bool { return !unicode.IsLetter(r) })
	for _, w := range words {
		if pantryExact[w] {
			return true
		}
		for _, suf := range pantrySuffixes {
			if strings.HasSuffix(w, suf) && !excludedPantry[w] {
				return true
			}
		}
	}
	return false
}

// excludedPantry are compounds that end like a staple but are not one.
var excludedPantry = map[string]bool{"sesamöl": true, "kürbiskernöl": true, "trüffelöl": true, "walnussöl": true, "leinöl": true}

// ImagePath is the app route that proxies a recipe image.
func ImagePath(r RecipeSummary) string {
	if r.Image.String() == "" || !IsUUID(r.ID) {
		return ""
	}
	return fmt.Sprintf("/api/mealie/images/%s?v=%s", r.ID, url.QueryEscape(r.Image.String()))
}

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// IsUUID reports whether s is a UUID, the format of Mealie recipe IDs.
func IsUUID(s string) bool { return uuidRe.MatchString(s) }

var slugRe = regexp.MustCompile(`^[\p{L}\p{N}_-]{1,250}$`)

// ValidSlug reports whether s looks like a Mealie recipe slug (or ID).
func ValidSlug(s string) bool { return slugRe.MatchString(s) }
