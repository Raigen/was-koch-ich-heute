package research

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
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
		usage := map[string]any{"input_tokens": 5000, "output_tokens": 400, "cache_read_input_tokens": 1000,
			"cache_creation_input_tokens": 0, "server_tool_use": map[string]any{"web_search_requests": 1, "web_fetch_requests": 1}}
		if len(bodies) == 1 {
			stop, text = "pause_turn", "Still searching …"
			usage = map[string]any{"input_tokens": 3000, "output_tokens": 200, "cache_read_input_tokens": 0,
				"cache_creation_input_tokens": 500, "server_tool_use": map[string]any{"web_search_requests": 2, "web_fetch_requests": 0}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "msg_1", "type": "message", "role": "assistant", "model": "claude-opus-5-5",
			"stop_reason": stop, "content": []map[string]any{{"type": "text", "text": text}},
			"usage": usage,
		})
	}))
	defer srv.Close()

	c := NewClaude(Config{APIKey: "test"})
	c.client = anthropic.NewClient(option.WithAPIKey("test"), option.WithBaseURL(srv.URL), option.WithMaxRetries(0))

	store, _ := StoreByID("rewe")
	res, usage, err := c.FindPrices(context.Background(), store, []model.Ingredient{{Name: "Spaghetti", Quantity: 500, Unit: "g"}, {Name: "Safran", Quantity: 1, Unit: "Prise"}}, "10115")
	if err != nil {
		t.Fatal(err)
	}
	if len(bodies) != 2 {
		t.Fatalf("expected 2 requests (pause_turn resumed), got %d", len(bodies))
	}
	// The usage is summed over the paused and the final response.
	want := model.Usage{Provider: "claude", Model: "claude-opus-5-5", Requests: 2, InputTokens: 8000, OutputTokens: 600,
		CacheReadTokens: 1000, CacheWriteTokens: 500, WebSearches: 3, WebFetches: 1,
		Calls: []model.ModelCall{
			{Model: "claude-opus-5-5", InputTokens: 3000, OutputTokens: 200, CacheWriteTokens: 500},
			{Model: "claude-opus-5-5", InputTokens: 5000, OutputTokens: 400, CacheReadTokens: 1000},
		}}
	if !reflect.DeepEqual(usage, want) {
		t.Errorf("usage = %+v, want %+v", usage, want)
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

// TestClaudeUsageOnRefusal checks that usage is reported together with an
// error, so that a refused research still shows what it cost.
func TestClaudeUsageOnRefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg_1","type":"message","role":"assistant","model":"claude-opus-5-5","stop_reason":"refusal",
			"content":[],"usage":{"input_tokens":700,"output_tokens":10,"server_tool_use":{"web_search_requests":1}}}`)
	}))
	defer srv.Close()

	c := NewClaude(Config{APIKey: "test"})
	c.client = anthropic.NewClient(option.WithAPIKey("test"), option.WithBaseURL(srv.URL), option.WithMaxRetries(0))
	_, usage, err := c.SearchRecipes(context.Background(), model.RecipeQuery{Query: "dal"})
	if err == nil {
		t.Fatal("expected a refusal error")
	}
	if usage.Requests != 1 || usage.InputTokens != 700 || usage.WebSearches != 1 {
		t.Errorf("usage = %+v", usage)
	}
}

// TestClaudeFallbackIterations checks that a response served by a refusal
// fallback is split into per-model calls via usage.iterations, and that
// iterations which do not add up are ignored.
func TestClaudeFallbackIterations(t *testing.T) {
	var resp anthropic.BetaMessage
	raw := `{"id":"m","type":"message","role":"assistant","model":"claude-opus-5-5","stop_reason":"end_turn","content":[],
		"usage":{"input_tokens":300,"output_tokens":30,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,
		"iterations":[
			{"type":"message","model":"claude-opus-5-5","input_tokens":100,"output_tokens":10},
			{"type":"compaction","input_tokens":999,"output_tokens":99},
			{"type":"fallback_message","model":"claude-opus-5","input_tokens":200,"output_tokens":20}]}}`
	if err := json.Unmarshal([]byte(raw), &resp); err != nil {
		t.Fatal(err)
	}
	want := []model.ModelCall{
		{Model: "claude-opus-5-5", InputTokens: 100, OutputTokens: 10},
		{Model: "claude-opus-5", InputTokens: 200, OutputTokens: 20},
	}
	if got := claudeCalls(&resp); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %+v, want %+v", got, want)
	}

	resp.Usage.InputTokens = 301 // iterations no longer add up: one call for the whole response
	want = []model.ModelCall{{Model: "claude-opus-5-5", InputTokens: 301, OutputTokens: 30}}
	if got := claudeCalls(&resp); !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %+v, want %+v", got, want)
	}
}

func TestDemoUsageIsZero(t *testing.T) {
	d := NewDemo()
	d.Delay = 0
	_, usage, err := d.SearchRecipes(context.Background(), model.RecipeQuery{Query: "dal", MaxRecipes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if usage.Provider != "demo" || usage.Requests != 0 || usage.TotalTokens() != 0 || usage.WebSearches != 0 {
		t.Errorf("demo usage = %+v", usage)
	}
}
