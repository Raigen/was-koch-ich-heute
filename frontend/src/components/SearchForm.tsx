import type { FormEvent } from "react";
import type { RecipeQuery } from "../types";

const DIETS = ["", "vegetarisch", "vegan", "glutenfrei", "laktosefrei", "low carb", "high protein"];
const SUGGESTIONS = ["Schnelle Pasta", "Vegetarisches Curry", "Eintopf für den Winter", "Ofengemüse", "Kartoffelsuppe"];

interface Props {
  value: RecipeQuery;
  onChange: (q: RecipeQuery) => void;
  onSubmit: (q: RecipeQuery) => void;
  loading: boolean;
}

export default function SearchForm({ value, onChange, onSubmit, loading }: Props) {
  const set = <K extends keyof RecipeQuery>(k: K, v: RecipeQuery[K]) => onChange({ ...value, [k]: v });

  function submit(e: FormEvent) {
    e.preventDefault();
    if (value.query.trim()) onSubmit(value);
  }

  return (
    <form onSubmit={submit} className="search-form">
      <div className="search-main">
        <input
          className="search-input"
          placeholder="Worauf hast du Lust? z. B. „günstiges Chili für 4 Personen“"
          value={value.query}
          onChange={(e) => set("query", e.target.value)}
          aria-label="Rezeptsuche"
        />
        <button type="submit" className="btn-primary" disabled={loading || !value.query.trim()}>
          {loading ? "Suche …" : "Rezepte suchen"}
        </button>
      </div>
      <div className="suggestions">
        {SUGGESTIONS.map((s) => (
          <button type="button" key={s} className="chip" onClick={() => set("query", s)}>
            {s}
          </button>
        ))}
      </div>
      <div className="row">
        <label className="field">
          <span>Portionen</span>
          <input
            type="number"
            min={1}
            max={20}
            value={value.servings}
            onChange={(e) => set("servings", Number(e.target.value) || 1)}
          />
        </label>
        <label className="field">
          <span>Budget gesamt (€)</span>
          <input
            type="number"
            min={0}
            step={0.5}
            placeholder="egal"
            value={value.maxBudget || ""}
            onChange={(e) => set("maxBudget", Number(e.target.value) || 0)}
          />
        </label>
        <label className="field">
          <span>Ernährung</span>
          <select value={value.diet} onChange={(e) => set("diet", e.target.value)}>
            {DIETS.map((d) => (
              <option key={d} value={d}>
                {d || "alles"}
              </option>
            ))}
          </select>
        </label>
        <label className="field grow">
          <span>Ohne</span>
          <input placeholder="z. B. Pilze, Koriander" value={value.exclude} onChange={(e) => set("exclude", e.target.value)} />
        </label>
      </div>
    </form>
  );
}
