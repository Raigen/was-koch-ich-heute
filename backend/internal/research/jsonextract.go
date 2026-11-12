package research

import (
	"encoding/json"
	"errors"
	"strings"
)

// extractJSON sucht im Antworttext des Modells ein JSON-Objekt und dekodiert
// es in out. Bevorzugt wird der letzte ```json-Block, ansonsten der Bereich
// von der ersten öffnenden bis zur letzten schließenden geschweiften Klammer.
func extractJSON(text string, out any) error {
	candidates := []string{}
	if i := strings.LastIndex(text, "```json"); i >= 0 {
		rest := text[i+len("```json"):]
		if j := strings.Index(rest, "```"); j >= 0 {
			candidates = append(candidates, rest[:j])
		} else {
			candidates = append(candidates, rest)
		}
	}
	if i, j := strings.Index(text, "{"), strings.LastIndex(text, "}"); i >= 0 && j > i {
		candidates = append(candidates, text[i:j+1])
	}
	var lastErr error = errors.New("keine JSON-Daten in der KI-Antwort gefunden")
	for _, c := range candidates {
		if err := json.Unmarshal([]byte(strings.TrimSpace(c)), out); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}
