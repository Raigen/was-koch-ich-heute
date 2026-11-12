interface Props {
  /** Recipe title, so screen readers can tell the buttons apart. */
  title: string;
  saved: boolean;
  onClick: () => void;
}

/**
 * Star toggle that adds a recipe to the Merkliste or removes it. The accessible name
 * is fixed per recipe; aria-pressed carries the state.
 */
export default function FavoriteButton({ title, saved, onClick }: Props) {
  return (
    <button
      type="button"
      className={`btn-fav ${saved ? "on" : ""}`}
      aria-label={`${title} merken`}
      aria-pressed={saved}
      title={saved ? "Von der Merkliste entfernen" : "Auf die Merkliste setzen"}
      onClick={onClick}
    >
      <span aria-hidden="true">{saved ? "★" : "☆"}</span> {saved ? "Gemerkt" : "Merken"}
    </button>
  );
}
