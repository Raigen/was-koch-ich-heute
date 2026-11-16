import type { Favorite } from "../favorites";
import { formatDateTime, formatEur } from "../format";
import type { Store } from "../types";

interface Props {
  favorites: Favorite[];
  allStores: Store[];
  canCompare: boolean;
  onOpen: (f: Favorite) => void;
  onCompare: (f: Favorite) => void;
  onRemove: (f: Favorite) => void;
}

export default function FavoritesList({ favorites, allStores, canCompare, onOpen, onCompare, onRemove }: Props) {
  if (favorites.length === 0) {
    return (
      <div className="empty">
        Noch nichts gemerkt. Such ein Rezept und tippe auf „☆ Merken“, um es hier zu speichern.
      </div>
    );
  }
  const storeName = (id: string) => allStores.find((s) => s.id === id)?.name ?? id;

  return (
    <section className="recipe-grid">
      {favorites.map((f) => {
        const pc = f.priceCheck;
        const items = pc ? pc.comparison.mix.filter((m) => m.offer).length : 0;
        return (
          <article key={f.id} className="recipe-card favorite-card">
            <h2>{f.recipe.title}</h2>
            <p className="muted">{f.recipe.description}</p>
            {f.note && <p className="fav-note">„{f.note}“</p>}
            <div className="fav-price">
              {pc ? (
                <>
                  <div>
                    Günstigster Mix: <strong>{formatEur(pc.comparison.mixTotal)}</strong>
                  </div>
                  <div className="muted small">
                    Preisstand {formatDateTime(pc.comparedAt)} · {pc.storeIds.map(storeName).join(", ")}
                  </div>
                  {items > 0 && (
                    <div className="muted small">
                      Einkaufsliste: {Math.min(pc.checked.length, items)}/{items} erledigt
                    </div>
                  )}
                </>
              ) : (
                <span className="muted small">Noch kein Preisvergleich gespeichert.</span>
              )}
            </div>
            <div className="card-actions">
              <button
                className="btn-secondary"
                disabled={!canCompare}
                title={canCompare ? undefined : "Bitte oben mindestens einen Supermarkt auswählen."}
                onClick={() => onCompare(f)}
              >
                Preise neu vergleichen
              </button>
              <button className="btn-primary" onClick={() => onOpen(f)}>
                Öffnen
              </button>
            </div>
            <div className="fav-foot muted small">
              <span>Gemerkt am {formatDateTime(f.savedAt)}</span>
              <button className="btn-link" onClick={() => onRemove(f)}>
                Entfernen
              </button>
            </div>
          </article>
        );
      })}
    </section>
  );
}
