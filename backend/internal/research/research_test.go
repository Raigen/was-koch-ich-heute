package research

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/v2"
	"github.com/anthropics/anthropic-sdk-go/v2/option"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

func TestExtractJSON(t *testing.T) {
	var out struct{ A int }
	for _, in := range []string{
		"Hier das Ergebnis:\n```json\n{\"A\": 1}\n```",
		"Text davor {\"A\": 1} Text danach",
		"```json\n{\"A\": 1}",
	} {
		out.A = 0
		if err := extractJSON(in, &out); err != nil || out.A != 1 {
			t.Errorf("extractJSON(%q) = %v, A=%d", in, err, out.A)
		}
	}
	if err := extractJSON("gar nichts", &out); err == nil {
		t.Error("Fehler erwartet")
	}
}

// TestClaudeFindPrices prüft gegen einen Fake-Server, dass die Anfrage die
// Web-Werkzeuge mit Domain-Beschränkung enthält, pause_turn fortgesetzt wird
// und das JSON der Antwort gelesen wird.
func TestClaudeFindPrices(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		stop, text := "end_turn", "Ergebnis:\n```json\n{\"offers\":[{\"ingredient\":\"Spaghetti\",\"productName\":\"ja! Spaghetti\",\"price\":0.99,\"packageAmount\":500,\"packageUnit\":\"g\",\"packagesNeeded\":1,\"confidence\":\"hoch\"}],\"notFound\":[\"Safran\",\"\"]}\n```"
		if len(bodies) == 1 {
			stop, text = "pause_turn", "Ich suche weiter …"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
			"stop_reason": stop, "content": []map[string]any{{"type": "text", "text": text}},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer srv.Close()

	c := NewClaude(Config{APIKey: "test"})
	c.client = anthropic.NewClient(option.WithAPIKey("test"), option.WithBaseURL(srv.URL), option.WithMaxRetries(0))

	store, _ := StoreByID("rewe")
	res, err := c.FindPrices(context.Background(), store, []model.Ingredient{{Name: "Spaghetti", Quantity: 500, Unit: "g"}, {Name: "Safran", Quantity: 1, Unit: "Prise"}}, "10115")
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 {
		t.Fatalf("erwartet 2 Anfragen (pause_turn fortgesetzt), bekommen %d", len(bodies))
	}
	if msgs := bodies[1]["messages"].([]any); len(msgs) != 2 {
		t.Errorf("Fortsetzung sollte 2 Nachrichten enthalten, hat %d", len(msgs))
	}
	tools, _ := json.Marshal(bodies[0]["tools"])
	for _, want := range []string{`"web_search_20260209"`, `"web_fetch_20260209"`, `"rewe.de"`} {
		if !strings.Contains(string(tools), want) {
			t.Errorf("Tools enthalten %s nicht: %s", want, tools)
		}
	}
	if bodies[0]["model"] != "claude-opus-5-5" || bodies[0]["fallbacks"] != "default" {
		t.Errorf("model/fallbacks falsch: %v / %v", bodies[0]["model"], bodies[0]["fallbacks"])
	}
	if len(res.Offers) != 1 || res.Offers[0].StoreID != "rewe" || res.Offers[0].Price != 0.99 {
		t.Errorf("Angebote falsch: %+v", res.Offers)
	}
	if len(res.NotFound) != 1 || res.NotFound[0] != "Safran" {
		t.Errorf("NotFound falsch: %v", res.NotFound)
	}
}
