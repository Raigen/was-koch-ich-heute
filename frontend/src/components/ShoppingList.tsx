import { useState } from "react";
import { shoppingItemKey, type SavedPriceCheck } from "../favorites";
import { formatEur } from "../format";
import { groupMixByStore, shoppingLine, shoppingListText } from "../shoppingList";
import type { Store } from "../types";

interface Props {
  check: SavedPriceCheck;
  allStores: Store[];
  onCheckedChange: (checked: string[]) => void;
}

/** The saved cheapest mix as a checklist to tick off while shopping. */
export default function ShoppingList({ check, allStores, onCheckedChange }: Props) {
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState(false);
  const groups = groupMixByStore(check.comparison);
  const checked = new Set(check.checked);
  const total = groups.reduce((n, [, offers]) => n + offers.length, 0);
  const done = groups.reduce((n, [sid, offers]) => n + offers.filter((o) => checked.has(shoppingItemKey(sid, o.ingredient))).length, 0);

  const store = (id: string) => allStores.find((s) => s.id === id);

  function toggle(k: string) {
    const next = new Set(checked);
    if (next.has(k)) next.delete(k);
    else next.add(k);
    onCheckedChange([...next]);
  }

  async function copy() {
    try {
      await navigator.clipboard.writeText(shoppingListText(check.comparison, (id) => store(id)?.name ?? id));
      setCopyError(false);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      setCopyError(true);
    }
  }

  return (
    <div className="panel shopping-list">
      <div className="shopping-head">
        <h3>Einkaufsliste</h3>
        <span className="muted small">
          {done}/{total} erledigt
        </span>
      </div>
      {groups.map(([sid, offers]) => (
        <div key={sid} className="shopping-group" style={{ "--store": store(sid)?.color ?? "#888" } as React.CSSProperties}>
          <h4>{store(sid)?.name ?? sid}</h4>
          <ul className="ingredients">
            {offers.map((o) => {
              const k = shoppingItemKey(sid, o.ingredient);
              return (
                <li key={k}>
                  <label>
                    <input type="checkbox" checked={checked.has(k)} onChange={() => toggle(k)} />
                    <span className={checked.has(k) ? "struck" : ""}>{shoppingLine(o)}</span>
                    <span className="qty">{formatEur(o.cost)}</span>
                  </label>
                </li>
              );
            })}
          </ul>
        </div>
      ))}
      {check.comparison.mixMissing.length > 0 && (
        <p className="muted small">Nicht gefunden: {check.comparison.mixMissing.join(", ")}</p>
      )}
      <div className="shopping-foot">
        <strong>Summe: {formatEur(check.comparison.mixTotal)}</strong>
        <span className="shopping-actions">
          {done > 0 && (
            <button className="btn-secondary" onClick={() => onCheckedChange([])}>
              Haken zurücksetzen
            </button>
          )}
          <button className="btn-secondary" onClick={copy}>
            {copied ? "Kopiert ✓" : "Einkaufsliste kopieren"}
          </button>
        </span>
      </div>
      {copyError && <p className="error small">Zwischenablage nicht verfügbar.</p>}
    </div>
  );
}
