import { formatCost, formatCount } from "../format";
import type { Usage } from "../types";

interface Props {
  usage?: Usage;
  /** Compact form (cost only), e.g. next to a shop name. */
  compact?: boolean;
  className?: string;
}

const totalTokens = (u: Usage) => u.inputTokens + u.outputTokens + u.cacheReadTokens + u.cacheWriteTokens;

function details(u: Usage): string {
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
  lines.push(
    u.costKnown
      ? "Geschätzte Kosten laut Preistabelle im Backend, ohne Gewähr."
      : "Für dieses Modell ist kein Preis hinterlegt (PRICE_TABLE).",
  );
  return lines.join("\n");
}

/** Shows the AI usage of a search, e.g. "≈ 0,12 € · 18.400 Tokens · 6 Web-Suchen". */
export default function UsageLine({ usage, compact, className = "" }: Props) {
  if (!usage) return null;
  const cls = `usage-line ${className}`.trim();

  if (usage.provider === "demo") {
    return compact ? null : <div className={cls}>Demo-Modus · keine KI-Kosten</div>;
  }
  if (usage.cached) {
    const title = "Ergebnis aus dem Cache des Backends – es wurde keine neue KI-Recherche ausgeführt.";
    return compact ? (
      <span className={cls} title={title}>
        kostenlos
      </span>
    ) : (
      <div className={cls} title={title}>
        Aus dem Cache · keine neuen KI-Kosten
      </div>
    );
  }

  const cost = usage.costKnown ? `≈ ${formatCost(usage.costEur)}` : "Kosten unbekannt";
  if (compact) {
    return (
      <span className={cls} title={details(usage)}>
        {cost}
      </span>
    );
  }
  const searches = `${formatCount(usage.webSearches)} ${usage.webSearches === 1 ? "Web-Suche" : "Web-Suchen"}`;
  return (
    <div className={cls} title={details(usage)}>
      {cost} · {formatCount(totalTokens(usage))} Tokens · {searches}
    </div>
  );
}
