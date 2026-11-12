import { useState } from "react";
import { formatQty } from "../format";
import type { Recipe, Store } from "../types";
import PriceComparison from "./PriceComparison";

interface Props {
  recipe: Recipe;
  stores: Store[];
  allStores: Store[];
  postalCode: string;
  onBack: () => void;
}

export default function RecipeDetail({ recipe, stores, allStores, postalCode, onBack }: Props) {
  // Grundzutaten (Salz, Öl …) sind standardmäßig abgewählt.
  const [buy, setBuy] = useState<boolean[]>(() => recipe.ingredients.map((i) => !i.pantry));
  const [run, setRun] = useState(0);
  const toBuy = recipe.ingredients.filter((_, i) => buy[i]);

  return (
    <section className="detail">
      <button className="btn-link" onClick={onBack}>
        ← zurück zu den Ergebnissen
      </button>
      <div className="detail-head">
        <div>
          <h2>{recipe.title}</h2>
          <p className="muted">{recipe.description}</p>
        </div>
        {recipe.sourceUrl && (
          <a href={recipe.sourceUrl} target="_blank" rel="noreferrer noopener" className="link">
            Originalrezept öffnen ↗
          </a>
        )}
      </div>

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
            onClick={() => setRun((r) => r + 1)}
          >
            {run ? "Preise neu vergleichen" : `Preise in ${stores.length} Shops vergleichen`}
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

      {run > 0 && (
        <PriceComparison key={run} ingredients={toBuy} stores={stores} allStores={allStores} postalCode={postalCode} />
      )}
    </section>
  );
}
