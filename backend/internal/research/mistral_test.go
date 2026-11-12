package research

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

// TestMistralFindPrices checks against a fake server: the Conversations API
// request format, parsing chunked content, retrying and dropping links to
// foreign domains.
func TestMistralFindPrices(t *testing.T) {
	var got map[string]any
	var auth string
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 { // first attempt: server error -> retry
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path != "/v1/conversations" {
			t.Errorf("path %s", r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"conversation.response","conversation_id":"conv_1","outputs":[
		  {"object":"entry","type":"tool.execution","name":"web_search"},
		  {"object":"entry","type":"message.output","role":"assistant","content":[
		    {"type":"text","text":"Found:\n`+"```json"+`\n{\"offers\":[{\"ingredient\":\"Reis\",\"productName\":\"ja! Langkornreis\",\"price\":1.29,"},
		    {"type":"tool_reference","tool":"web_search","title":"REWE","url":"https://shop.rewe.de/p/reis"},
		    {"type":"text","text":"\"packageAmount\":1,\"packageUnit\":\"kg\",\"url\":\"https://shop.rewe.de/p/reis\",\"confidence\":\"hoch\"},{\"ingredient\":\"Milch\",\"productName\":\"Milch\",\"price\":0.99,\"url\":\"https://preisvergleich.example/milch\",\"confidence\":\"hoch\"}],\"notFound\":[]}\n`+"```"+`"}
		  ]}],"usage":{"prompt_tokens":1200,"completion_tokens":300,"total_tokens":8612,"connector_tokens":7112,
		  "connectors":{"web_search":3},"prompt_tokens_details":{"cached_tokens":1024}}}`)
	}))
	defer srv.Close()

	m := NewMistral(Config{MistralAPIKey: "key", MistralBaseURL: srv.URL, MistralReasoningEffort: "low"})
	m.maxRetries = 1
	store, _ := StoreByID("rewe")
	sp, usage, err := m.FindPrices(context.Background(), store, []model.Ingredient{{Name: "Reis", Quantity: 250, Unit: "g"}, {Name: "Milch", Quantity: 1, Unit: "l"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	// prompt_tokens includes the cached tokens; connector tokens count as input.
	want := model.Usage{Provider: "mistral", Model: "mistral-medium-latest", Requests: 1,
		InputTokens: 1200 - 1024 + 7112, OutputTokens: 300, CacheReadTokens: 1024, WebSearches: 3}
	if !reflect.DeepEqual(usage, want) {
		t.Errorf("usage = %+v, want %+v", usage, want)
	}
	if calls != 2 {
		t.Errorf("expected 2 calls (1 retry), got %d", calls)
	}
	if auth != "Bearer key" {
		t.Errorf("Authorization = %q", auth)
	}
	if got["model"] != "mistral-medium-latest" || got["store"] != false || got["stream"] != false || got["instructions"] == "" {
		t.Errorf("wrong request: %v", got)
	}
	if tools := got["tools"].([]any); tools[0].(map[string]any)["type"] != "web_search" {
		t.Errorf("tools = %v", tools)
	}
	if ca := got["completion_args"].(map[string]any); ca["reasoning_effort"] != "low" {
		t.Errorf("completion_args = %v", ca)
	}
	if len(sp.Offers) != 2 || sp.Offers[0].Price != 1.29 || sp.Offers[0].URL == "" {
		t.Fatalf("wrong offers: %+v", sp.Offers)
	}
	if sp.Offers[1].URL != "" || sp.Offers[1].Confidence != "mittel" {
		t.Errorf("foreign domain should have been dropped: %+v", sp.Offers[1])
	}
}

func TestMistralStringContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"conversation_id":"c","outputs":[
		  {"type":"tool.execution","name":"web_search"},{"type":"tool.execution","name":"web_search"},
		  {"type":"message.output","content":"{\"recipes\":[{\"title\":\"Dal\",\"servings\":2}]}"}],
		  "usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120}}`)
	}))
	defer srv.Close()
	m := NewMistral(Config{MistralAPIKey: "key", MistralBaseURL: srv.URL})
	rs, usage, err := m.SearchRecipes(context.Background(), model.RecipeQuery{Query: "dal", Servings: 2, MaxRecipes: 1})
	if err != nil || len(rs) != 1 || rs[0].Title != "Dal" {
		t.Fatalf("rs=%+v err=%v", rs, err)
	}
	// Without a connectors map, the web searches are counted from the outputs.
	if usage.InputTokens != 100 || usage.OutputTokens != 20 || usage.WebSearches != 2 || usage.Requests != 1 {
		t.Errorf("usage = %+v", usage)
	}
}

// TestMistralUsageOnError checks that a failed research still reports the
// usage of the successful API call.
func TestMistralUsageOnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"outputs":[],"usage":{"prompt_tokens":50,"completion_tokens":0,"connectors":{"web_search_premium":1}}}`)
	}))
	defer srv.Close()
	m := NewMistral(Config{MistralAPIKey: "key", MistralBaseURL: srv.URL, MistralWebSearch: "premium"})
	_, usage, err := m.SearchRecipes(context.Background(), model.RecipeQuery{Query: "dal"})
	if err == nil {
		t.Fatal("expected an error for a response without text")
	}
	if usage.InputTokens != 50 || usage.WebSearches != 1 {
		t.Errorf("usage = %+v", usage)
	}
}

// TestMistralNotBilled checks that a request rejected with 4xx is marked as
// not billed, unless an earlier attempt failed with a server error.
func TestMistralNotBilled(t *testing.T) {
	run := func(statuses ...int) error {
		calls := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(statuses[min(calls, len(statuses)-1)])
			calls++
		}))
		defer srv.Close()
		m := NewMistral(Config{MistralAPIKey: "key", MistralBaseURL: srv.URL})
		m.maxRetries = len(statuses) - 1
		_, usage, err := m.SearchRecipes(context.Background(), model.RecipeQuery{Query: "dal"})
		if err == nil || usage.Requests != 0 {
			t.Fatalf("statuses %v: err=%v usage=%+v", statuses, err, usage)
		}
		return err
	}
	if err := run(http.StatusUnauthorized); !IsNotBilled(err) {
		t.Errorf("401 should not be billed: %v", err)
	}
	if err := run(http.StatusTooManyRequests); !IsNotBilled(err) {
		t.Errorf("429 should not be billed: %v", err)
	}
	if err := run(http.StatusInternalServerError); IsNotBilled(err) {
		t.Errorf("500 may be billed: %v", err)
	}
	if err := run(http.StatusServiceUnavailable, http.StatusTooManyRequests); IsNotBilled(err) {
		t.Errorf("a 429 after a server error may be billed: %v", err)
	}
	_, _, err := NewMistral(Config{MistralAPIKey: ""}).SearchRecipes(context.Background(), model.RecipeQuery{Query: "dal"})
	if !IsNotBilled(err) {
		t.Errorf("missing key sends no request: %v", err)
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
		t.Error("expected an error without MISTRAL_API_KEY")
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
