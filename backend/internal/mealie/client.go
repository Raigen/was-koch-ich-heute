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
	// Timeout per request (default 15 s). It is applied as a context deadline,
	// so an exceeded timeout is reported as context.DeadlineExceeded rather
	// than as an unreachable instance.
	Timeout time.Duration
}

// Client is a small Mealie REST client. It is safe for concurrent use.
type Client struct {
	base      *url.URL
	publicURL string
	token     string
	timeout   time.Duration
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
		timeout:   cfg.Timeout,
		// No http.Client.Timeout: its error would be indistinguishable from an
		// unreachable host. The per-request context deadline is used instead.
		http: &http.Client{},
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
// 2xx status. Other statuses are converted to the package errors. A timeout
// (the client's own or the caller's) is returned as context.DeadlineExceeded.
// The request deadline lasts until the response body is closed.
func (c *Client) do(ctx context.Context, path string, q url.Values) (*http.Response, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint(path, q), nil)
	if err != nil {
		cancel()
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	resp, err := c.http.Do(req)
	if err != nil {
		ctxErr := ctx.Err() // read before cancel(), which would set it
		cancel()
		if ctxErr != nil {
			return nil, ctxErr
		}
		// url.Error contains method, URL and cause, never the header.
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		resp.Body = &cancelOnClose{ReadCloser: resp.Body, cancel: cancel}
		return resp, nil
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	resp.Body.Close()
	cancel()
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrUnauthorized
	case http.StatusNotFound:
		return nil, ErrNotFound
	default:
		return nil, &StatusError{Code: resp.StatusCode}
	}
}

// cancelOnClose releases the request context when the body is closed.
type cancelOnClose struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *cancelOnClose) Close() error {
	err := b.ReadCloser.Close()
	b.cancel()
	return err
}

func (c *Client) getJSON(ctx context.Context, path string, q url.Values, out any) error {
	resp, err := c.do(ctx, path, q)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(out); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return err
		}
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

// Image is a recipe image loaded from Mealie.
type Image struct {
	Data        []byte
	ContentType string
}

// ErrImageTooLarge means an image exceeds MaxImageBytes.
var ErrImageTooLarge = errors.New("mealie: image too large")

// imageTypes are the content types passed through to the browser. Anything
// else (notably image/svg+xml, which can carry script) is served as WebP,
// the format Mealie stores its recipe images in.
var imageTypes = map[string]bool{"image/webp": true, "image/png": true, "image/jpeg": true, "image/gif": true, "image/avif": true}

// Image fetches the medium-sized image ("min-original.webp") of a recipe.
// recipeID must be a validated recipe UUID. Images larger than MaxImageBytes
// are rejected with ErrImageTooLarge instead of being truncated.
func (c *Client) Image(ctx context.Context, recipeID string) (Image, error) {
	resp, err := c.do(ctx, "/api/media/recipes/"+url.PathEscape(recipeID)+"/images/min-original.webp", nil)
	if err != nil {
		return Image{}, err
	}
	defer resp.Body.Close()
	if resp.ContentLength > MaxImageBytes {
		return Image{}, ErrImageTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxImageBytes+1))
	if err != nil {
		return Image{}, err
	}
	if len(data) > MaxImageBytes {
		return Image{}, ErrImageTooLarge
	}
	ct := strings.ToLower(strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0]))
	if !imageTypes[ct] {
		ct = "image/webp"
	}
	return Image{Data: data, ContentType: ct}, nil
}
