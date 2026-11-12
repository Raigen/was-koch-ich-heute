package mealie

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/raigen/was-koch-ich-heute/backend/internal/mealie/mealietest"
	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

func newClient(t *testing.T, baseURL, token string) *Client {
	t.Helper()
	c, err := New(Config{BaseURL: baseURL, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewValidatesConfig(t *testing.T) {
	cases := []Config{
		{BaseURL: "mealie.local:9000", Token: "x"},
		{BaseURL: "ftp://mealie.local", Token: "x"},
		{BaseURL: "http://user:pw@mealie.local", Token: "x"},
		{BaseURL: "http://mealie.local", Token: " "},
		{BaseURL: "http://mealie.local", Token: "x", PublicURL: "/relative"},
	}
	for _, cfg := range cases {
		if _, err := New(cfg); err == nil {
			t.Errorf("New(%+v): expected error", cfg)
		} else if strings.Contains(err.Error(), "pw") {
			t.Errorf("error leaks credentials: %v", err)
		}
	}
	c, err := New(Config{BaseURL: "http://mealie:9000/", PublicURL: "https://mealie.example.org/", Token: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.RecipeURL("home", "linsen-dal"); got != "https://mealie.example.org/g/home/r/linsen-dal" {
		t.Errorf("RecipeURL = %q", got)
	}
}

func TestSearch(t *testing.T) {
	fake := mealietest.New()
	defer fake.Close()
	c := newClient(t, fake.URL, mealietest.Token)

	p, err := c.Search(context.Background(), "dal", 2, 24)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 || p.Items[0].Slug != "linsen-dal" || p.TotalPages != 1 {
		t.Fatalf("unexpected page: %+v", p)
	}
	q := fake.Queries()[0]
	if q.Get("search") != "dal" || q.Get("page") != "2" || q.Get("perPage") != "24" || q.Get("orderBy") != "" {
		t.Errorf("unexpected query: %v", q)
	}

	// Without a search term, all recipes are listed by name.
	p, err = c.Search(context.Background(), "", 1, 24)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 2 {
		t.Fatalf("want 2 recipes, got %d", len(p.Items))
	}
	if q := fake.Queries()[1]; q.Get("orderBy") != "name" || q.Has("search") {
		t.Errorf("unexpected query: %v", q)
	}

	s := Summary(p.Items[1])
	want := model.MealieRecipeSummary{Slug: "omas-kartoffelsuppe", Title: "Omas Kartoffelsuppe", Servings: 6, TotalTimeMinutes: 75}
	if s != want {
		t.Errorf("Summary = %+v, want %+v", s, want)
	}
	if ImagePath(p.Items[1]) != "" {
		t.Error("recipe without image must not get an image URL")
	}
	if got := ImagePath(p.Items[0]); got != "/api/mealie/images/"+mealietest.RecipeID+"?v=Xy7a" {
		t.Errorf("ImagePath = %q", got)
	}
}

func TestRecipeMappingAndScaling(t *testing.T) {
	fake := mealietest.New()
	defer fake.Close()
	c := newClient(t, fake.URL, mealietest.Token)

	mr, err := c.Recipe(context.Background(), "linsen-dal")
	if err != nil {
		t.Fatal(err)
	}
	r := ToRecipe(mr, 4) // the recipe is for 2 servings

	if r.ID != "mealie-linsen-dal" || r.Title != "Linsen-Dal" || r.Servings != 4 || r.TotalTimeMinutes != 45 {
		t.Errorf("unexpected recipe header: %+v", r)
	}
	if r.SourceURL != "https://www.example.org/rezepte/dal" || r.SourceName != "example.org" {
		t.Errorf("source = %q / %q", r.SourceURL, r.SourceName)
	}
	want := []model.Ingredient{
		{Name: "Rote Linsen", Quantity: 400, Unit: "g"},
		{Name: "Olivenöl", Quantity: 2, Unit: "EL", Pantry: true},
		{Name: "Kokosmilch", Quantity: 3, Unit: "Dose"},
		{Name: "Zwiebel", Quantity: 1, Unit: "Stück"},
		{Name: "Salz und Pfeffer", Quantity: 0, Unit: "", Pantry: true},
	}
	if !reflect.DeepEqual(r.Ingredients, want) {
		t.Errorf("ingredients:\n got %+v\nwant %+v", r.Ingredients, want)
	}
	wantSteps := []string{"Zwiebel in Öl anschwitzen.", "Kochen: Linsen und Kokosmilch zugeben, 20 Minuten köcheln."}
	if !reflect.DeepEqual(r.Steps, wantSteps) {
		t.Errorf("steps = %q", r.Steps)
	}

	// Without requested servings the original quantities are kept.
	if r := ToRecipe(mr, 0); r.Servings != 2 || r.Ingredients[0].Quantity != 200 {
		t.Errorf("unscaled recipe: servings %d, first quantity %v", r.Servings, r.Ingredients[0].Quantity)
	}
	// Unknown base servings: no scaling, servings 0.
	mr.RecipeServings = 0
	if r := ToRecipe(mr, 4); r.Servings != 0 || r.Ingredients[0].Quantity != 200 {
		t.Errorf("unknown servings: servings %d, first quantity %v", r.Servings, r.Ingredients[0].Quantity)
	}
}

func TestErrors(t *testing.T) {
	fake := mealietest.New()
	defer fake.Close()

	bad := newClient(t, fake.URL, "wrong-token")
	if _, err := bad.Search(context.Background(), "", 1, 10); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("wrong token: got %v, want ErrUnauthorized", err)
	}
	c := newClient(t, fake.URL, mealietest.Token)
	if _, err := c.Recipe(context.Background(), "gibt-es-nicht"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing recipe: got %v, want ErrNotFound", err)
	}

	forbidden := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer forbidden.Close()
	if _, err := newClient(t, forbidden.URL, "x").Search(context.Background(), "", 1, 10); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("403: got %v, want ErrUnauthorized", err)
	}

	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer broken.Close()
	var se *StatusError
	if _, err := newClient(t, broken.URL, "x").Search(context.Background(), "", 1, 10); !errors.As(err, &se) || se.Code != 500 {
		t.Errorf("500: got %v", err)
	}

	// A closed server is unreachable; the error must not contain the token.
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	_, err := newClient(t, closedURL, "secret-token").Search(context.Background(), "", 1, 10)
	if !errors.Is(err, ErrUnreachable) {
		t.Errorf("closed server: got %v, want ErrUnreachable", err)
	}
	if err != nil && strings.Contains(err.Error(), "secret-token") {
		t.Errorf("error leaks token: %v", err)
	}
}

func TestGroupSlugAndImage(t *testing.T) {
	fake := mealietest.New()
	defer fake.Close()
	c := newClient(t, fake.URL, mealietest.Token)

	g, err := c.GroupSlug(context.Background())
	if err != nil || g != mealietest.GroupSlug {
		t.Fatalf("GroupSlug = %q, %v", g, err)
	}
	if got := c.RecipeURL(g, "linsen-dal"); got != fake.URL+"/g/home/r/linsen-dal" {
		t.Errorf("RecipeURL = %q", got)
	}

	img, err := c.Image(context.Background(), mealietest.RecipeID)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Body.Close()
	b, _ := io.ReadAll(img.Body)
	if img.ContentType != "image/webp" || string(b) != string(mealietest.ImageBytes) {
		t.Errorf("image: %q %q", img.ContentType, b)
	}
}

func TestParseFreeText(t *testing.T) {
	cases := []struct {
		in   string
		want model.Ingredient
	}{
		{"500 g Mehl", model.Ingredient{Name: "Mehl", Quantity: 500, Unit: "g"}},
		{"500g Hackfleisch (gemischt)", model.Ingredient{Name: "Hackfleisch", Quantity: 500, Unit: "g"}},
		{"1,5 l Gemüsebrühe", model.Ingredient{Name: "Gemüsebrühe", Quantity: 1.5, Unit: "l"}},
		{"2 EL Tomatenmark", model.Ingredient{Name: "Tomatenmark", Quantity: 2, Unit: "EL"}},
		{"1½ TL Kreuzkümmel", model.Ingredient{Name: "Kreuzkümmel", Quantity: 1.5, Unit: "TL"}},
		{"2-3 Möhren", model.Ingredient{Name: "Möhren", Quantity: 3}},
		{"3 Zehen Knoblauch", model.Ingredient{Name: "Knoblauch", Quantity: 3, Unit: "Zehe"}},
		{"2 tbsp olive oil", model.Ingredient{Name: "olive oil", Quantity: 2, Unit: "EL"}},
		{"1/4 Bund Petersilie", model.Ingredient{Name: "Petersilie", Quantity: 0.25, Unit: "Bund"}},
		{"Eier", model.Ingredient{Name: "Eier"}},
		{"Große Zwiebel", model.Ingredient{Name: "Große Zwiebel"}},
	}
	for _, c := range cases {
		if got := parseFreeText(c.in); got != c.want {
			t.Errorf("parseFreeText(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestIsPantry(t *testing.T) {
	yes := []string{"Salz", "Meersalz", "Pfeffer", "Salz und Pfeffer", "Olivenöl", "Öl", "Zucker", "Wasser", "salt", "black pepper", "olive oil"}
	no := []string{"Paprika", "Pfefferschote", "Vanillezucker", "Kokoswasser", "Sesamöl", "red bell pepper", "Rote Linsen"}
	for _, n := range yes {
		if !IsPantry(n) {
			t.Errorf("IsPantry(%q) = false, want true", n)
		}
	}
	for _, n := range no {
		if IsPantry(n) {
			t.Errorf("IsPantry(%q) = true, want false", n)
		}
	}
}

func TestParseMinutes(t *testing.T) {
	cases := map[string]int{"45 Minuten": 45, "1 Stunde 30 Minuten": 90, "PT1H30M": 90, "1h30": 90, "20 min": 20, "": 0, "über Nacht": 0, "1.5 hours": 90}
	for in, want := range cases {
		if got := parseMinutes(in); got != want {
			t.Errorf("parseMinutes(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestValidSlug(t *testing.T) {
	for _, s := range []string{"linsen-dal", "omas_kartoffelsuppe", mealietest.RecipeID} {
		if !ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = false", s)
		}
	}
	for _, s := range []string{"", "../admin", "a/b", "a b", "a?b", strings.Repeat("a", 251)} {
		if ValidSlug(s) {
			t.Errorf("ValidSlug(%q) = true", s)
		}
	}
}
