// Package pricing computes package counts, costs and the price comparison
// across shops. The arithmetic deliberately happens in the backend, not in
// the AI, so that totals are correct and traceable.
package pricing

import (
	"math"
	"sort"
	"strings"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

type unitInfo struct {
	base   string
	factor float64
}

var units = map[string]unitInfo{
	"g": {"g", 1}, "gramm": {"g", 1}, "kg": {"g", 1000}, "kilogramm": {"g", 1000},
	"ml": {"ml", 1}, "milliliter": {"ml", 1}, "cl": {"ml", 10}, "l": {"ml", 1000}, "liter": {"ml", 1000},
	"el": {"ml", 15}, "tl": {"ml", 5},
	"stück": {"stk", 1}, "stk": {"stk", 1}, "stk.": {"stk", 1}, "st.": {"stk", 1}, "st": {"stk", 1}, "x": {"stk", 1},
}

// normalize converts a quantity to its base unit. ok is false if the unit is
// unknown; base is then the lower-cased unit.
func normalize(qty float64, unit string) (amount float64, base string, ok bool) {
	u := strings.ToLower(strings.TrimSpace(unit))
	if info, found := units[u]; found {
		return qty * info.factor, info.base, true
	}
	return qty, u, false
}

// PackagesNeeded computes how many packages the recipe quantity requires. If
// the units are not comparable, the AI's suggestion (fallback) is used, but
// at least 1.
func PackagesNeeded(ing model.Ingredient, packageAmount float64, packageUnit string, fallback int) int {
	if fallback < 1 {
		fallback = 1
	}
	if ing.Quantity <= 0 || packageAmount <= 0 {
		return fallback
	}
	need, nb, _ := normalize(ing.Quantity, ing.Unit)
	have, pb, _ := normalize(packageAmount, packageUnit)
	if nb == "" || nb != pb {
		return fallback
	}
	n := int(math.Ceil(need/have - 1e-9))
	if n < 1 {
		n = 1
	}
	return n
}

// Enrich fills in package counts and costs for a shop's offers.
func Enrich(sp *model.StorePrices, ingredients []model.Ingredient) {
	byName := map[string]model.Ingredient{}
	for _, in := range ingredients {
		byName[key(in.Name)] = in
	}
	for i := range sp.Offers {
		o := &sp.Offers[i]
		if in, ok := byName[key(o.Ingredient)]; ok {
			o.Ingredient = in.Name
			o.PackagesNeeded = PackagesNeeded(in, o.PackageAmount, o.PackageUnit, o.PackagesNeeded)
		} else if o.PackagesNeeded < 1 {
			o.PackagesNeeded = 1
		}
		o.Cost = round2(o.Price * float64(o.PackagesNeeded))
	}
}

// Compare builds the price comparison across all shops.
func Compare(ingredients []model.Ingredient, results []model.StorePrices) model.Comparison {
	cmp := model.Comparison{IngredientCount: len(ingredients)}
	best := map[string]*model.Offer{}

	for _, sp := range results {
		if sp.Error != "" {
			continue
		}
		// Cheapest offer per ingredient within this shop.
		cheapest := map[string]*model.Offer{}
		for i := range sp.Offers {
			o := &sp.Offers[i]
			if o.Price <= 0 {
				continue
			}
			k := key(o.Ingredient)
			if c, ok := cheapest[k]; !ok || o.Cost < c.Cost {
				cheapest[k] = o
			}
		}
		st := model.StoreTotal{StoreID: sp.StoreID, Missing: []string{}}
		for _, in := range ingredients {
			o, ok := cheapest[key(in.Name)]
			if !ok {
				st.Missing = append(st.Missing, in.Name)
				continue
			}
			st.Found++
			st.Total += o.Cost
			if b, ok := best[key(in.Name)]; !ok || o.Cost < b.Cost {
				best[key(in.Name)] = o
			}
		}
		st.Total = round2(st.Total)
		cmp.StoreTotals = append(cmp.StoreTotals, st)
	}

	// Sort: more ingredients found first, then cheaper.
	sort.SliceStable(cmp.StoreTotals, func(i, j int) bool {
		a, b := cmp.StoreTotals[i], cmp.StoreTotals[j]
		if a.Found != b.Found {
			return a.Found > b.Found
		}
		return a.Total < b.Total
	})
	if len(cmp.StoreTotals) > 0 && cmp.StoreTotals[0].Found > 0 {
		cmp.CheapestStoreID = cmp.StoreTotals[0].StoreID
	}

	cmp.MixMissing = []string{}
	for _, in := range ingredients {
		o := best[key(in.Name)]
		cmp.Mix = append(cmp.Mix, model.MixItem{Ingredient: in.Name, Offer: o})
		if o == nil {
			cmp.MixMissing = append(cmp.MixMissing, in.Name)
		} else {
			cmp.MixTotal += o.Cost
		}
	}
	cmp.MixTotal = round2(cmp.MixTotal)
	return cmp
}

func key(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

func round2(f float64) float64 { return math.Round(f*100) / 100 }
