package plugin

import (
	"encoding/json"
	"io"
	"net/http"

	"cpa-helper-plugin/internal/snapshot"
)

func (a *App) serveTTLMetadata(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ExecutionIDs []string `json:"execution_ids"`
	}
	// A bounded batch keeps authenticated lookups proportional to queue batches
	// and prevents unbounded filesystem IO in the management callback.
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || len(request.ExecutionIDs) == 0 || len(request.ExecutionIDs) > 1000 {
		problem(w, 400, "invalid_execution_ids", "Expected 1 to 1000 canonical execution UUIDs")
		return
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		problem(w, 400, "invalid_execution_ids", "Expected one JSON document")
		return
	}
	seen := map[string]bool{}
	for _, id := range request.ExecutionIDs {
		if !snapshot.ValidExecutionID(id) || seen[id] {
			problem(w, 400, "invalid_execution_ids", "Invalid or duplicate execution UUID")
			return
		}
		seen[id] = true
	}
	a.ttl.mu.Lock()
	store := a.ttl.store
	a.ttl.mu.Unlock()
	if store == nil {
		problem(w, 503, "ttl_metadata_unavailable", "TTL metadata storage is unavailable")
		return
	}
	records := make([]snapshot.TTLMetadata, 0, len(request.ExecutionIDs))
	missing := make([]string, 0)
	for _, id := range request.ExecutionIDs {
		m, err := store.Get(id)
		if err != nil {
			problem(w, 503, "ttl_metadata_unavailable", err.Error())
			return
		}
		if m == nil {
			missing = append(missing, id)
		} else {
			records = append(records, *m)
		}
	}
	jsonResponse(w, 200, map[string]any{"metadata_version": "v1", "records": records, "missing_execution_ids": missing, "retention_hours": 24})
}
