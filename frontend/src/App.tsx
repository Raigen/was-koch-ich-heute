import { useEffect, useRef, useState } from "react";
import { getConfig, getStores, searchRecipes } from "./api";
import { favorites as favoritesStore, recipeKey, type Favorite, type FavoriteChanges, type SavedPriceCheck } from "./favorites";
import { storageGet, storageSet } from "./format";
import type { AppConfig, Recipe, RecipeQuery, Store } from "./types";
import SearchForm from "./components/SearchForm";
import StorePicker from "./components/StorePicker";
import RecipeList from "./components/RecipeList";
import RecipeDetail from "./components/RecipeDetail";
import FavoritesList from "./components/FavoritesList";

type View = "search" | "favorites";

interface Selection {
  recipe: Recipe;
  /** Start a price comparison right away. */
  autoCompare: boolean;
  /** Makes reopening the same recipe a fresh detail view. */
  nonce: number;
}

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
  const [view, setView] = useState<View>("search");
  const [selected, setSelected] = useState<Selection | null>(null);
  const [favs, setFavs] = useState<Favorite[]>([]);
  // Loading the Merkliste failed (IndexedDB unavailable): shown instead of the list.
  const [favUnavailable, setFavUnavailable] = useState<string | null>(null);
  // Saving, updating or removing a favorite failed.
  const [favWriteError, setFavWriteError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const abortRef = useRef<AbortController | null>(null);

  useEffect(() => {
    getConfig().then(setConfig).catch(() => setConfig(null));
    getStores()
      .then(setStores)
      .catch((e: Error) => setError(`Backend nicht erreichbar: ${e.message}`));
  }, []);

  useEffect(() => {
    favoritesStore
      .list()
      .then(setFavs)
      .catch((e: Error) => setFavUnavailable(e.message));
  }, []);

  useEffect(() => storageSet("stores", selectedStores), [selectedStores]);
  useEffect(() => storageSet("plz", postalCode), [postalCode]);

  async function runSearch(q: RecipeQuery) {
    abortRef.current?.abort();
    const ctrl = new AbortController();
    abortRef.current = ctrl;
    storageSet("query", { servings: q.servings, maxBudget: q.maxBudget, diet: q.diet });
    setLoading(true);
    setError(null);
    setSelected(null);
    setView("search");
    try {
      setRecipes(await searchRecipes(q, ctrl.signal));
    } catch (e) {
      if ((e as Error).name !== "AbortError") setError((e as Error).message);
    } finally {
      if (abortRef.current === ctrl) setLoading(false);
    }
  }

  const findFavorite = (r: Recipe) => {
    const k = recipeKey(r);
    return favs.find((f) => recipeKey(f.recipe) === k);
  };

  const favWritten = () => setFavWriteError(null);

  // Removing and updating are optimistic. On failure, the stored state is reloaded,
  // or the previous state restored if even reading fails.
  function favFailed(e: Error, previous?: Favorite[]) {
    setFavWriteError(e.message);
    if (previous) favoritesStore.list().then(setFavs, () => setFavs(previous));
  }

  function saveFavorite(recipe: Recipe, priceCheck: SavedPriceCheck | null = null) {
    favoritesStore
      .save(recipe, priceCheck)
      .then((f) => {
        favWritten();
        setFavs((list) => [f, ...list.filter((x) => x.id !== f.id)]);
      })
      .catch((e: Error) => favFailed(e));
  }

  function removeFavorite(id: string) {
    const previous = favs;
    setFavs((list) => list.filter((f) => f.id !== id));
    favoritesStore.remove(id).then(favWritten, (e: Error) => favFailed(e, previous));
  }

  function updateFavorite(id: string, changes: FavoriteChanges) {
    const previous = favs;
    // Optimistic, so that ticking shopping list items feels instant.
    setFavs((list) => list.map((f) => (f.id === id ? { ...f, ...changes } : f)));
    favoritesStore.update(id, changes).then(favWritten, (e: Error) => favFailed(e, previous));
  }

  // Asks before throwing away a note or a saved price comparison.
  function confirmRemove(f: Favorite, always = false) {
    const hasData = !!f.note || !!f.priceCheck;
    if (!always && !hasData) return true;
    const loss = hasData ? " Notiz und gespeicherter Preisvergleich gehen dabei verloren." : "";
    return window.confirm(`„${f.recipe.title}“ von der Merkliste entfernen?${loss}`);
  }

  function toggleFavorite(r: Recipe) {
    const f = findFavorite(r);
    if (!f) saveFavorite(r);
    else if (confirmRemove(f)) removeFavorite(f.id);
  }

  function select(recipe: Recipe, autoCompare = false) {
    setSelected({ recipe, autoCompare, nonce: Date.now() });
    window.scrollTo({ top: 0 });
  }

  function switchView(v: View) {
    setView(v);
    setSelected(null);
    setFavWriteError(null);
  }

  const storeRow = (
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
  );

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

      <nav className="tabs" aria-label="Ansicht">
        <button className={view === "search" ? "on" : ""} aria-current={view === "search"} onClick={() => switchView("search")}>
          Rezeptsuche
        </button>
        <button
          className={view === "favorites" ? "on" : ""}
          aria-current={view === "favorites"}
          onClick={() => switchView("favorites")}
        >
          ★ Merkliste{favs.length > 0 && ` (${favs.length})`}
        </button>
      </nav>

      {view === "search" ? (
        <section className="panel">
          <SearchForm value={query} onChange={setQuery} onSubmit={runSearch} loading={loading} />
          {storeRow}
        </section>
      ) : (
        <section className="panel">
          <h2>Merkliste</h2>
          <p className="muted small">
            Gemerkte Rezepte werden nur in diesem Browser gespeichert. Für „Preise neu vergleichen“ gelten die
            Shops und die PLZ hier:
          </p>
          {storeRow}
        </section>
      )}

      {view === "search" && error && <div className="error">{error}</div>}
      {/* In the Merkliste, an "unavailable" message is already shown in place of the list. */}
      {favWriteError && !(view === "favorites" && favWriteError === favUnavailable) && (
        <div className="error">{favWriteError}</div>
      )}

      {view === "search" && loading && (
        <div className="loading">
          <span className="spinner" /> Die KI recherchiert Rezepte im Web … das kann bis zu einer Minute dauern.
        </div>
      )}

      {selected ? (
        <RecipeDetail
          key={selected.nonce}
          recipe={selected.recipe}
          stores={stores.filter((s) => selectedStores.includes(s.id))}
          allStores={stores}
          postalCode={postalCode}
          favorite={findFavorite(selected.recipe)}
          autoCompare={selected.autoCompare}
          backLabel={view === "search" ? "zurück zu den Ergebnissen" : "zurück zur Merkliste"}
          onBack={() => setSelected(null)}
          onSave={saveFavorite}
          onRemove={(id) => {
            const f = favs.find((x) => x.id === id);
            if (f && confirmRemove(f)) removeFavorite(id);
          }}
          onUpdate={updateFavorite}
        />
      ) : view === "search" ? (
        recipes &&
        !loading && (
          <RecipeList
            recipes={recipes}
            onSelect={(r) => select(r)}
            isSaved={(r) => !!findFavorite(r)}
            onToggleSaved={toggleFavorite}
          />
        )
      ) : favUnavailable ? (
        <div className="error">{favUnavailable}</div>
      ) : (
        <FavoritesList
          favorites={favs}
          allStores={stores}
          canCompare={selectedStores.length > 0}
          onOpen={(f) => select(f.recipe)}
          onCompare={(f) => select(f.recipe, true)}
          onRemove={(f) => {
            if (confirmRemove(f, true)) removeFavorite(f.id);
          }}
        />
      )}

      <footer className="footer">
        Preise werden per KI-Websuche ermittelt und können vom Preis im Markt abweichen. Angaben ohne Gewähr.
      </footer>
    </div>
  );
}
