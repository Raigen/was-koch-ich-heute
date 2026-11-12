import type { AppConfig, Ingredient, PriceEvent, Recipe, RecipeQuery, Store } from "./types";

async function json<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let msg = `${res.status} ${res.statusText}`;
    try {
      const body = await res.json();
      if (body?.error) msg = body.error;
    } catch {
      /* response was not JSON */
    }
    throw new Error(msg);
  }
  return res.json() as Promise<T>;
}

export const getConfig = () => fetch("/api/config").then((r) => json<AppConfig>(r));
export const getStores = () => fetch("/api/stores").then((r) => json<Store[]>(r));

export async function searchRecipes(q: RecipeQuery, signal?: AbortSignal): Promise<Recipe[]> {
  const res = await fetch("/api/recipes/search", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(q),
    signal,
  });
  return (await json<{ recipes: Recipe[] }>(res)).recipes ?? [];
}

/**
 * Starts the price comparison. The backend streams NDJSON: one line per shop as
 * soon as its research is done, keep-alive pings, and a final summary.
 */
export async function comparePrices(
  ingredients: Ingredient[],
  storeIds: string[],
  postalCode: string,
  onEvent: (ev: PriceEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const res = await fetch("/api/prices/compare", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ ingredients, storeIds, postalCode }),
    signal,
  });
  if (!res.ok || !res.body) {
    await json(res);
    return;
  }
  const reader = res.body.pipeThrough(new TextDecoderStream()).getReader();
  let buffer = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += value;
    let nl: number;
    while ((nl = buffer.indexOf("\n")) >= 0) {
      const line = buffer.slice(0, nl).trim();
      buffer = buffer.slice(nl + 1);
      if (line) onEvent(JSON.parse(line) as PriceEvent);
    }
  }
  if (buffer.trim()) onEvent(JSON.parse(buffer) as PriceEvent);
}
