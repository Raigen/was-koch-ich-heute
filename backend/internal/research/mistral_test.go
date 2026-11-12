package research

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

// TestMistralFindPrices prüft gegen einen Fake-Server das Request-Format der
// Conversations API, das Lesen von String- und Chunk-Inhalten sowie das
// Verwerfen von Links auf fremde Domains.
func TestMistralFindPrices(t *testing.T) {
	var got map[string]any
	var auth string
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 { // erster Versuch: Serverfehler -> Retry
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path != "/v1/conversations" {
			t.Errorf("Pfad %s", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"conversation.response","conversation_id":"conv_1","outputs":[
		  {"object":"entry","type":"tool.execution","name":"web_search"},
		  {"object":"entry","type":"message.output","role":"assistant","content":[
		    {"type":"text","text":"Gefunden:\n`+"```json"+`\n{\"offers\":[{\"ingredient\":\"Reis\",\"productName\":\"ja! Langkornreis\",\"price\":1.29,"},
		    {"type":"tool_reference","tool":"web_search","title":"REWE","url":"https://shop.rewe.de/p/reis"},
		    {"type":"text","text":"\"packageAmount\":1,\"packageUnit\":\"kg\",\"url\":\"https://shop.rewe.de/p/reis\",\"confidence\":\"hoch\"},{\"ingredient\":\"Milch\",\"productName\":\"Milch\",\"price\":0.99,\"url\":\"https://preisvergleich.example/milch\",\"confidence\":\"hoch\"}],\"notFound\":[]}\n`+"```"+`"}
		  ]}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer srv.Close()

	m := NewMistral(Config{MistralAPIKey: "key", MistralBaseURL: srv.URL, MistralReasoningEffort: "low"})
	m.maxRetries = 1
	store, _ := StoreByID("rewe")
	sp, err := m.FindPrices(context.Background(), store, []model.Ingredient{{Name: "Reis", Quantity: 250, Unit: "g"}, {Name: "Milch", Quantity: 1, Unit: "l"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("erwartet 2 Aufrufe (1 Retry), bekommen %d", calls)
	}
	if auth != "Bearer key" {
		t.Errorf("Authorization = %q", auth)
	}
	if got["model"] != "mistral-medium-latest" || got["store"] != false || got["stream"] != false || got["instructions"] == "" {
		t.Errorf("Request falsch: %v", got)
	}
	if tools := got["tools"].([]any); tools[0].(map[string]any)["type"] != "web_search" {
		t.Errorf("tools = %v", tools)
	}
	if ca := got["completion_args"].(map[string]any); ca["reasoning_effort"] != "low" {
		t.Errorf("completion_args = %v", ca)
	}
	if len(sp.Offers) != 2 || sp.Offers[0].Price != 1.29 || sp.Offers[0].URL == "" {
		t.Fatalf("Angebote falsch: %+v", sp.Offers)
	}
	if sp.Offers[1].URL != "" || sp.Offers[1].Confidence != "mittel" {
		t.Errorf("fremde Domain hätte verworfen werden müssen: %+v", sp.Offers[1])
	}
}

func TestMistralStringContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"conversation_id":"c","outputs":[{"type":"message.output","content":"{\"recipes\":[{\"title\":\"Dal\",\"servings\":2}]}"}]}`)
	}))
	defer srv.Close()
	m := NewMistral(Config{MistralAPIKey: "key", MistralBaseURL: srv.URL})
	rs, err := m.SearchRecipes(context.Background(), model.RecipeQuery{Query: "dal", Servings: 2, MaxRecipes: 1})
	if err != nil || len(rs) != 1 || rs[0].Title != "Dal" {
		t.Fatalf("rs=%+v err=%v", rs, err)
	}
}

func TestNewProviderSelection(t *testing.T) {
	for _, c := range []struct {
		cfg  Config
		want string
	}{
		{Config{}, "*research.Demo"},
		{Config{MistralAPIKey: "m"}, "*research.Mistral"},
		{Config{APIKey: "a", MistralAPIKey: "m"}, "*research.Claude"},
		{Config{Provider: "mistral", APIKey: "a", MistralAPIKey: "m"}, "*research.Mistral"},
	} {
		r, err := New(c.cfg)
		if err != nil {
			t.Fatal(err)
		}
		if got := typeName(r); got != c.want {
			t.Errorf("New(%+v) = %s, want %s", c.cfg, got, c.want)
		}
	}
	if _, err := New(Config{Provider: "mistral"}); err == nil {
		t.Error("Fehler ohne MISTRAL_API_KEY erwartet")
	}
}

func typeName(v any) string {
	switch v.(type) {
	case *Demo:
		return "*research.Demo"
	case *Mistral:
		return "*research.Mistral"
	case *Claude:
		return "*research.Claude"
	}
	return "?"
}
