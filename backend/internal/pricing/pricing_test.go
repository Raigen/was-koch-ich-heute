package pricing

import (
	"testing"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

func TestPackagesNeeded(t *testing.T) {
	cases := []struct {
		ing      model.Ingredient
		amount   float64
		unit     string
		fallback int
		want     int
	}{
		{model.Ingredient{Quantity: 750, Unit: "g"}, 500, "g", 0, 2},
		{model.Ingredient{Quantity: 1.5, Unit: "kg"}, 1000, "g", 0, 2},
		{model.Ingredient{Quantity: 500, Unit: "ml"}, 1, "l", 0, 1},
		{model.Ingredient{Quantity: 4, Unit: "Stück"}, 6, "Stk", 0, 1},
		{model.Ingredient{Quantity: 2, Unit: "Dose"}, 1, "Dose", 0, 2},
		{model.Ingredient{Quantity: 2, Unit: "Zehe"}, 3, "Stück", 3, 3}, // unvergleichbar -> KI-Wert
		{model.Ingredient{Quantity: 0, Unit: ""}, 500, "g", 0, 1},
	}
	for _, c := range cases {
		if got := PackagesNeeded(c.ing, c.amount, c.unit, c.fallback); got != c.want {
			t.Errorf("PackagesNeeded(%v, %v %s) = %d, want %d", c.ing, c.amount, c.unit, got, c.want)
		}
	}
}

func TestCompare(t *testing.T) {
	ings := []model.Ingredient{{Name: "Nudeln", Quantity: 500, Unit: "g"}, {Name: "Tomaten", Quantity: 800, Unit: "g"}}
	a := model.StorePrices{StoreID: "a", Offers: []model.Offer{
		{Ingredient: "nudeln", Price: 1.00, PackageAmount: 500, PackageUnit: "g"},
		{Ingredient: "Tomaten", Price: 0.50, PackageAmount: 400, PackageUnit: "g"},
		{Ingredient: "Tomaten", Price: 0.90, PackageAmount: 1, PackageUnit: "kg"},
	}}
	b := model.StorePrices{StoreID: "b", Offers: []model.Offer{
		{Ingredient: "Nudeln", Price: 0.80, PackageAmount: 500, PackageUnit: "g"},
	}}
	c := model.StorePrices{StoreID: "c", Error: "kaputt"}
	for _, sp := range []*model.StorePrices{&a, &b, &c} {
		Enrich(sp, ings)
	}
	cmp := Compare(ings, []model.StorePrices{a, b, c})

	if cmp.CheapestStoreID != "a" {
		t.Fatalf("CheapestStoreID = %q, want a", cmp.CheapestStoreID)
	}
	if got := cmp.StoreTotals[0].Total; got != 1.90 { // 1.00 + 0.90 (1 kg schlägt 2×400 g)
		t.Errorf("Total a = %v, want 1.90", got)
	}
	if cmp.MixTotal != 1.70 { // 0.80 bei b + 0.90 bei a
		t.Errorf("MixTotal = %v, want 1.70", cmp.MixTotal)
	}
	if len(cmp.StoreTotals) != 2 || len(cmp.StoreTotals[1].Missing) != 1 {
		t.Errorf("unerwartete StoreTotals: %+v", cmp.StoreTotals)
	}
}
