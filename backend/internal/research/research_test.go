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
		"Here is the result:\n```json\n{\"A\": 1}\n```",
		"Text before {\"A\": 1} text after",
		"```json\n{\"A\": 1}",
	} {
		out.A = 0
		if err := extractJSON(in, &out); err != nil || out.A != 1 {
			t.Errorf("extractJSON(%q) = %v, A=%d", in, err, out.A)
		}
	}
	if err := extractJSON("nothing at all", &out); err == nil {
		t.Error("expected an error")
	}
}

// TestClaudeFindPrices checks against a fake server that the request contains
// the web tools with domain restriction, that pause_turn is resumed and that
// the JSON answer is parsed.
func TestClaudeFindPrices(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		stop, text := "end_turn", "Result:\n```json\n{\"offers\":[{\"ingredient\":\"Spaghetti\",\"productName\":\"ja! Spaghetti\",\"price\":0.99,\"packageAmount\":500,\"packageUnit\":\"g\",\"packagesNeeded\":1,\"confidence\":\"hoch\"}],\"notFound\":[\"Safran\",\"\"]}\n```"
		if len(bodies) == 1 {
			stop, text = "pause_turn", "Still searching …"
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
		t.Fatalf("expected 2 requests (pause_turn resumed), got %d", len(bodies))
	}
	if msgs := bodies[1]["messages"].([]any); len(msgs) != 2 {
		t.Errorf("continuation should contain 2 messages, has %d", len(msgs))
	}
	tools, _ := json.Marshal(bodies[0]["tools"])
	for _, want := range []string{`"web_search_20260209"`, `"web_fetch_20260209"`, `"rewe.de"`} {
		if !strings.Contains(string(tools), want) {
			t.Errorf("tools do not contain %s: %s", want, tools)
		}
	}
	if bodies[0]["model"] != "claude-opus-5-5" || bodies[0]["fallbacks"] != "default" {
		t.Errorf("wrong model/fallbacks: %v / %v", bodies[0]["model"], bodies[0]["fallbacks"])
	}
	if len(res.Offers) != 1 || res.Offers[0].StoreID != "rewe" || res.Offers[0].Price != 0.99 {
		t.Errorf("wrong offers: %+v", res.Offers)
	}
	if len(res.NotFound) != 1 || res.NotFound[0] != "Safran" {
		t.Errorf("wrong NotFound: %v", res.NotFound)
	}
}
