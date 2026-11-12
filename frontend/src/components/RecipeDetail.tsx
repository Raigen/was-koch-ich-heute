import { useRef, useState } from "react";
import { shoppingItemKey, type Favorite, type FavoriteChanges, type SavedPriceCheck } from "../favorites";
import { formatQty } from "../format";
import type { Comparison, Recipe, Store, StorePrices } from "../types";
import FavoriteButton from "./FavoriteButton";
import PriceComparison from "./PriceComparison";
import ShoppingList from "./ShoppingList";

interface Props {
  recipe: Recipe;
  stores: Store[];
  allStores: Store[];
  postalCode: string;
  /** The saved entry of this recipe, if it is on the Merkliste. */
  favorite?: Favorite;
  /** Starts a price comparison right away (used by "Preise neu vergleichen" in the Merkliste). */
  autoCompare?: boolean;
  backLabel: string;
  onBack: () => void;
  onSave: (recipe: Recipe, priceCheck: SavedPriceCheck | null) => void;
  onRemove: (id: string) => void;
  onUpdate: (id: string, changes: FavoriteChanges) => void;
}

export default function RecipeDetail({
  recipe,
  stores,
  allStores,
  postalCode,
  favorite,
  autoCompare,
  backLabel,
  onBack,
  onSave,
  onRemove,
  onUpdate,
}: Props) {
  const saved = favorite?.priceCheck ?? null;
  // Staples (salt, oil …) are unchecked by default. For a saved comparison, the
  // ingredients that were compared back then are checked.
  const [buy, setBuy] = useState<boolean[]>(() => {
    const names = saved && new Set(saved.ingredients.map((i) => i.name));
    return recipe.ingredients.map((i) => (names ? names.has(i.name) : !i.pantry));
  });
  const [run, setRun] = useState(autoCompare ? 1 : 0);
  // The latest comparison of this view, kept so that saving afterwards includes it.
  const [lastCheck, setLastCheck] = useState<SavedPriceCheck | null>(null);
  const [note, setNote] = useState(favorite?.note ?? "");
  // A comparison with failed shops that was not stored (see handleComplete).
  const [incomplete, setIncomplete] = useState(false);
  // Follow the stored note when the entry changes (e.g. un-starred and starred again).
  const [noteFor, setNoteFor] = useState(favorite?.id);
  if (noteFor !== favorite?.id) {
    setNoteFor(favorite?.id);
    setNote(favorite?.note ?? "");
  }
  const toBuy = recipe.ingredients.filter((_, i) => buy[i]);
  // What the running comparison was started with (the inputs may change while it runs).
  const runInput = useRef({ ingredients: toBuy, storeIds: stores.map((s) => s.id), postalCode });

  function startComparison() {
    runInput.current = { ingredients: toBuy, storeIds: stores.map((s) => s.id), postalCode };
    setRun((r) => r + 1);
  }

  function handleComplete(results: StorePrices[], comparison: Comparison) {
    // Only a complete comparison is stored: a failed or partial one (timeouts, network
    // trouble in the supermarket) must not replace a good stored list and its ticks.
    // After "Fehlgeschlagene erneut versuchen" succeeds, this runs again and stores it.
    const complete = results.every((r) => !r.error) && comparison.mix.some((m) => m.offer);
    setIncomplete(!complete);
    if (!complete) return;
    // Keep ticks for items that are still bought in the same shop.
    const inMix = new Set(comparison.mix.flatMap((m) => (m.offer ? [shoppingItemKey(m.offer.storeId, m.offer.ingredient)] : [])));
    const check: SavedPriceCheck = {
      comparedAt: new Date().toISOString(),
      ...runInput.current,
      results,
      comparison,
      checked: (saved?.checked ?? []).filter((k) => inMix.has(k)),
    };
    setLastCheck(check);
    if (favorite) onUpdate(favorite.id, { priceCheck: check });
  }

  function toggleFavorite() {
    if (favorite) onRemove(favorite.id);
    else onSave(recipe, lastCheck);
  }

  return (
    <section className="detail">
      <button className="btn-link" onClick={onBack}>
        ← {backLabel}
      </button>
      <div className="detail-head">
        <div>
          <h2>{recipe.title}</h2>
          <p className="muted">{recipe.description}</p>
        </div>
        <div className="detail-actions">
          <FavoriteButton title={recipe.title} saved={!!favorite} onClick={toggleFavorite} />
          {recipe.sourceUrl && (
            <a href={recipe.sourceUrl} target="_blank" rel="noreferrer noopener" className="link">
              Originalrezept öffnen ↗
            </a>
          )}
        </div>
      </div>

      {favorite && (
        <label className="field note">
          <span>Notiz</span>
          <textarea
            rows={2}
            maxLength={1000}
            placeholder="z. B. „mit Feta statt Hack“ oder „Kinder fanden’s super“"
            value={note}
            onChange={(e) => setNote(e.target.value)}
            onBlur={() => note !== favorite.note && onUpdate(favorite.id, { note })}
          />
        </label>
      )}

      <div className="detail-grid">
        <div className="panel">
          <h3>Zutaten für {recipe.servings} Portionen</h3>
          <p className="muted small">Haken entfernen, was du schon zuhause hast.</p>
          <ul className="ingredients">
            {recipe.ingredients.map((ing, i) => (
              <li key={i}>
                <label>
                  <input
                    type="checkbox"
                    checked={buy[i]}
                    onChange={() => setBuy((b) => b.map((v, j) => (j === i ? !v : v)))}
                  />
                  <span className={buy[i] ? "" : "struck"}>{ing.name}</span>
                  <span className="qty">{formatQty(ing.quantity, ing.unit)}</span>
                </label>
              </li>
            ))}
          </ul>
          <button
            className="btn-primary wide"
            disabled={toBuy.length === 0 || stores.length === 0}
            onClick={startComparison}
          >
            {run || saved ? "Preise neu vergleichen" : `Preise in ${stores.length} Shops vergleichen`}
          </button>
          {stores.length === 0 && <p className="error small">Bitte oben mindestens einen Supermarkt auswählen.</p>}
        </div>

        {recipe.steps?.length > 0 && (
          <div className="panel">
            <h3>Zubereitung</h3>
            <ol className="steps">
              {recipe.steps.map((s, i) => (
                <li key={i}>{s}</li>
              ))}
            </ol>
          </div>
        )}
      </div>

      {favorite && saved && saved.comparison.mix.some((m) => m.offer) && (
        <ShoppingList
          key={saved.comparedAt}
          check={saved}
          allStores={allStores}
          onCheckedChange={(checked) => onUpdate(favorite.id, { priceCheck: { ...saved, checked } })}
        />
      )}

      {incomplete && favorite && (
        <div className="notice incomplete">
          Der Preisvergleich ist unvollständig und wurde nicht gespeichert.
          {saved && " Die gespeicherte Einkaufsliste bleibt erhalten."}
        </div>
      )}

      {run > 0 ? (
        <PriceComparison
          key={run}
          ingredients={toBuy}
          stores={stores}
          allStores={allStores}
          postalCode={postalCode}
          onComplete={handleComplete}
        />
      ) : (
        saved && (
          <PriceComparison
            key="saved"
            ingredients={saved.ingredients}
            stores={allStores.filter((s) => saved.storeIds.includes(s.id))}
            allStores={allStores}
            postalCode={saved.postalCode}
            saved={saved}
          />
        )
      )}
    </section>
  );
}
