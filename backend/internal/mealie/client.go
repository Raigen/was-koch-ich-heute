// Package mealie reads recipes from a self-hosted Mealie instance
// (https://mealie.io) via its REST API and maps them to model.Recipe.
//
// Checked against the Mealie source (v3.x, "mealie-next" branch):
//   - GET /api/recipes?search=…&page=…&perPage=…  returns a pagination object
//     {page, per_page, total, total_pages, items: [RecipeSummary]}
//   - GET /api/recipes/{slug}                       returns a RecipeOut (camelCase)
//   - GET /api/groups/self                          returns the group with its slug
//   - GET /api/media/recipes/{id}/images/{file}     returns a recipe image
//
// The API token is only ever sent in the Authorization header. It never
// appears in URLs, logs or error messages.
package mealie

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Errors returned by the client. The API layer maps them to German messages.
var (
	// ErrUnauthorized means Mealie rejected the token (HTTP 401 or 403).
	ErrUnauthorized = errors.New("mealie: token rejected")
	// ErrNotFound means the recipe or image does not exist (HTTP 404).
	ErrNotFound = errors.New("mealie: not found")
	// ErrUnreachable means the instance could not be reached at all.
	ErrUnreachable = errors.New("mealie: instance unreachable")
)

// StatusError is returned for any other non-2xx response.
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return fmt.Sprintf("mealie: unexpected HTTP status %d", e.Code) }

// Config configures the client.
type Config struct {
	// BaseURL is the URL the backend uses to reach Mealie, e.g. http://mealie:9000.
	BaseURL string
	// PublicURL is the URL the browser uses for "open in Mealie" links.
	// Defaults to BaseURL.
	PublicURL string
	// Token is a Mealie API token (user profile → API tokens).
	Token string
	// Timeout per request (default 15 s).
	Timeout time.Duration
}

// Client is a small Mealie REST client. It is safe for concurrent use.
type Client struct {
	base      *url.URL
	publicURL string
	token     string
	http      *http.Client

	mu        sync.Mutex
	groupSlug string
}

// maxResponseBytes limits JSON responses read from Mealie.
const maxResponseBytes = 4 << 20

// MaxImageBytes limits images proxied from Mealie.
const MaxImageBytes = 8 << 20

// New validates the configuration and creates a client.
func New(cfg Config) (*Client, error) {
	base, err := parseBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("MEALIE_URL: %w", err)
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("MEALIE_API_TOKEN is required when MEALIE_URL is set")
	}
	public := base.String()
	if strings.TrimSpace(cfg.PublicURL) != "" {
		p, err := parseBaseURL(cfg.PublicURL)
		if err != nil {
			return nil, fmt.Errorf("MEALIE_PUBLIC_URL: %w", err)
		}
		public = p.String()
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 15 * time.Second
	}
	return &Client{
		base:      base,
		publicURL: public,
		token:     strings.TrimSpace(cfg.Token),
		http:      &http.Client{Timeout: cfg.Timeout},
	}, nil
}

func parseBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, errors.New("invalid URL")
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("must be an absolute http(s) URL, e.g. http://mealie.local:9000")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return nil, errors.New("must not contain credentials, a query or a fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = ""
	return u, nil
}

// endpoint builds an absolute URL below the base URL. Path segments must
// already be escaped.
func (c *Client) endpoint(path string, q url.Values) string {
	u := *c.base
	u.Path = c.base.Path + path
	u.RawPath = ""
	if q != nil {
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// do performs an authenticated GET request and returns the response for a
// 2xx status. Other statuses are converted to the package errors.
func (c *Client) do(ctx context.Context, path string, q url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint(path, q), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// url.Error contains method, URL and cause, never the header.
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp, nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrUnauthorized
	case http.StatusNotFound:
		return nil, ErrNotFound
	default:
		return nil, &StatusError{Code: resp.StatusCode}
	}
}

func (c *Client) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	resp, err := c.do(ctx, path, q)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(out); err != nil {
		return fmt.Errorf("mealie: invalid JSON response: %w", err)
	}
	return nil
}

// Search lists recipes matching query (all recipes if empty), sorted by name.
// page starts at 1.
func (c *Client) Search(ctx context.Context, query string, page, perPage int) (Page, error) {
	q := url.Values{}
	q.Set("page", strconv.Itoa(page))
	q.Set("perPage", strconv.Itoa(perPage))
	if query != "" {
		// Mealie ranks search results by relevance; an explicit orderBy would
		// override that ranking.
		q.Set("search", query)
	} else {
		q.Set("orderBy", "name")
		q.Set("orderDirection", "asc")
	}
	var p Page
	if err := c.getJSON(ctx, "/api/recipes", q, &p); err != nil {
		return Page{}, err
	}
	return p, nil
}

// Recipe loads a full recipe by slug (Mealie also accepts the recipe ID).
func (c *Client) Recipe(ctx context.Context, slug string) (Recipe, error) {
	var r Recipe
	if err := c.getJSON(ctx, "/api/recipes/"+url.PathEscape(slug), nil, &r); err != nil {
		return Recipe{}, err
	}
	return r, nil
}

// GroupSlug returns the slug of the token user's group, which Mealie uses in
// recipe page URLs (/g/{group}/r/{recipe}). It is cached after the first
// successful call.
func (c *Client) GroupSlug(ctx context.Context) (string, error) {
	c.mu.Lock()
	slug := c.groupSlug
	c.mu.Unlock()
	if slug != "" {
		return slug, nil
	}
	var g struct {
		Slug string `json:"slug"`
	}
	if err := c.getJSON(ctx, "/api/groups/self", nil, &g); err != nil {
		return "", err
	}
	if g.Slug == "" {
		return "", errors.New("mealie: group without slug")
	}
	c.mu.Lock()
	c.groupSlug = g.Slug
	c.mu.Unlock()
	return g.Slug, nil
}

// RecipeURL returns the browser link to a recipe in Mealie.
func (c *Client) RecipeURL(groupSlug, recipeSlug string) string {
	return c.publicURL + "/g/" + url.PathEscape(groupSlug) + "/r/" + url.PathEscape(recipeSlug)
}

// Image is a recipe image streamed from Mealie. The caller must close Body.
type Image struct {
	Body        io.ReadCloser
	ContentType string
}

// Image fetches the medium-sized image ("min-original.webp") of a recipe.
// recipeID must be a validated recipe UUID.
func (c *Client) Image(ctx context.Context, recipeID string) (Image, error) {
	resp, err := c.do(ctx, "/api/media/recipes/"+url.PathEscape(recipeID)+"/images/min-original.webp", nil)
	if err != nil {
		return Image{}, err
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		ct = "image/webp"
	}
	return Image{Body: resp.Body, ContentType: ct}, nil
}
