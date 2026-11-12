interface Props {
  saved: boolean;
  onClick: () => void;
}

/** Star toggle that adds a recipe to the Merkliste or removes it. */
export default function FavoriteButton({ saved, onClick }: Props) {
  return (
    <button
      type="button"
      className={`btn-fav ${saved ? "on" : ""}`}
      aria-pressed={saved}
      title={saved ? "Von der Merkliste entfernen" : "Auf die Merkliste setzen"}
      onClick={onClick}
    >
      <span aria-hidden="true">{saved ? "★" : "☆"}</span> {saved ? "Gemerkt" : "Merken"}
    </button>
  );
}
