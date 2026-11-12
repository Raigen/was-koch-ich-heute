// Mirrors the types in backend/internal/model/model.go. Keep both in sync.

export interface Store {
  id: string;
  name: string;
  domains: string[];
  color: string;
}

export interface Ingredient {
  name: string;
  quantity: number;
  unit: string;
  pantry: boolean;
}

export interface Recipe {
  id: string;
  title: string;
  description: string;
  sourceUrl: string;
  sourceName: string;
  servings: number;
  totalTimeMinutes: number;
  difficulty: string;
  ingredients: Ingredient[];
  steps: string[];
  estimatedCostEur: number;
}

export interface RecipeQuery {
  query: string;
  servings: number;
  maxBudget: number;
  diet: string;
  maxRecipes: number;
  exclude: string;
}

export interface Offer {
  storeId: string;
  ingredient: string;
  productName: string;
  price: number;
  packageAmount: number;
  packageUnit: string;
  packagesNeeded: number;
  cost: number;
  unitPrice: string;
  url: string;
  isPromotion: boolean;
  confidence: string;
  note: string;
}

export interface StorePrices {
  storeId: string;
  offers: Offer[];
  notFound: string[];
  error?: string;
  timedOut?: boolean;
  cached: boolean;
  /** AI usage of this shop's research (zero for cache hits). */
  usage?: Usage;
}

export interface StoreTotal {
  storeId: string;
  total: number;
  found: number;
  missing: string[];
}

export interface Comparison {
  storeTotals: StoreTotal[];
  cheapestStoreId: string;
  mix: { ingredient: string; offer: Offer | null }[];
  mixTotal: number;
  mixMissing: string[];
  ingredientCount: number;
  /** Total AI usage of all shops in this comparison. */
  usage?: Usage;
}

/** AI usage of a research call (or a sum of several) and its estimated cost. */
export interface Usage {
  provider: string;
  model: string;
  /** Number of API calls, including continuations. */
  requests: number;
  inputTokens: number;
  outputTokens: number;
  cacheReadTokens: number;
  cacheWriteTokens: number;
  webSearches: number;
  webFetches: number;
  costEur: number;
  /** False if the backend price table has no entry for the model. */
  costKnown: boolean;
  /** Served from the backend cache: no new AI cost. */
  cached: boolean;
}

/** Response of /api/recipes/search. */
export interface RecipeSearchResult {
  recipes: Recipe[];
  cached: boolean;
  usage?: Usage;
}

export interface AppConfig {
  provider: string;
  demo: boolean;
}

export type PriceEvent =
  | { type: "store"; store: StorePrices }
  | { type: "summary"; comparison: Comparison }
  | { type: "error"; message: string }
  // Backend keep-alive so that proxies keep the connection open.
  | { type: "ping" };
