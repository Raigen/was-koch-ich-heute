package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/raigen/was-koch-ich-heute/backend/internal/mealie"
	"github.com/raigen/was-koch-ich-heute/backend/internal/model"
)

const (
	mealiePerPage = 24
	mealieTimeout = 20 * time.Second
)

// handleMealieSearch lists recipes from Mealie: GET /api/mealie/recipes?query=…&page=…
func (s *Server) handleMealieSearch(w http.ResponseWriter, r *http.Request) {
	if !s.requireMealie(w) {
		return
	}
	query := strings.TrimSpace(r.URL.Query().Get("query"))
	if utf8.RuneCountInString(query) > 100 {
		writeError(w, http.StatusBadRequest, "Suchbegriff ist zu lang.")
		return
	}
	page := 1
	if v := r.URL.Query().Get("page"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 1000 {
			writeError(w, http.StatusBadRequest, "Ungültige Seitenzahl.")
			return
		}
		page = n
	}

	ctx, cancel := context.WithTimeout(r.Context(), mealieTimeout)
	defer cancel()
	res, err := s.mealie.Search(ctx, query, page, mealiePerPage)
	if errors.Is(err, mealie.ErrNotFound) {
		// The recipe list itself is missing: most likely a wrong base URL.
		slog.Error("mealie search failed", "err", err)
		writeError(w, http.StatusBadGateway, "Mealie-API nicht gefunden. Bitte MEALIE_URL prüfen.")
		return
	}
	if err != nil {
		writeMealieError(w, "mealie search failed", err)
		return
	}
	out := model.MealieRecipePage{
		Recipes:    make([]model.MealieRecipeSummary, 0, len(res.Items)),
		Page:       page,
		TotalPages: res.TotalPages,
		Total:      res.Total,
	}
	for _, item := range res.Items {
		if !mealie.ValidSlug(item.Slug) {
			continue
		}
		sum := mealie.Summary(item)
		sum.ImageURL = mealie.ImagePath(item)
		out.Recipes = append(out.Recipes, sum)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleMealieRecipe loads one recipe from Mealie and scales it to the
// requested servings: GET /api/mealie/recipes/{slug}?servings=…
func (s *Server) handleMealieRecipe(w http.ResponseWriter, r *http.Request) {
	if !s.requireMealie(w) {
		return
	}
	slug := r.PathValue("slug")
	if !mealie.ValidSlug(slug) {
		writeError(w, http.StatusBadRequest, "Ungültiger Rezept-Name.")
		return
	}
	servings := 0
	if v := r.URL.Query().Get("servings"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 20 {
			writeError(w, http.StatusBadRequest, "Portionen müssen zwischen 1 und 20 liegen.")
			return
		}
		servings = n
	}

	ctx, cancel := context.WithTimeout(r.Context(), mealieTimeout)
	defer cancel()
	mr, err := s.mealie.Recipe(ctx, slug)
	if err != nil {
		writeMealieError(w, "mealie recipe failed", err)
		return
	}
	recipe := mealie.ToRecipe(mr, servings)
	recipe.ImageURL = mealie.ImagePath(mr.RecipeSummary)
	// The link back to Mealie needs the group slug. Without it the recipe is
	// still usable, so a failure only drops the link.
	if group, err := s.mealie.GroupSlug(ctx); err != nil {
		slog.Warn("mealie group lookup failed", "err", err)
	} else if mealie.ValidSlug(mr.Slug) {
		recipe.MealieURL = s.mealie.RecipeURL(group, mr.Slug)
	}
	writeJSON(w, http.StatusOK, recipe)
}

// handleMealieImage proxies a recipe image so that the browser needs neither
// the token nor a direct connection to Mealie: GET /api/mealie/images/{id}
func (s *Server) handleMealieImage(w http.ResponseWriter, r *http.Request) {
	if !s.requireMealie(w) {
		return
	}
	id := r.PathValue("id")
	if !mealie.IsUUID(id) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), mealieTimeout)
	defer cancel()
	img, err := s.mealie.Image(ctx, id)
	if err != nil {
		if !errors.Is(err, mealie.ErrNotFound) {
			slog.Warn("mealie image failed", "err", err)
		}
		http.Error(w, "image not available", http.StatusNotFound)
		return
	}
	defer img.Body.Close()
	w.Header().Set("Content-Type", img.ContentType)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, io.LimitReader(img.Body, mealie.MaxImageBytes))
}

func (s *Server) requireMealie(w http.ResponseWriter) bool {
	if s.mealie == nil {
		writeError(w, http.StatusNotFound, "Mealie ist nicht konfiguriert.")
		return false
	}
	return true
}

// writeMealieError logs err and returns a German message for the UI. Errors
// never contain the token.
func writeMealieError(w http.ResponseWriter, logMsg string, err error) {
	slog.Error(logMsg, "err", err)
	var se *mealie.StatusError
	switch {
	case errors.Is(err, mealie.ErrUnauthorized):
		writeError(w, http.StatusBadGateway, "Mealie hat den API-Token abgelehnt. Bitte MEALIE_API_TOKEN prüfen.")
	case errors.Is(err, mealie.ErrNotFound):
		writeError(w, http.StatusNotFound, "Rezept in Mealie nicht gefunden.")
	case errors.Is(err, mealie.ErrUnreachable):
		writeError(w, http.StatusBadGateway, "Mealie ist nicht erreichbar. Bitte MEALIE_URL prüfen.")
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, "Mealie antwortet nicht (Zeitüberschreitung).")
	case errors.As(err, &se):
		writeError(w, http.StatusBadGateway, "Mealie hat mit einem Fehler geantwortet (HTTP "+strconv.Itoa(se.Code)+").")
	default:
		writeError(w, http.StatusBadGateway, "Antwort von Mealie konnte nicht gelesen werden.")
	}
}
