import type { Store } from "../types";

interface Props {
  stores: Store[];
  selected: string[];
  onChange: (ids: string[]) => void;
}

export default function StorePicker({ stores, selected, onChange }: Props) {
  const toggle = (id: string) =>
    onChange(selected.includes(id) ? selected.filter((s) => s !== id) : [...selected, id]);

  return (
    <fieldset className="store-picker">
      <legend>Supermärkte vergleichen</legend>
      <div className="chips">
        {stores.map((s) => (
          <label key={s.id} className={`store-chip ${selected.includes(s.id) ? "on" : ""}`} style={{ "--store": s.color } as React.CSSProperties}>
            <input type="checkbox" checked={selected.includes(s.id)} onChange={() => toggle(s.id)} />
            {s.name}
          </label>
        ))}
      </div>
    </fieldset>
  );
}
