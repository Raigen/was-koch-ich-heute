import { formatEur } from "./format";
import type { Comparison, Offer } from "./types";

/** The cheapest mix of a comparison, grouped by shop in the order the shops first appear. */
export function groupMixByStore(c: Comparison): [storeId: string, offers: Offer[]][] {
  const byStore = new Map<string, Offer[]>();
  for (const m of c.mix) {
    if (!m.offer) continue;
    byStore.set(m.offer.storeId, [...(byStore.get(m.offer.storeId) ?? []), m.offer]);
  }
  return [...byStore];
}

export const shoppingLine = (o: Offer) => `${o.packagesNeeded}× ${o.productName}`;

/** Plain-text shopping list for the clipboard. */
export function shoppingListText(c: Comparison, storeName: (id: string) => string): string {
  const lines: string[] = [];
  for (const [sid, offers] of groupMixByStore(c)) {
    lines.push(`${storeName(sid)}:`);
    for (const o of offers) lines.push(`  - ${shoppingLine(o)} (${formatEur(o.cost)})`);
  }
  if (c.mixMissing.length) lines.push(`Nicht gefunden: ${c.mixMissing.join(", ")}`);
  lines.push(`Summe: ${formatEur(c.mixTotal)}`);
  return lines.join("\n");
}
