import { useEffect, useMemo, useState } from "react";
import { comparePrices } from "../api";
import { formatEur, formatQty } from "../format";
import type { Comparison, Ingredient, Offer, Store, StorePrices } from "../types";

interface Props {
  ingredients: Ingredient[];
  stores: Store[];
  allStores: Store[];
  postalCode: string;
}

const key = (s: string) => s.trim().toLowerCase();

export default function PriceComparison({ ingredients, stores, allStores, postalCode }: Props) {
  const [results, setResults] = useState<Record<string, StorePrices>>({});
  const [comparison, setComparison] = useState<Comparison | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    const ctrl = new AbortController();
    comparePrices(
      ingredients,
      stores.map((s) => s.id),
      postalCode,
      (ev) => {
        if (ev.type === "store") setResults((r) => ({ ...r, [ev.store.storeId]: ev.store }));
        else if (ev.type === "summary") setComparison(ev.comparison);
        else if (ev.type === "error") setError(ev.message);
      },
      ctrl.signal,
    ).catch((e: Error) => {
      if (e.name !== "AbortError") setError(e.message);
    });
    return () => ctrl.abort();
    // Neustart nur über "attempt" (erneuter Versuch) bzw. key in RecipeDetail.
  }, [attempt]);

  const failedStores = Object.values(results).filter((r) => r.error);

  // Erneuter Versuch: Erfolgreiche Shops kommen aus dem Backend-Cache (ohne
  // KI-Kosten), nur die fehlgeschlagenen werden neu recherchiert.
  function retryFailed() {
    setResults((r) => Object.fromEntries(Object.entries(r).filter(([, sp]) => !sp.error)));
    setComparison(null);
    setError(null);
    setAttempt((a) => a + 1);
  }

  const storeName = (id: string) => allStores.find((s) => s.id === id)?.name ?? id;
  const storeColor = (id: string) => allStores.find((s) => s.id === id)?.color ?? "#888";

  // Günstigstes Angebot je Zutat und Shop – wird schon während des Streamings angezeigt.
  const cells = useMemo(() => {
    const m = new Map<string, Offer>();
    for (const sp of Object.values(results)) {
      for (const o of sp.offers) {
        const k = `${sp.storeId}|${key(o.ingredient)}`;
        const prev = m.get(k);
        if (o.price > 0 && (!prev || o.cost < prev.cost)) m.set(k, o);
      }
    }
    return m;
  }, [results]);

  const rowMin = (ing: string) => {
    let min = Infinity;
    for (const s of stores) {
      const o = cells.get(`${s.id}|${key(ing)}`);
      if (o && o.cost < min) min = o.cost;
    }
    return min;
  };

  const doneCount = Object.keys(results).length;
  const finished = comparison !== null;

  function shoppingListText(c: Comparison): string {
    const byStore = new Map<string, Offer[]>();
    for (const m of c.mix) {
      if (!m.offer) continue;
      byStore.set(m.offer.storeId, [...(byStore.get(m.offer.storeId) ?? []), m.offer]);
    }
    const lines: string[] = [];
    for (const [sid, offers] of byStore) {
      lines.push(`${storeName(sid)}:`);
      for (const o of offers) lines.push(`  - ${o.packagesNeeded}× ${o.productName} (${formatEur(o.cost)})`);
    }
    if (c.mixMissing.length) lines.push(`Nicht gefunden: ${c.mixMissing.join(", ")}`);
    lines.push(`Summe: ${formatEur(c.mixTotal)}`);
    return lines.join("\n");
  }

  async function copyList() {
    if (!comparison) return;
    try {
      await navigator.clipboard.writeText(shoppingListText(comparison));
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      setError("Zwischenablage nicht verfügbar.");
    }
  }

  return (
    <section className="prices">
      <h3>Preisvergleich</h3>

      <div className="progress">
        {stores.map((s) => {
          const r = results[s.id];
          const state = !r ? "pending" : r.error ? "failed" : "done";
          return (
            <span key={s.id} className={`progress-item ${state}`} style={{ "--store": s.color } as React.CSSProperties} title={r?.error}>
              {state === "pending" && <span className="spinner small" />}
              {state === "done" && "✓"}
              {state === "failed" && (r?.timedOut ? "⏱" : "✕")} {s.name}
              {r?.cached && <em className="muted small"> (Cache)</em>}
            </span>
          );
        })}
        {!finished && (
          <span className="muted small">
            {doneCount}/{stores.length} Shops recherchiert – die KI durchsucht die Webshops, das dauert pro Shop 1–3 Minuten.
          </span>
        )}
      </div>

      {error && <div className="error">{error}</div>}

      {finished && failedStores.length > 0 && (
        <div className="notice retry">
          <span>
            {failedStores.length === 1 ? "1 Shop konnte" : `${failedStores.length} Shops konnten`} nicht recherchiert
            werden: {failedStores.map((f) => `${storeName(f.storeId)} (${f.error})`).join(", ")}
          </span>
          <button className="btn-secondary" onClick={retryFailed}>
            Fehlgeschlagene erneut versuchen
          </button>
        </div>
      )}

      {comparison && (
        <div className="summary">
          <div className="summary-card best">
            <div className="label">Günstigster Mix aus allen Shops</div>
            <div className="amount">{formatEur(comparison.mixTotal)}</div>
            <div className="muted small">
              {comparison.ingredientCount - comparison.mixMissing.length}/{comparison.ingredientCount} Zutaten gefunden
            </div>
            <button className="btn-secondary" onClick={copyList}>
              {copied ? "Kopiert ✓" : "Einkaufsliste kopieren"}
            </button>
          </div>
          {comparison.storeTotals.map((t) => (
            <div
              key={t.storeId}
              className={`summary-card ${t.storeId === comparison.cheapestStoreId ? "cheapest" : ""}`}
              style={{ "--store": storeColor(t.storeId) } as React.CSSProperties}
            >
              <div className="label">
                {storeName(t.storeId)}
                {t.storeId === comparison.cheapestStoreId && <span className="tag">günstigster Einzelshop</span>}
              </div>
              <div className="amount">{formatEur(t.total)}</div>
              <div className="muted small">
                {t.found}/{comparison.ingredientCount} Zutaten
                {t.missing.length > 0 && ` · fehlt: ${t.missing.join(", ")}`}
              </div>
            </div>
          ))}
        </div>
      )}

      <div className="table-wrap">
        <table className="matrix">
          <thead>
            <tr>
              <th>Zutat</th>
              {stores.map((s) => (
                <th key={s.id} style={{ borderTopColor: s.color }}>
                  {s.name}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {ingredients.map((ing) => {
              const min = rowMin(ing.name);
              return (
                <tr key={ing.name}>
                  <th scope="row">
                    {ing.name}
                    <div className="muted small">{formatQty(ing.quantity, ing.unit)}</div>
                  </th>
                  {stores.map((s) => {
                    const r = results[s.id];
                    const o = cells.get(`${s.id}|${key(ing.name)}`);
                    if (!r) return <td key={s.id} className="cell pending">…</td>;
                    if (r.error)
                      return (
                        <td key={s.id} className="cell muted" title={r.error}>
                          {r.timedOut ? "Zeitüberschreitung" : "Fehler"}
                        </td>
                      );
                    if (!o) return <td key={s.id} className="cell muted">nicht gefunden</td>;
                    return (
                      <td key={s.id} className={`cell ${o.cost === min ? "min" : ""}`}>
                        <div className="cost">{formatEur(o.cost)}</div>
                        <div className="product">
                          {o.url ? (
                            <a href={o.url} target="_blank" rel="noreferrer noopener">
                              {o.productName}
                            </a>
                          ) : (
                            o.productName
                          )}
                        </div>
                        <div className="muted small">
                          {o.packagesNeeded > 1 ? `${o.packagesNeeded}× ` : ""}
                          {formatEur(o.price)}
                          {o.packageAmount > 0 && ` / ${formatQty(o.packageAmount, o.packageUnit)}`}
                        </div>
                        <div className="tags">
                          {o.isPromotion && <span className="tag promo">Angebot</span>}
                          {o.confidence && <span className={`tag conf-${o.confidence}`}>{o.confidence}</span>}
                        </div>
                        {o.note && <div className="muted small">{o.note}</div>}
                      </td>
                    );
                  })}
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <p className="muted small">
        „hoch“ = Preis direkt auf der Shopseite gefunden, „mittel“ = aus Prospekt/Suchvorschau, „niedrig“ = Schätzung.
      </p>
    </section>
  );
}
