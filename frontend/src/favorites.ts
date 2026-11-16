// Favorites ("Merkliste") stored in the browser via IndexedDB.
//
// The rest of the app only talks to the async `FavoritesStore` interface, so the
// IndexedDB implementation can later be replaced by (or synced with) a backend API
// without touching the components.

import type { Comparison, Ingredient, Recipe, StorePrices } from "./types";

/** Version of the stored record format. Bump it and migrate in `upgrade` when it changes. */
export const FAVORITE_SCHEMA_VERSION = 1;

/** The latest price comparison saved with a favorite. */
export interface SavedPriceCheck {
  /** ISO timestamp of the comparison. */
  comparedAt: string;
  storeIds: string[];
  postalCode: string;
  /** The ingredients that were compared (staples the user already has are left out). */
  ingredients: Ingredient[];
  results: StorePrices[];
  comparison: Comparison;
  /** Keys (see `shoppingItemKey`) of shopping list items ticked off while shopping. */
  checked: string[];
}

export interface Favorite {
  id: string;
  schemaVersion: number;
  recipe: Recipe;
  note: string;
  priceCheck: SavedPriceCheck | null;
  /** ISO timestamps. */
  savedAt: string;
  updatedAt: string;
}

export type FavoriteChanges = Partial<Pick<Favorite, "note" | "priceCheck">>;

export interface FavoritesStore {
  /** All favorites, most recently saved first. */
  list(): Promise<Favorite[]>;
  get(id: string): Promise<Favorite | undefined>;
  /**
   * Saves a recipe. A recipe that is already saved (see `recipeKey`) is updated instead.
   * While a save of the same recipe is still pending, its promise is returned.
   */
  save(recipe: Recipe, priceCheck?: SavedPriceCheck | null): Promise<Favorite>;
  /** Updates an entry. Resolves to `undefined` if it was removed in the meantime. */
  update(id: string, changes: FavoriteChanges): Promise<Favorite | undefined>;
  remove(id: string): Promise<void>;
}

/** Thrown when the browser offers no usable IndexedDB (e.g. some private modes). */
export class FavoritesUnavailableError extends Error {
  constructor() {
    super(
      "Die Merkliste ist in diesem Browser nicht verfügbar (z. B. im privaten Modus). Gemerkte Rezepte können nicht gespeichert werden.",
    );
    this.name = "FavoritesUnavailableError";
  }
}

const norm = (s: string) => s.trim().toLowerCase().replace(/\s+/g, " ");

/**
 * Identifies "the same recipe" across searches. Recipe IDs come from the AI and are
 * only unique within one search result. Source URL or title alone are not enough
 * either (the AI may cite one collection page for several recipes, and titles like
 * "Spaghetti Carbonara" repeat), so the key combines source URL, title and the
 * ingredient names: only a recipe with the same content counts as already saved.
 */
export function recipeKey(r: Recipe): string {
  const url = norm(r.sourceUrl ?? "").replace(/^https?:\/\/(www\.)?/, "").replace(/\/+$/, "");
  const ingredients = (r.ingredients ?? []).map((i) => norm(i.name)).sort();
  return JSON.stringify([url, norm(r.title), ingredients]);
}

/** Stable key of one shopping list line, used to persist ticked items. */
export const shoppingItemKey = (storeId: string, ingredient: string) =>
  `${storeId}|${ingredient.trim().toLowerCase()}`;

const DB_NAME = "was-koch-ich-heute";
const DB_VERSION = 1;
const STORE = "favorites";

function newId(): string {
  // crypto.randomUUID is only available in secure contexts (not on plain http via LAN IP).
  if (typeof crypto !== "undefined" && "randomUUID" in crypto) return crypto.randomUUID();
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

function promisify<T>(req: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

/**
 * Brings records written by older app versions up to the current schema. Records from
 * a newer app version (e.g. read by a cached older deployment) are left as they are,
 * so that writing them back does not downgrade their version.
 */
function migrate(f: Favorite): Favorite {
  if ((f.schemaVersion ?? 0) >= FAVORITE_SCHEMA_VERSION) return f;
  // Version 1 is the first schema; future migrations go here.
  return { ...f, schemaVersion: FAVORITE_SCHEMA_VERSION };
}

class IndexedDBFavorites implements FavoritesStore {
  private db: Promise<IDBDatabase> | null = null;
  // Writes read and then write a record in two transactions; running them one
  // after another keeps quick successive updates (e.g. ticking items) from losing data.
  private writes: Promise<unknown> = Promise.resolve();

  private serial<T>(fn: () => Promise<T>): Promise<T> {
    const next = this.writes.then(fn, fn);
    this.writes = next.catch(() => {});
    return next;
  }

  private open(): Promise<IDBDatabase> {
    if (this.db) return this.db;
    const opened: Promise<IDBDatabase> = new Promise<IDBDatabase>((resolve, reject) => {
      let req: IDBOpenDBRequest;
      try {
        if (typeof indexedDB === "undefined") throw new Error("no IndexedDB");
        req = indexedDB.open(DB_NAME, DB_VERSION);
      } catch {
        reject(new FavoritesUnavailableError());
        return;
      }
      req.onupgradeneeded = () => {
        const db = req.result;
        if (!db.objectStoreNames.contains(STORE)) db.createObjectStore(STORE, { keyPath: "id" });
      };
      req.onsuccess = () => {
        const db = req.result;
        // Let a newer app version in another tab upgrade the DB, and reopen on the next
        // call if the browser closes the connection (e.g. Safari after a while).
        const drop = () => {
          if (this.db === opened) this.db = null;
        };
        db.onversionchange = () => {
          db.close();
          drop();
        };
        db.onclose = drop;
        resolve(db);
      };
      req.onerror = () => reject(new FavoritesUnavailableError());
      req.onblocked = () => reject(new FavoritesUnavailableError());
    });
    this.db = opened;
    // Allow a later retry if opening failed.
    opened.catch(() => {
      if (this.db === opened) this.db = null;
    });
    return opened;
  }

  private async tx<T>(mode: IDBTransactionMode, fn: (s: IDBObjectStore) => IDBRequest<T>): Promise<T> {
    const db = await this.open();
    const t = db.transaction(STORE, mode);
    const result = promisify(fn(t.objectStore(STORE)));
    // A failed request also fails the transaction; that error is the one reported below.
    result.catch(() => {});
    await new Promise<void>((resolve, reject) => {
      t.oncomplete = () => resolve();
      t.onerror = () => reject(t.error);
      t.onabort = () => reject(t.error ?? new Error("Transaktion abgebrochen"));
    });
    return result;
  }

  private put(f: Favorite): Promise<IDBValidKey> {
    return this.tx("readwrite", (s) => s.put(f));
  }

  async list(): Promise<Favorite[]> {
    const all = await this.tx("readonly", (s) => s.getAll() as IDBRequest<Favorite[]>);
    return all.map(migrate).sort((a, b) => b.savedAt.localeCompare(a.savedAt));
  }

  async get(id: string): Promise<Favorite | undefined> {
    const f = await this.tx("readonly", (s) => s.get(id) as IDBRequest<Favorite | undefined>);
    return f && migrate(f);
  }

  // Pending saves per recipe key: a double click must not queue a second save.
  private pendingSaves = new Map<string, Promise<Favorite>>();

  save(recipe: Recipe, priceCheck: SavedPriceCheck | null = null): Promise<Favorite> {
    const key = recipeKey(recipe);
    const pending = this.pendingSaves.get(key);
    if (pending) return pending;
    const p = this.serial(() => this.doSave(recipe, priceCheck)).finally(() => this.pendingSaves.delete(key));
    this.pendingSaves.set(key, p);
    return p;
  }

  private async doSave(recipe: Recipe, priceCheck: SavedPriceCheck | null): Promise<Favorite> {
    const now = new Date().toISOString();
    // The list is small (a personal Merkliste), so a scan is fine and keeps the
    // stored records free of derived index fields.
    const key = recipeKey(recipe);
    const existing = (await this.list()).find((f) => recipeKey(f.recipe) === key);
    const f: Favorite = existing
      ? {
          ...existing,
          recipe,
          priceCheck: priceCheck ?? existing.priceCheck,
          updatedAt: now,
        }
      : {
          id: newId(),
          schemaVersion: FAVORITE_SCHEMA_VERSION,
          recipe,
          note: "",
          priceCheck,
          savedAt: now,
          updatedAt: now,
        };
    await this.put(f);
    return f;
  }

  update(id: string, changes: FavoriteChanges): Promise<Favorite | undefined> {
    return this.serial(() => this.doUpdate(id, changes));
  }

  private async doUpdate(id: string, changes: FavoriteChanges): Promise<Favorite | undefined> {
    const existing = await this.get(id);
    // Removed in the meantime (e.g. a late tick or note write): nothing to update,
    // and the entry must not be written back.
    if (!existing) return undefined;
    const f: Favorite = { ...existing, ...changes, updatedAt: new Date().toISOString() };
    await this.put(f);
    return f;
  }

  remove(id: string): Promise<void> {
    return this.serial(async () => {
      await this.tx("readwrite", (s) => s.delete(id));
    });
  }
}

export const favorites: FavoritesStore = new IndexedDBFavorites();
