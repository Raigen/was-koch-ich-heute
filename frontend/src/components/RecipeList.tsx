import { formatEur } from "../format";
import type { Recipe } from "../types";

interface Props {
  recipes: Recipe[];
  onSelect: (r: Recipe) => void;
}

export default function RecipeList({ recipes, onSelect }: Props) {
  if (recipes.length === 0) {
    return <div className="empty">Keine Rezepte gefunden. Versuch es mit einem anderen Suchbegriff.</div>;
  }
  return (
    <section className="recipe-grid">
      {recipes.map((r) => (
        <article key={r.id} className="recipe-card">
          <h2>{r.title}</h2>
          <p className="muted">{r.description}</p>
          <ul className="facts">
            <li>👥 {r.servings} Portionen</li>
            {r.totalTimeMinutes > 0 && <li>⏱ {r.totalTimeMinutes} min</li>}
            {r.difficulty && <li>📈 {r.difficulty}</li>}
            <li>🧺 {r.ingredients.length} Zutaten</li>
            {r.estimatedCostEur > 0 && <li title="Grobe KI-Schätzung">≈ {formatEur(r.estimatedCostEur)}</li>}
          </ul>
          <div className="card-actions">
            {r.sourceUrl && (
              <a href={r.sourceUrl} target="_blank" rel="noreferrer noopener" className="link">
                Quelle: {r.sourceName || new URL(r.sourceUrl).hostname}
              </a>
            )}
            <button className="btn-primary" onClick={() => onSelect(r)}>
              Zutaten &amp; Preise
            </button>
          </div>
        </article>
      ))}
    </section>
  );
}
