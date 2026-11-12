import { useEffect, useRef, useState } from "react";
import { getConfig, getMealieRecipe, getStores, searchMealie, searchRecipes } from "./api";
import { storageGet, storageSet } from "./format";
import type { AppConfig, MealieRecipePage, MealieRecipeSummary, Recipe, RecipeQuery, Store } from "./types";
import SearchForm from "./components/SearchForm";
import MealieSearch from "./components/MealieSearch";
import MealieList from "./components/MealieList";
import StorePicker from "./components/StorePicker";
import RecipeList from "./components/RecipeList";
import RecipeDetail from "./components/RecipeDetail";

type Source = "web" | "mealie";

const DEFAULT_QUERY: RecipeQuery = { query: "", servings: 4, maxBudget: 0, diet: "", maxRecipes: 4, exclude: "" };

export default function App() {
  const [config, setConfig] = useState<AppConfig | null>(null);
  const [stores, setStores] = useState<Store[]>([]);
  const [selectedStores, setSelectedStores] = useState<string[]>(() =>
    storageGet("stores", ["rewe", "edeka", "aldi-sued", "lidl"]),
  );
  const [postalCode, setPostalCode] = useState<string>(() => storageGet("plz", ""));
  const [query, setQuery] = useState<RecipeQuery>(() => ({ ...DEFAULT_QUERY, ...storageGet("query", {}), query: "" }));

  const [recipes, setRecipes] = useState<Recipe[] | null>(null);
  const [selected, setSelected] = useState<Recipe | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  // Recipe source: AI web research or the user's own Mealie instance.
  // Without Mealie configured in the backend, only the web search exists.
  const [sourcePref, setSourcePref] = useState<Source>(() => storageGet<Source>("source", "web"));
  const source: Source = config?.mealie ? sourcePref : "web";
  const [mealieQuery, setMealieQuery] = useState("");
  const [mealieResult, setMealieResult] = useState<MealieRecipePage | null>(null);
  const [mealieLoading, setMealieLoading] = useState(false);
  const [mealieMore, setMealieMore] = useState(false);
  const [openingSlug, setOpeningSlug] = useState<string | null>(null);
  const mealieAbortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    getConfig().then(setConfig).catch(() => setConfig(null));
    getStores()
      .then(setStores)
      .catch((e: Error) => setError(`Backend nicht erreichbar: ${e.message}`));
  }, []);

  useEffect(() => storageSet("stores", selectedStores), [selectedStores]);
  useEffect(() => storageSet("plz", postalCode), [postalCode]);
  useEffect(() => storageSet("source", sourcePref), [sourcePref]);

  // In Mealie mode, list the recipes right away: there is no AI waiting time.
  useEffect(() => {
    if (source === "mealie" && !mealieResult) void runMealieSearch(1);
  }, [source]);

  async function runSearch(q: RecipeQuery) {
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    storageSet("query", { servings: q.servings, maxBudget: q.maxBudget, diet: q.diet });
    setLoading(true);
    setError(null);
    setSelected(null);
    try {
      setRecipes(await searchRecipes(q, ctrl.signal));
    } catch (e) {
      if ((e as Error).name !== "AbortError") setError((e as Error).message);
    } finally {
      if (abortRef.current === ctrl) setLoading(false);
    }
  }

  function switchSource(next: Source) {
    if (next === source) return;
    setSourcePref(next);
    setSelected(null);
    setError(null);
  }

  async function runMealieSearch(page: number) {
    mealieAbortRef.current?.abort();
    const ctrl = new AbortController();
    mealieAbortRef.current = ctrl;
    if (page === 1) {
      setMealieLoading(true);
      setSelected(null);
    } else {
      setMealieMore(true);
    }
    setError(null);
    try {
      const res = await searchMealie(mealieQuery.trim(), page, ctrl.signal);
      setMealieResult((prev) => (page > 1 && prev ? { ...res, recipes: [...prev.recipes, ...res.recipes] } : res));
    } catch (e) {
      if ((e as Error).name !== "AbortError") setError((e as Error).message);
    } finally {
      if (mealieAbortRef.current === ctrl) {
        setMealieLoading(false);
        setMealieMore(false);
      }
    }
  }

  async function openMealieRecipe(r: MealieRecipeSummary) {
    storageSet("query", { servings: query.servings, maxBudget: query.maxBudget, diet: query.diet });
    setOpeningSlug(r.slug);
    setError(null);
    try {
      setSelected(await getMealieRecipe(r.slug, query.servings));
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setOpeningSlug(null);
    }
  }

  return (
    <div className="app">
      <header className="app-header">
        <div>
          <h1>Was koch ich heute?</h1>
          <p className="subtitle">Rezepte finden, Zutaten im Supermarkt-Webshop suchen, Preise vergleichen.</p>
        </div>
        {config && (
          <span className={`badge ${config.demo ? "badge-warn" : "badge-ok"}`} title="Aktiver KI-Anbieter">
            {config.provider}
          </span>
        )}
      </header>

      {config?.demo && (
        <div className="notice">
          Demo-Modus: Rezepte und Preise sind Beispieldaten. Für echte KI-Recherche <code>ANTHROPIC_API_KEY</code>{" "}
          oder <code>MISTRAL_API_KEY</code> im Backend setzen.
        </div>
      )}

      <section className="panel">
        {config?.mealie && (
          <div className="source-toggle" role="group" aria-label="Rezeptquelle">
            <button type="button" aria-pressed={source === "web"} onClick={() => switchSource("web")}>
              Im Web suchen
            </button>
            <button type="button" aria-pressed={source === "mealie"} onClick={() => switchSource("mealie")}>
              Aus Mealie
            </button>
          </div>
        )}
        {source === "mealie" ? (
          <MealieSearch
            query={mealieQuery}
            onQueryChange={setMealieQuery}
            servings={query.servings}
            onServingsChange={(servings) => setQuery((q) => ({ ...q, servings }))}
            onSubmit={() => runMealieSearch(1)}
            loading={mealieLoading}
          />
        ) : (
          <SearchForm value={query} onChange={setQuery} onSubmit={runSearch} loading={loading} />
        )}
        <div className="row">
          <StorePicker stores={stores} selected={selectedStores} onChange={setSelectedStores} />
          <label className="field plz">
            <span>PLZ (optional)</span>
            <input
              inputMode="numeric"
              maxLength={5}
              placeholder="z. B. 50667"
              value={postalCode}
              onChange={(e) => setPostalCode(e.target.value.replace(/\D/g, ""))}
            />
          </label>
        </div>
      </section>

      {error && <div className="error">{error}</div>}

      {source === "web" && loading && (
        <div className="loading">
          <span className="spinner" /> Die KI recherchiert Rezepte im Web … das kann bis zu einer Minute dauern.
        </div>
      )}

      {selected ? (
        <RecipeDetail
          key={selected.id}
          recipe={selected}
          stores={stores.filter((s) => selectedStores.includes(s.id))}
          allStores={stores}
          postalCode={postalCode}
          onBack={() => setSelected(null)}
        />
      ) : source === "mealie" ? (
        mealieLoading ? (
          <div className="loading">
            <span className="spinner" /> Rezepte werden aus Mealie geladen …
          </div>
        ) : (
          mealieResult && (
            <MealieList
              result={mealieResult}
              onSelect={openMealieRecipe}
              onMore={() => runMealieSearch(mealieResult.page + 1)}
              loadingMore={mealieMore}
              openingSlug={openingSlug}
            />
          )
        )
      ) : (
        recipes && !loading && <RecipeList recipes={recipes} onSelect={setSelected} />
      )}

      <footer className="footer">
        Preise werden per KI-Websuche ermittelt und können vom Preis im Markt abweichen. Angaben ohne Gewähr.
      </footer>
    </div>
  );
}
