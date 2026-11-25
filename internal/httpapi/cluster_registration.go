package httpapi

import (
	"net/http"
	"strings"

	"github.com/mickeyzzc/incus-fleet/internal/config"
	"github.com/mickeyzzc/incus-fleet/internal/store"
)

// clusterTraffic aggregates the network byte and packet counters of every
// running instance on one cluster. The dashboard polls this endpoint and
// derives rates from consecutive samples.
func (a *API) clusterTraffic(w http.ResponseWriter, r *http.Request) {
	totals, err := a.instances.ClusterTraffic(r.PathValue("cluster"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, totals)
}

// registerCluster adds a new cluster at runtime: it dials the endpoint with
// the supplied certificates, persists the configuration, and registers the
// connection with the manager.
func (a *API) registerCluster(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "cluster registration requires a persistent store")
		return
	}

	var req struct {
		Name          string `json:"name"`
		DisplayName   string `json:"display_name"`
		Endpoint      string `json:"endpoint"`
		Project       string `json:"project"`
		TLSCert       string `json:"tls_cert"`
		TLSKey        string `json:"tls_key"`
		TLSServerCert string `json:"tls_server_cert"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	cfg := config.Cluster{
		Name:          strings.TrimSpace(req.Name),
		DisplayName:   strings.TrimSpace(req.DisplayName),
		Endpoint:      strings.TrimSpace(req.Endpoint),
		Project:       strings.TrimSpace(req.Project),
		TLSCert:       strings.TrimSpace(req.TLSCert),
		TLSKey:        strings.TrimSpace(req.TLSKey),
		TLSServerCert: strings.TrimSpace(req.TLSServerCert),
	}
	if cfg.Name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "cluster name is required")
		return
	}
	if cfg.Endpoint == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "cluster endpoint is required")
		return
	}
	if cfg.DisplayName == "" {
		cfg.DisplayName = cfg.Name
	}
	if cfg.Project == "" {
		cfg.Project = "default"
	}

	summary, err := a.clusters.Register(r.Context(), cfg)
	if err != nil {
		// A failed registration must not leave a phantom offline cluster in the
		// manager: the user only sees clusters that actually connected.
		_ = a.clusters.Unregister(cfg.Name)
		a.serviceError(w, r, err)
		return
	}

	registered := store.RegisteredCluster{
		Name:          cfg.Name,
		DisplayName:   cfg.DisplayName,
		Endpoint:      cfg.Endpoint,
		Project:       cfg.Project,
		TLSCert:       cfg.TLSCert,
		TLSKey:        cfg.TLSKey,
		TLSServerCert: cfg.TLSServerCert,
	}
	if err := a.store.InsertCluster(r.Context(), &registered); err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, summary)
}

// renameCluster changes the operator-facing display name of any cluster while
// keeping the Incus identifier used by routes and API calls stable.
func (a *API) renameCluster(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("cluster")
	if _, err := a.clusters.Connection(name); err != nil {
		a.serviceError(w, r, err)
		return
	}

	var req struct {
		DisplayName string `json:"display_name"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "display name is required")
		return
	}
	if len(displayName) > 64 {
		writeError(w, http.StatusBadRequest, "invalid_request", "display name must be 64 characters or fewer")
		return
	}

	if a.store != nil {
		if err := a.store.SetClusterDisplayName(r.Context(), name, displayName); err != nil {
			a.serviceError(w, r, err)
			return
		}
	}
	if err := a.clusters.SetDisplayName(name, displayName); err != nil {
		a.serviceError(w, r, err)
		return
	}

	summary, err := a.clusters.Get(name)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

// unregisterCluster removes one dynamically registered cluster. Config-file
// clusters cannot be removed this way.
func (a *API) unregisterCluster(w http.ResponseWriter, r *http.Request) {
	if a.store == nil {
		writeError(w, http.StatusServiceUnavailable, "store_unavailable", "cluster registration requires a persistent store")
		return
	}

	name := r.PathValue("cluster")
	registered, err := a.store.GetCluster(r.Context(), name)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	if err := a.clusters.Unregister(name); err != nil {
		a.serviceError(w, r, err)
		return
	}

	if err := a.store.DeleteCluster(r.Context(), registered.Name); err != nil {
		a.serviceError(w, r, err)
		return
	}
	// A display-name override is only meaningful while the cluster exists.
	_ = a.store.DeleteClusterDisplayName(r.Context(), registered.Name)

	w.WriteHeader(http.StatusNoContent)
}
