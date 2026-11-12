package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raigen/was-koch-ich-heute/backend/internal/mealie"
	"github.com/raigen/was-koch-ich-heute/backend/internal/mealie/mealietest"
	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
	"github.com/raigen/was-koch-ich-heute/backend/internal/research"
)

func newMealieTestServer(t *testing.T, mealieURL, token string) *httptest.Server {
	t.Helper()
	c, err := mealie.New(mealie.Config{BaseURL: mealieURL, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	d := research.NewDemo()
	d.Delay = 0
	ts := httptest.NewServer(New(d, Options{Mealie: c}).Handler())
	t.Cleanup(ts.Close)
	return ts
}

func getJSON(t *testing.T, url string, out any) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("GET %s: invalid JSON: %v", url, err)
	}
	return resp.StatusCode
}

func TestConfigReportsMealie(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()
	var cfg struct{ Mealie bool }
	getJSON(t, ts.URL+"/api/config", &cfg)
	if cfg.Mealie {
		t.Error("mealie must be false without configuration")
	}
	var e struct{ Error string }
	if code := getJSON(t, ts.URL+"/api/mealie/recipes", &e); code != http.StatusNotFound || e.Error == "" {
		t.Errorf("unconfigured search: %d %q", code, e.Error)
	}

	fake := mealietest.New()
	defer fake.Close()
	ms := newMealieTestServer(t, fake.URL, mealietest.Token)
	getJSON(t, ms.URL+"/api/config", &cfg)
	if !cfg.Mealie {
		t.Error("mealie must be true when configured")
	}
}

func TestMealieSearchAndRecipe(t *testing.T) {
	fake := mealietest.New()
	defer fake.Close()
	ts := newMealieTestServer(t, fake.URL, mealietest.Token)

	var page model.MealieRecipePage
	if code := getJSON(t, ts.URL+"/api/mealie/recipes?query=Dal", &page); code != http.StatusOK {
		t.Fatalf("search status %d", code)
	}
	if len(page.Recipes) != 1 || page.Recipes[0].Slug != "linsen-dal" || page.Recipes[0].ImageURL == "" {
		t.Fatalf("unexpected page: %+v", page)
	}

	var r model.Recipe
	if code := getJSON(t, ts.URL+"/api/mealie/recipes/linsen-dal?servings=4", &r); code != http.StatusOK {
		t.Fatalf("recipe status %d", code)
	}
	if r.Servings != 4 || r.Ingredients[0].Quantity != 400 || r.MealieURL != fake.URL+"/g/home/r/linsen-dal" {
		t.Errorf("unexpected recipe: %+v", r)
	}

	// The image is proxied without exposing the token to the browser.
	resp, err := http.Get(ts.URL + r.ImageURL)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "image/webp" || string(body) != string(mealietest.ImageBytes) {
		t.Errorf("image: %d %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}
}

func TestMealieInputValidation(t *testing.T) {
	fake := mealietest.New()
	defer fake.Close()
	ts := newMealieTestServer(t, fake.URL, mealietest.Token)

	cases := []struct {
		path string
		want int
	}{
		{"/api/mealie/recipes?query=" + strings.Repeat("x", 101), http.StatusBadRequest},
		{"/api/mealie/recipes?page=0", http.StatusBadRequest},
		{"/api/mealie/recipes?page=abc", http.StatusBadRequest},
		{"/api/mealie/recipes/linsen-dal?servings=99", http.StatusBadRequest},
		{"/api/mealie/recipes/a%20b", http.StatusBadRequest},
		{"/api/mealie/recipes/gibt-es-nicht", http.StatusNotFound},
		{"/api/mealie/images/not-a-uuid", http.StatusBadRequest},
		{"/api/mealie/images/00000000-0000-0000-0000-000000000000", http.StatusNotFound},
	}
	for _, c := range cases {
		resp, err := http.Get(ts.URL + c.path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("GET %s: status %d, want %d", c.path, resp.StatusCode, c.want)
		}
	}
}

func TestMealieErrorMessages(t *testing.T) {
	fake := mealietest.New()
	defer fake.Close()

	var e struct{ Error string }
	ts := newMealieTestServer(t, fake.URL, "wrong-token")
	if code := getJSON(t, ts.URL+"/api/mealie/recipes", &e); code != http.StatusBadGateway || !strings.Contains(e.Error, "API-Token") {
		t.Errorf("invalid token: %d %q", code, e.Error)
	}
	if strings.Contains(e.Error, "wrong-token") {
		t.Error("error message leaks the token")
	}

	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	ts = newMealieTestServer(t, closed.URL, mealietest.Token)
	if code := getJSON(t, ts.URL+"/api/mealie/recipes", &e); code != http.StatusBadGateway || !strings.Contains(e.Error, "nicht erreichbar") {
		t.Errorf("unreachable: %d %q", code, e.Error)
	}
}
