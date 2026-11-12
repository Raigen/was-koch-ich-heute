import type { MealieRecipePage, MealieRecipeSummary } from "../types";

interface Props {
  result: MealieRecipePage;
  onSelect: (r: MealieRecipeSummary) => void;
  onMore: () => void;
  loadingMore: boolean;
  openingSlug: string | null;
}

/** Recipe cards from a Mealie search, with "load more" pagination. */
export default function MealieList({ result, onSelect, onMore, loadingMore, openingSlug }: Props) {
  if (result.recipes.length === 0) {
    return <div className="empty">Keine Rezepte in Mealie gefunden. Versuch es mit einem anderen Suchbegriff.</div>;
  }
  return (
    <>
      <p className="muted small result-count">
        {result.total} {result.total === 1 ? "Rezept" : "Rezepte"} in Mealie
      </p>
      <section className="recipe-grid">
        {result.recipes.map((r) => (
          <article key={r.slug} className="recipe-card">
            {r.imageUrl ? (
              <img
                className="recipe-image"
                src={r.imageUrl}
                alt=""
                loading="lazy"
                onError={(e) => (e.currentTarget.style.visibility = "hidden")}
              />
            ) : (
              <div className="recipe-image placeholder" aria-hidden="true">
                🍲
              </div>
            )}
            <h2>{r.title}</h2>
            <p className="muted">{r.description}</p>
            <ul className="facts">
              {r.servings > 0 && <li>👥 {r.servings} Portionen</li>}
              {r.totalTimeMinutes > 0 && <li>⏱ {r.totalTimeMinutes} min</li>}
            </ul>
            <div className="card-actions">
              <span className="tag">Mealie</span>
              <button className="btn-primary" onClick={() => onSelect(r)} disabled={openingSlug !== null}>
                {openingSlug === r.slug ? "Lädt …" : "Zutaten & Preise"}
              </button>
            </div>
          </article>
        ))}
      </section>
      {result.page < result.totalPages && (
        <div className="more">
          <button className="btn-secondary" onClick={onMore} disabled={loadingMore}>
            {loadingMore ? "Lädt …" : "Mehr Rezepte laden"}
          </button>
        </div>
      )}
    </>
  );
}
