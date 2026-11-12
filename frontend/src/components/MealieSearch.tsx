import type { FormEvent } from "react";

interface Props {
  query: string;
  onQueryChange: (q: string) => void;
  servings: number;
  onServingsChange: (n: number) => void;
  onSubmit: () => void;
  loading: boolean;
}

/** Search form for recipes from the user's own Mealie instance. */
export default function MealieSearch({ query, onQueryChange, servings, onServingsChange, onSubmit, loading }: Props) {
  function submit(e: FormEvent) {
    e.preventDefault();
    onSubmit();
  }

  return (
    <form onSubmit={submit} className="search-form">
      <div className="search-main">
        <input
          className="search-input"
          placeholder="Rezept in Mealie suchen, z. B. „Linsen“ (leer = alle)"
          value={query}
          maxLength={100}
          onChange={(e) => onQueryChange(e.target.value)}
          aria-label="Mealie-Suche"
        />
        <button type="submit" className="btn-primary" disabled={loading}>
          {loading ? "Suche …" : "In Mealie suchen"}
        </button>
      </div>
      <div className="row">
        <label className="field">
          <span>Portionen</span>
          <input
            type="number"
            min={1}
            max={20}
            value={servings}
            onChange={(e) => onServingsChange(Math.min(20, Math.max(1, Number(e.target.value) || 1)))}
          />
        </label>
        <p className="muted small mealie-hint">Mengen werden beim Öffnen eines Rezepts auf die Portionen umgerechnet.</p>
      </div>
    </form>
  );
}
