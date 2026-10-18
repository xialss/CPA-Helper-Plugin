package plugin

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"cpa-helper-plugin/internal/catalog"
	"cpa-helper-plugin/internal/snapshot"
)

func (a *App) serveModelCatalog(w http.ResponseWriter, r *http.Request) {
	s := a.currentCatalogStore()
	if r.Method == "GET" {
		p, h := s.View()
		w.Header().Set("ETag", strconv.Quote(strconv.FormatUint(h.Revision, 10)))
		jsonResponse(w, 200, map[string]any{"snapshot": p, "health": h})
		return
	}
	base, err := strconv.ParseUint(strings.Trim(r.Header.Get("If-Match"), "\""), 10, 64)
	if err != nil {
		s.RecordRejection("If-Match must contain the catalog revision (0 before first sync)")
		problem(w, 428, "precondition_required", "If-Match must contain the catalog revision (0 before first sync)")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" {
		s.RecordRejection("Idempotency-Key is required")
		problem(w, 400, "invalid", "Idempotency-Key is required")
		return
	}
	if _, _, err = a.currentStore().Engine(); err != nil {
		problem(w, 503, "policy_unavailable", err.Error())
		return
	}
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		s.RecordRejection("Cannot read catalog body")
		problem(w, 400, "invalid_model_catalog", "Cannot read catalog body")
		return
	}
	var p catalog.Snapshot
	var rev uint64
	if err = snapshot.Decode(raw, &p); err == nil {
		rev, err = s.Apply(p, base, key)
	}
	if err != nil {
		s.RecordRejection(err.Error())
		status, code := 400, "invalid_model_catalog"
		switch {
		case errors.Is(err, snapshot.ErrConflict):
			status, code = 409, "revision_conflict"
		case errors.Is(err, snapshot.ErrCatalogUnavailable):
			status, code = 503, "model_catalog_unavailable"
		case errors.Is(err, snapshot.ErrPersistenceUncertain):
			status, code = 500, "persistence_uncertain"
		case errors.Is(err, snapshot.ErrCatalogPersistence):
			status, code = 500, "persistence_failed"
		}
		problem(w, status, code, err.Error())
		return
	}
	jsonResponse(w, 200, map[string]uint64{"catalog_revision": rev})
}
