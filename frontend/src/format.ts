const eur = new Intl.NumberFormat("de-DE", { style: "currency", currency: "EUR" });
const num = new Intl.NumberFormat("de-DE", { maximumFractionDigits: 2 });

export const formatEur = (v: number) => eur.format(v);

const int = new Intl.NumberFormat("de-DE", { maximumFractionDigits: 0 });

/** Formats a count with German thousands separators, e.g. 18.400. */
export const formatCount = (v: number) => int.format(v);

/** Formats an estimated AI cost; amounts below one cent are shown as "< 0,01 €". */
export function formatCost(v: number): string {
  if (v > 0 && v < 0.005) return `< ${eur.format(0.01)}`;
  return eur.format(v);
}

export function formatQty(quantity: number, unit: string): string {
  if (!quantity) return unit;
  return `${num.format(quantity)} ${unit}`.trim();
}

export function storageGet<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : fallback;
  } catch {
    return fallback;
  }
}

export function storageSet(key: string, value: unknown) {
  try {
    localStorage.setItem(key, JSON.stringify(value));
  } catch {
    /* storage unavailable – ignore */
  }
}
