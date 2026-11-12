package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
	"github.com/raigen/was-koch-ich-heute/backend/internal/research"
)

func newTestServer() *httptest.Server {
	d := research.NewDemo()
	d.Delay = 0
	return httptest.NewServer(New(d, Options{}).Handler())
}

func TestRecipeSearch(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/recipes/search", "application/json", strings.NewReader(`{"query":"vegan curry","servings":2}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct{ Recipes []model.Recipe }
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if len(out.Recipes) == 0 || out.Recipes[0].ID != "demo-dal" || out.Recipes[0].Servings != 2 {
		t.Fatalf("unerwartete Rezepte: %+v", out.Recipes)
	}

	resp2, _ := http.Post(ts.URL+"/api/recipes/search", "application/json", strings.NewReader(`{"query":"  "}`))
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("leere Suche: Status %d, erwartet 400", resp2.StatusCode)
	}
}

func TestPriceCompareStream(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()

	body := `{"ingredients":[{"name":"Spaghetti","quantity":500,"unit":"g"},{"name":"Zwiebel","quantity":2,"unit":"Stück"}],"storeIds":["rewe","aldi-sued","gibtsnicht"],"postalCode":"10115"}`
	resp, err := http.Post(ts.URL+"/api/prices/compare", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var stores, summaries int
	var last priceEvent
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		var ev priceEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatalf("ungültige NDJSON-Zeile %q: %v", sc.Text(), err)
		}
		switch ev.Type {
		case "store":
			stores++
		case "summary":
			summaries++
		}
		last = ev
	}
	if stores != 2 || summaries != 1 || last.Type != "summary" {
		t.Fatalf("stores=%d summaries=%d last=%s", stores, summaries, last.Type)
	}
	if len(last.Comparison.StoreTotals) != 2 || last.Comparison.MixTotal <= 0 {
		t.Errorf("unerwarteter Vergleich: %+v", last.Comparison)
	}
}
