package research

import (
	"encoding/json"
	"errors"
	"strings"
)

// extractJSON finds a JSON object in the model's answer and decodes it into
// out. It prefers the last ```json block, otherwise the range from the first
// opening to the last closing curly brace.
//
// The error text is shown in the (German) UI, hence German.
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
