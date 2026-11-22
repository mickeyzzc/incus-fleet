package httpapi

import (
	"encoding/csv"
	"encoding/json"
	"net/http"
	"strings"
)

func (a *API) exportHostAudit(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "host audit is not enabled")
		return
	}
	entries, err := a.store.ExportHostAudit(r.Context(),
		strings.TrimSpace(r.URL.Query().Get("cluster")),
		strings.TrimSpace(r.URL.Query().Get("member")))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format"))) {
	case "", "jsonl":
		w.Header().Set("Content-Type", "application/x-ndjson")
		encoder := json.NewEncoder(w)
		for _, entry := range entries {
			_ = encoder.Encode(entry)
		}
	case "csv":
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		writer := csv.NewWriter(w)
		_ = writer.Write([]string{"at", "actor", "source_ip", "backend", "cluster", "member", "action", "task_id", "result", "detail"})
		for _, entry := range entries {
			_ = writer.Write([]string{
				entry.At.UTC().Format("2006-01-02T15:04:05.000Z"), entry.Actor, entry.SourceIP,
				entry.Backend, entry.Cluster, entry.Member, entry.Action, entry.TaskID,
				entry.Result, entry.Detail,
			})
		}
		writer.Flush()
	default:
		writeError(w, http.StatusBadRequest, "invalid_request", "format must be jsonl or csv")
	}
}
