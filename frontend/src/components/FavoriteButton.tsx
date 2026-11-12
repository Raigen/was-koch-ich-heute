interface Props {
  /** Recipe title, so screen readers can tell the buttons apart. */
  title: string;
  saved: boolean;
  onClick: () => void;
}

/**
 * Star toggle that adds a recipe to the Merkliste or removes it. The accessible name
 * starts with the visible label (WCAG 2.5.3 "Label in Name") and adds the recipe title.
 * The label itself says the state, so there is no aria-pressed (it would announce the
 * state twice).
 */
export default function FavoriteButton({ title, saved, onClick }: Props) {
  const label = saved ? "Gemerkt" : "Merken";
  return (
    <button
      type="button"
      className={`btn-fav ${saved ? "on" : ""}`}
      aria-label={`${label}: ${title}`}
      title={saved ? "Von der Merkliste entfernen" : "Auf die Merkliste setzen"}
      onClick={onClick}
    >
      <span aria-hidden="true">{saved ? "★" : "☆"}</span> {label}
    </button>
  );
}
