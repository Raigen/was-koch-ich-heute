import { formatCost, formatCount } from "../format";
import type { Usage } from "../types";

export interface UsagePart {
  label: string;
  usage?: Usage;
}

interface Props {
  usage?: Usage;
  /** Compact form (cost only), e.g. in a shop's progress chip. */
  compact?: boolean;
  /** Optional per-part breakdown (e.g. per shop) shown in the details. */
  breakdown?: UsagePart[];
  className?: string;
}

const totalTokens = (u: Usage) => u.inputTokens + u.outputTokens + u.cacheReadTokens + u.cacheWriteTokens;

function costText(u: Usage): string {
  if (u.cached || u.provider === "demo") return "kostenlos";
  if (!u.costKnown) return "Kosten unbekannt";
  return `${u.incomplete ? "mind." : "≈"} ${formatCost(u.costEur)}`;
}

const searchesText = (n: number) => `${formatCount(n)} ${n === 1 ? "Web-Suche" : "Web-Suchen"}`;

function summary(u: Usage): string {
  return `${costText(u)} · ${formatCount(totalTokens(u))} Tokens · ${searchesText(u.webSearches)}`;
}

function details(u: Usage): string[] {
  const lines = [
    `Anbieter: ${u.provider}${u.model && u.model !== u.provider ? ` (${u.model})` : ""}`,
    `API-Aufrufe: ${formatCount(u.requests)}`,
    `Eingabe-Tokens: ${formatCount(u.inputTokens)}`,
    `Ausgabe-Tokens: ${formatCount(u.outputTokens)}`,
  ];
  if (u.cacheReadTokens) lines.push(`Aus Prompt-Cache gelesen: ${formatCount(u.cacheReadTokens)} Tokens`);
  if (u.cacheWriteTokens) lines.push(`In Prompt-Cache geschrieben: ${formatCount(u.cacheWriteTokens)} Tokens`);
  lines.push(`Web-Suchen: ${formatCount(u.webSearches)}`);
  if (u.webFetches) lines.push(`Seitenabrufe: ${formatCount(u.webFetches)}`);
  if (u.incomplete) {
    lines.push("Unvollständig: mindestens ein API-Aufruf wurde abgebrochen (z. B. Zeitüberschreitung) – seine Kosten fehlen.");
  }
  if (!u.costKnown) {
    lines.push(
      u.requests === 0
        ? "Keine Verbrauchsdaten: Die Recherche wurde vor der ersten Antwort abgebrochen oder ist fehlgeschlagen."
        : "Für dieses Modell ist kein Preis hinterlegt (PRICE_TABLE).",
    );
  } else {
    lines.push("Geschätzte Kosten laut Preistabelle im Backend, ohne Gewähr.");
  }
  return lines;
}

const CACHE_HINT = "Ergebnis aus dem Cache des Backends – es wurde keine neue KI-Recherche ausgeführt.";

/**
 * Shows the AI usage of a search, e.g. "≈ 0,12 € · 18.400 Tokens · 6 Web-Suchen".
 * The full form is a disclosure (`<details>`), so the details also work with
 * touch and keyboard; the compact form only shows the cost.
 */
export default function UsageLine({ usage, compact, breakdown, className = "" }: Props) {
  if (!usage) return null;
  const cls = `usage-line ${className}`.trim();

  if (compact) {
    if (usage.provider === "demo") {
      return usage.cached ? <em className={cls}>(Cache)</em> : null;
    }
    if (usage.cached) {
      return (
        <em className={cls} title={CACHE_HINT}>
          (Cache · kostenlos)
        </em>
      );
    }
    const text = costText(usage);
    return (
      <span className={cls} title={details(usage).join("\n")}>
        {text}
      </span>
    );
  }

  if (usage.provider === "demo") return <div className={cls}>Demo-Modus · keine KI-Kosten</div>;
  if (usage.cached) {
    return <div className={cls}>Aus dem Cache · keine neuen KI-Kosten</div>;
  }

  return (
    <details className={cls}>
      <summary>{summary(usage)}</summary>
      <ul className="usage-details">
        {details(usage).map((line) => (
          <li key={line}>{line}</li>
        ))}
      </ul>
      {breakdown && breakdown.length > 0 && (
        <ul className="usage-details">
          {breakdown.map((p) => (
            <li key={p.label}>
              {p.label}:{" "}
              {!p.usage ? "keine Angaben" : p.usage.cached ? "aus dem Cache, kostenlos" : summary(p.usage)}
            </li>
          ))}
        </ul>
      )}
    </details>
  );
}
