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
  /** Saves a recipe. A recipe that is already saved (same source or title) is updated instead. */
  save(recipe: Recipe, priceCheck?: SavedPriceCheck | null): Promise<Favorite>;
  update(id: string, changes: FavoriteChanges): Promise<Favorite>;
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

/**
 * Identifies "the same recipe" across searches. Recipe IDs come from the AI and are
 * only unique within one search result, so the source URL (or the title) is used.
 */
export function recipeKey(r: Recipe): string {
  return (r.sourceUrl || r.title).trim().toLowerCase();
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

/** Brings records written by older app versions up to the current schema. */
function migrate(f: Favorite): Favorite {
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
    this.db ??= new Promise<IDBDatabase>((resolve, reject) => {
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
        if (!db.objectStoreNames.contains(STORE)) {
          const store = db.createObjectStore(STORE, { keyPath: "id" });
          store.createIndex("recipeKey", "recipeKey", { unique: false });
        }
      };
      req.onsuccess = () => resolve(req.result);
      req.onerror = () => reject(new FavoritesUnavailableError());
      req.onblocked = () => reject(new FavoritesUnavailableError());
    });
    // Allow a later retry if opening failed.
    this.db.catch(() => (this.db = null));
    return this.db;
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
    // recipeKey is stored alongside the record for the index; it is not part of `Favorite`.
    return this.tx("readwrite", (s) => s.put({ ...f, recipeKey: recipeKey(f.recipe) }));
  }

  async list(): Promise<Favorite[]> {
    const all = await this.tx("readonly", (s) => s.getAll() as IDBRequest<Favorite[]>);
    return all.map(strip).map(migrate).sort((a, b) => b.savedAt.localeCompare(a.savedAt));
  }

  async get(id: string): Promise<Favorite | undefined> {
    const f = await this.tx("readonly", (s) => s.get(id) as IDBRequest<Favorite | undefined>);
    return f && migrate(strip(f));
  }

  save(recipe: Recipe, priceCheck: SavedPriceCheck | null = null): Promise<Favorite> {
    return this.serial(() => this.doSave(recipe, priceCheck));
  }

  private async doSave(recipe: Recipe, priceCheck: SavedPriceCheck | null): Promise<Favorite> {
    const now = new Date().toISOString();
    const existing = await this.tx(
      "readonly",
      (s) => s.index("recipeKey").get(recipeKey(recipe)) as IDBRequest<Favorite | undefined>,
    );
    const f: Favorite = existing
      ? {
          ...migrate(strip(existing)),
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

  update(id: string, changes: FavoriteChanges): Promise<Favorite> {
    return this.serial(() => this.doUpdate(id, changes));
  }

  private async doUpdate(id: string, changes: FavoriteChanges): Promise<Favorite> {
    const existing = await this.get(id);
    if (!existing) throw new Error("Dieses Rezept ist nicht mehr auf der Merkliste.");
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

function strip(f: Favorite & { recipeKey?: string }): Favorite {
  const { recipeKey: _ignored, ...rest } = f;
  void _ignored;
  return rest;
}

export const favorites: FavoritesStore = new IndexedDBFavorites();
