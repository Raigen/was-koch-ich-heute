package research

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

const (
	defaultMistralModel   = "mistral-medium-latest"
	defaultMistralBaseURL = "https://api.mistral.ai"
)

// Mistral researches via the Mistral Conversations API (/v1/conversations)
// with the built-in web_search tool. There is no official Mistral SDK for Go,
// so the REST API is called directly.
//
// Returned errors are shown in the German UI, hence German messages.
type Mistral struct {
	apiKey          string
	baseURL         string
	model           string
	reasoningEffort string
	searchTool      string
	http            *http.Client
	maxRetries      int
}

// NewMistral creates a Mistral researcher.
func NewMistral(cfg Config) *Mistral {
	m := &Mistral{
		apiKey:          cfg.MistralAPIKey,
		baseURL:         strings.TrimRight(cfg.MistralBaseURL, "/"),
		model:           cfg.MistralModel,
		reasoningEffort: strings.ToLower(strings.TrimSpace(cfg.MistralReasoningEffort)),
		searchTool:      "web_search",
		http:            &http.Client{Timeout: 6 * time.Minute},
		maxRetries:      2,
	}
	if m.baseURL == "" {
		m.baseURL = defaultMistralBaseURL
	}
	if m.model == "" {
		m.model = defaultMistralModel
	}
	if strings.EqualFold(cfg.MistralWebSearch, "premium") {
		// Premium search additionally includes news sources.
		m.searchTool = "web_search_premium"
	}
	return m
}

func (m *Mistral) Name() string { return "Mistral (" + m.model + ")" }

const (
	mistralRecipeTools = "Use the web_search tool for your research."
	mistralPriceTools  = "Use the web_search tool and search specifically on the supermarket's websites " +
		"(e.g. with \"site:<domain> <product>\"). Results from other websites do not count as a shop price."
)

func (m *Mistral) SearchRecipes(ctx context.Context, q model.RecipeQuery) ([]model.Recipe, model.Usage, error) {
	text, usage, err := m.run(ctx, recipeSystemPrompt(mistralRecipeTools), recipeUserPrompt(q))
	if err != nil {
		return nil, usage, err
	}
	recipes, err := parseRecipes(text)
	return recipes, usage, err
}

func (m *Mistral) FindPrices(ctx context.Context, store model.Store, ingredients []model.Ingredient, postalCode string) (model.StorePrices, model.Usage, error) {
	text, usage, err := m.run(ctx, priceSystemPrompt(mistralPriceTools), priceUserPrompt(store, ingredients, postalCode))
	if err != nil {
		return model.StorePrices{StoreID: store.ID}, usage, err
	}
	sp, err := parsePrices(store, text)
	if err != nil {
		return sp, usage, err
	}
	// Mistral's web search cannot be restricted to domains, so links to other
	// sites are dropped and the confidence is lowered.
	for i := range sp.Offers {
		o := &sp.Offers[i]
		if o.URL != "" && !urlMatchesDomains(o.URL, store.Domains) {
			o.URL = ""
			if o.Confidence == "hoch" {
				o.Confidence = "mittel"
			}
		}
	}
	return sp, usage, nil
}

// --- Conversations API wire format ------------------------------------------

type mistralTool struct {
	Type string `json:"type"`
}

type mistralCompletionArgs struct {
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

type mistralRequest struct {
	Model          string                 `json:"model"`
	Inputs         string                 `json:"inputs"`
	Instructions   string                 `json:"instructions,omitempty"`
	Tools          []mistralTool          `json:"tools,omitempty"`
	CompletionArgs *mistralCompletionArgs `json:"completion_args,omitempty"`
	Stream         bool                   `json:"stream"`
	// Store=false: the conversation is not stored server-side.
	Store bool `json:"store"`
}

type mistralChunk struct {
	Type  string `json:"type"` // "text", "tool_reference", …
	Text  string `json:"text"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

// mistralContent is either a string or a list of chunks.
type mistralContent []mistralChunk

func (c *mistralContent) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*c = mistralContent{{Type: "text", Text: s}}
		return nil
	}
	var chunks []mistralChunk
	if err := json.Unmarshal(data, &chunks); err != nil {
		return err
	}
	*c = chunks
	return nil
}

type mistralResponse struct {
	ConversationID string `json:"conversation_id"`
	Outputs        []struct {
		Type    string         `json:"type"` // "message.output", "tool.execution", …
		Name    string         `json:"name"` // tool name of "tool.execution" entries
		Content mistralContent `json:"content"`
	} `json:"outputs"`
	Usage mistralUsage `json:"usage"`
}

// mistralUsage is the usage object of a conversation response, see
// docs.mistral.ai (agent tools: websearch; prompt caching).
type mistralUsage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	// ConnectorTokens are the tokens of tool results (e.g. web search hits),
	// counted separately from prompt_tokens.
	ConnectorTokens int64 `json:"connector_tokens"`
	// Connectors counts the tool calls per tool, e.g. {"web_search": 2}.
	Connectors          map[string]float64 `json:"connectors"`
	PromptTokensDetails struct {
		CachedTokens int64 `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

// usage converts the Mistral response into the common usage format.
// prompt_tokens includes cached tokens; connector tokens are counted as input.
func (m *Mistral) usage(resp mistralResponse) model.Usage {
	u := resp.Usage
	cached := min(u.PromptTokensDetails.CachedTokens, u.PromptTokens)
	searches := 0
	for name, n := range u.Connectors {
		if strings.HasPrefix(name, "web_search") {
			searches += int(n)
		}
	}
	if len(u.Connectors) == 0 {
		// Older responses without the connectors map: count the tool entries.
		for _, out := range resp.Outputs {
			if out.Type == "tool.execution" && strings.HasPrefix(out.Name, "web_search") {
				searches++
			}
		}
	}
	return model.Usage{
		Provider:        "mistral",
		Model:           m.model,
		Requests:        1,
		InputTokens:     u.PromptTokens - cached + u.ConnectorTokens,
		OutputTokens:    u.CompletionTokens,
		CacheReadTokens: cached,
		WebSearches:     searches,
	}
}

func (m *Mistral) run(ctx context.Context, system, prompt string) (string, model.Usage, error) {
	usage := model.Usage{Provider: "mistral", Model: m.model}
	if m.apiKey == "" {
		return "", usage, notBilled(errors.New("Mistral API: MISTRAL_API_KEY ist nicht gesetzt"))
	}
	req := mistralRequest{
		Model:        m.model,
		Inputs:       prompt,
		Instructions: system,
		Tools:        []mistralTool{{Type: m.searchTool}},
		Stream:       false,
		Store:        false,
	}
	if m.reasoningEffort != "" {
		req.CompletionArgs = &mistralCompletionArgs{ReasoningEffort: m.reasoningEffort}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", usage, notBilled(err)
	}

	var resp mistralResponse
	// mayBeBilled is set once an attempt failed in a way where the API may
	// have done work (network error, timeout, server error).
	mayBeBilled := false
	for attempt := 0; ; attempt++ {
		status, raw, err := m.post(ctx, "/v1/conversations", body)
		if err != nil || (status >= 300 && !rejectedBeforeBilling(status)) {
			mayBeBilled = true
		}
		retryable := err != nil || status == http.StatusTooManyRequests || status >= 500
		if retryable && attempt < m.maxRetries && ctx.Err() == nil {
			if err := sleep(ctx, time.Duration(1<<attempt)*2*time.Second); err != nil {
				return "", usage, err
			}
			continue
		}
		if err != nil {
			return "", usage, fmt.Errorf("Mistral API: %w", err)
		}
		var httpErr error
		switch {
		case status == http.StatusUnauthorized || status == http.StatusForbidden:
			httpErr = fmt.Errorf("Mistral API: Zugang verweigert – MISTRAL_API_KEY prüfen (%d)", status)
		case status == http.StatusTooManyRequests:
			httpErr = errors.New("Mistral API: Rate-Limit erreicht, bitte später erneut versuchen")
		case status >= 300:
			httpErr = fmt.Errorf("Mistral API: HTTP %d: %s", status, truncate(string(raw), 300))
		}
		if httpErr != nil {
			if !mayBeBilled {
				httpErr = notBilled(httpErr)
			}
			return "", usage, httpErr
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return "", usage, fmt.Errorf("Mistral API: unerwartete Antwort: %w", err)
		}
		// Failed attempts report no usage; only the successful response counts.
		// If an earlier attempt may have been billed, the usage is a lower bound.
		usage = m.usage(resp)
		usage.Incomplete = mayBeBilled
		break
	}

	var sb strings.Builder
	for _, out := range resp.Outputs {
		if out.Type != "message.output" {
			continue
		}
		for _, ch := range out.Content {
			if ch.Type == "text" {
				sb.WriteString(ch.Text)
			}
		}
	}
	if sb.Len() == 0 {
		return "", usage, errors.New("Mistral API: Antwort enthielt keinen Text")
	}
	return sb.String(), usage, nil
}

func (m *Mistral) post(ctx context.Context, path string, body []byte) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := m.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	return res.StatusCode, raw, err
}

func urlMatchesDomains(rawURL string, domains []string) bool {
	host := strings.ToLower(rawURL)
	host = strings.TrimPrefix(strings.TrimPrefix(host, "https://"), "http://")
	if i := strings.IndexAny(host, "/?#:"); i >= 0 {
		host = host[:i]
	}
	for _, d := range domains {
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var _ Researcher = (*Mistral)(nil)
