// Package httpapi exposes the incus-fleet HTTP API and web UI.
package httpapi

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/assets"
	"github.com/mickeyzzc/incus-fleet/internal/auth"
	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/migrations"
	"github.com/mickeyzzc/incus-fleet/internal/store"

	"github.com/lxc/incus/v7/shared/api"
)

const (
	sessionCookie = "incus_fleet_session"
	maxBodyBytes  = 1 << 20
)

// Deps are the dependencies of the HTTP API.
type Deps struct {
	Clusters   *clusters.Manager
	Instances  *instances.Service
	Assets     *assets.Service
	Migrations *migrations.Service
	Auth       *auth.Authenticator
	StaticFS   fs.FS
	Logger     *slog.Logger
	Version    string
}

// API is the complete HTTP handler for incus-fleet.
type API struct {
	clusters   *clusters.Manager
	instances  *instances.Service
	assets     *assets.Service
	migrations *migrations.Service
	auth       *auth.Authenticator
	logger     *slog.Logger
	version    string
	static     fs.FS
}

// New builds the HTTP handler.
func New(deps Deps) *API {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	api := &API{
		clusters:   deps.Clusters,
		instances:  deps.Instances,
		assets:     deps.Assets,
		migrations: deps.Migrations,
		auth:       deps.Auth,
		logger:     logger,
		version:    deps.Version,
	}

	if deps.StaticFS != nil {
		if sub, err := fs.Sub(deps.StaticFS, "dist"); err == nil {
			api.static = sub
		}
	}

	return api
}

// Handler returns the fully wrapped HTTP handler.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/v1/healthz", a.health)
	mux.HandleFunc("GET /api/v1/version", a.versionHandler)
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("POST /api/v1/auth/logout", a.logout)

	mux.Handle("GET /api/v1/auth/session", a.requireAuth(http.HandlerFunc(a.session)))
	mux.Handle("GET /api/v1/clusters", a.requireAuth(http.HandlerFunc(a.listClusters)))
	mux.Handle("GET /api/v1/clusters/{cluster}", a.requireAuth(http.HandlerFunc(a.getCluster)))
	mux.Handle("POST /api/v1/clusters/{cluster}/reconnect", a.requireAuth(http.HandlerFunc(a.reconnectCluster)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances", a.requireAuth(http.HandlerFunc(a.listClusterInstances)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances/{instance}", a.requireAuth(http.HandlerFunc(a.getInstance)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances", a.requireAuth(http.HandlerFunc(a.createInstance)))
	mux.Handle("PATCH /api/v1/clusters/{cluster}/instances/{instance}", a.requireAuth(http.HandlerFunc(a.updateInstance)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/state", a.requireAuth(http.HandlerFunc(a.instanceAction)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances/{instance}/metrics", a.requireAuth(http.HandlerFunc(a.instanceMetrics)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/instances/{instance}", a.requireAuth(http.HandlerFunc(a.deleteInstance)))
	mux.Handle("GET /api/v1/clusters/{cluster}/images", a.requireAuth(http.HandlerFunc(a.listImages)))
	mux.Handle("POST /api/v1/clusters/{cluster}/images/import", a.requireAuth(http.HandlerFunc(a.importImage)))
	mux.Handle("POST /api/v1/clusters/{cluster}/images/{fingerprint}/copy", a.requireAuth(http.HandlerFunc(a.copyImage)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/images/{fingerprint}", a.requireAuth(http.HandlerFunc(a.deleteImage)))
	mux.Handle("GET /api/v1/clusters/{cluster}/resources", a.requireAuth(http.HandlerFunc(a.getResources)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances/{instance}/snapshots", a.requireAuth(http.HandlerFunc(a.listSnapshots)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/snapshots", a.requireAuth(http.HandlerFunc(a.createSnapshot)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/snapshots/{snapshot}/restore", a.requireAuth(http.HandlerFunc(a.restoreSnapshot)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/instances/{instance}/snapshots/{snapshot}", a.requireAuth(http.HandlerFunc(a.deleteSnapshot)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/devices", a.requireAuth(http.HandlerFunc(a.attachDevice)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/instances/{instance}/devices/{device}", a.requireAuth(http.HandlerFunc(a.detachDevice)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances/{instance}/console", a.requireAuth(http.HandlerFunc(a.instanceTerminal)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances/{instance}/vga", a.requireAuth(http.HandlerFunc(a.instanceVga)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances/{instance}/spice", a.requireAuth(http.HandlerFunc(a.instanceSPICE)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/spice/prepare", a.requireAuth(http.HandlerFunc(a.prepareSPICE)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances/{instance}/exec", a.requireAuth(http.HandlerFunc(a.instanceExec)))
	mux.Handle("GET /api/v1/clusters/{cluster}/instances/{instance}/files", a.requireAuth(http.HandlerFunc(a.instanceFiles)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/files", a.requireAuth(http.HandlerFunc(a.uploadInstanceFile)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/instances/{instance}/files", a.requireAuth(http.HandlerFunc(a.deleteInstanceFile)))
	mux.Handle("GET /api/v1/keys", a.requireAuth(http.HandlerFunc(a.listKeys)))
	mux.Handle("POST /api/v1/keys", a.requireAuth(http.HandlerFunc(a.createKey)))
	mux.Handle("PATCH /api/v1/keys/{id}", a.requireAuth(http.HandlerFunc(a.updateKey)))
	mux.Handle("DELETE /api/v1/keys/{id}", a.requireAuth(http.HandlerFunc(a.deleteKey)))
	mux.Handle("GET /api/v1/scripts", a.requireAuth(http.HandlerFunc(a.listScripts)))
	mux.Handle("POST /api/v1/scripts", a.requireAuth(http.HandlerFunc(a.createScript)))
	mux.Handle("PATCH /api/v1/scripts/{id}", a.requireAuth(http.HandlerFunc(a.updateScript)))
	mux.Handle("DELETE /api/v1/scripts/{id}", a.requireAuth(http.HandlerFunc(a.deleteScript)))
	mux.Handle("POST /api/v1/batch/keys", a.requireAuth(http.HandlerFunc(a.batchKeys)))
	mux.Handle("POST /api/v1/batch/scripts", a.requireAuth(http.HandlerFunc(a.batchScripts)))
	mux.Handle("POST /api/v1/batch/power", a.requireAuth(http.HandlerFunc(a.batchPower)))
	mux.Handle("GET /api/v1/clusters/{cluster}/iso", a.requireAuth(http.HandlerFunc(a.listISOs)))
	mux.Handle("POST /api/v1/clusters/{cluster}/iso", a.requireAuth(http.HandlerFunc(a.importISO)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/iso/{pool}/{name}", a.requireAuth(http.HandlerFunc(a.deleteISO)))
	mux.Handle("POST /api/v1/clusters/{cluster}/iso/{pool}/{name}/launch", a.requireAuth(http.HandlerFunc(a.launchFromISO)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/ops", a.requireAuth(http.HandlerFunc(a.runInstanceOps)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/rebuild", a.requireAuth(http.HandlerFunc(a.rebuildInstance)))
	mux.Handle("POST /api/v1/clusters/{cluster}/images/{fingerprint}/refresh", a.requireAuth(http.HandlerFunc(a.refreshImage)))
	mux.Handle("POST /api/v1/clusters/{cluster}/instances/{instance}/clone", a.requireAuth(http.HandlerFunc(a.cloneInstance)))
	mux.Handle("POST /api/v1/clusters/{cluster}/storage-pools", a.requireAuth(http.HandlerFunc(a.createStoragePool)))
	mux.Handle("PATCH /api/v1/clusters/{cluster}/storage-pools/{pool}", a.requireAuth(http.HandlerFunc(a.updateStoragePool)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/storage-pools/{pool}", a.requireAuth(http.HandlerFunc(a.deleteStoragePool)))
	mux.Handle("GET /api/v1/clusters/{cluster}/storage-pools/{pool}/volumes", a.requireAuth(http.HandlerFunc(a.listVolumes)))
	mux.Handle("POST /api/v1/clusters/{cluster}/storage-pools/{pool}/volumes", a.requireAuth(http.HandlerFunc(a.createVolume)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/storage-pools/{pool}/volumes/{volume}", a.requireAuth(http.HandlerFunc(a.deleteVolume)))
	mux.Handle("POST /api/v1/clusters/{cluster}/networks", a.requireAuth(http.HandlerFunc(a.createNetwork)))
	mux.Handle("PATCH /api/v1/clusters/{cluster}/networks/{network}", a.requireAuth(http.HandlerFunc(a.updateNetwork)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/networks/{network}", a.requireAuth(http.HandlerFunc(a.deleteNetwork)))
	mux.Handle("PATCH /api/v1/clusters/{cluster}/members/{member}", a.requireAuth(http.HandlerFunc(a.updateMember)))
	mux.Handle("GET /api/v1/clusters/{cluster}/profiles", a.requireAuth(http.HandlerFunc(a.listProfiles)))
	mux.Handle("POST /api/v1/clusters/{cluster}/profiles", a.requireAuth(http.HandlerFunc(a.createProfile)))
	mux.Handle("PATCH /api/v1/clusters/{cluster}/profiles/{profile}", a.requireAuth(http.HandlerFunc(a.updateProfile)))
	mux.Handle("DELETE /api/v1/clusters/{cluster}/profiles/{profile}", a.requireAuth(http.HandlerFunc(a.deleteProfile)))
	mux.Handle("GET /api/v1/instances", a.requireAuth(http.HandlerFunc(a.listInstances)))
	mux.Handle("GET /api/v1/migrations", a.requireAuth(http.HandlerFunc(a.listMigrations)))
	mux.Handle("POST /api/v1/migrations", a.requireAuth(http.HandlerFunc(a.submitMigration)))
	mux.Handle("GET /api/v1/migrations/{id}", a.requireAuth(http.HandlerFunc(a.getMigration)))

	mux.Handle("/", a.staticHandler())

	return a.recoverer(a.securityHeaders(a.logging(mux)))
}

func (a *API) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.authenticated(r) {
			writeError(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (a *API) authenticated(r *http.Request) bool {
	if token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		if a.auth.ValidateToken(strings.TrimSpace(token)) {
			return true
		}
	}

	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}

	return a.auth.ValidateSession(cookie.Value)
}

func (a *API) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": a.version,
		"time":    time.Now().UTC(),
	})
}

func (a *API) versionHandler(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": a.version})
}

// requestIsSecure reports whether the client reached the service over TLS,
// either directly or through a reverse proxy such as Caddy.
func requestIsSecure(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}

	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token string `json:"token"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	sessionID, expires, err := a.auth.Login(req.Token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid_token", "invalid admin token")
		return
	}

	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is set for TLS or X-Forwarded-Proto: https
		Name:     sessionCookie,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true,
		"expires_at":    expires,
	})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		a.auth.Logout(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is set for TLS or X-Forwarded-Proto: https
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsSecure(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) session(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"authenticated": true})
}

func (a *API) listClusters(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"clusters": a.clusters.List()})
}

func (a *API) getCluster(w http.ResponseWriter, r *http.Request) {
	summary, err := a.clusters.Get(r.PathValue("cluster"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

func (a *API) reconnectCluster(w http.ResponseWriter, r *http.Request) {
	summary, err := a.clusters.Reconnect(r.Context(), r.PathValue("cluster"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

func (a *API) listClusterInstances(w http.ResponseWriter, r *http.Request) {
	instanceType, err := parseInstanceType(r.URL.Query().Get("type"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	list, err := a.instances.List(r.PathValue("cluster"), instanceType)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"instances": list})
}

func (a *API) listInstances(w http.ResponseWriter, r *http.Request) {
	instanceType, err := parseInstanceType(r.URL.Query().Get("type"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	list, err := a.instances.ListAll(instanceType)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"instances": list})
}

func (a *API) getInstance(w http.ResponseWriter, r *http.Request) {
	detail, err := a.instances.Get(r.PathValue("cluster"), r.PathValue("instance"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

func (a *API) createInstance(w http.ResponseWriter, r *http.Request) {
	var req instances.CreateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	detail, err := a.instances.Create(r.Context(), r.PathValue("cluster"), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, detail)
}

func (a *API) updateInstance(w http.ResponseWriter, r *http.Request) {
	var req instances.UpdateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	detail, err := a.instances.Update(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

func (a *API) instanceAction(w http.ResponseWriter, r *http.Request) {
	var req instances.ActionRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.Action(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteInstance(w http.ResponseWriter, r *http.Request) {
	force, err := parseForce(r.URL.Query().Get("force"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.Delete(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), force); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listSnapshots(w http.ResponseWriter, r *http.Request) {
	snapshots, err := a.instances.ListSnapshots(r.PathValue("cluster"), r.PathValue("instance"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"snapshots": snapshots})
}

func (a *API) createSnapshot(w http.ResponseWriter, r *http.Request) {
	var req instances.SnapshotRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.CreateSnapshot(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) restoreSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.RestoreSnapshot(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), r.PathValue("snapshot")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteSnapshot(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteSnapshot(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), r.PathValue("snapshot")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listVolumes(w http.ResponseWriter, r *http.Request) {
	volumes, err := a.instances.Volumes(r.PathValue("cluster"), r.PathValue("pool"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"volumes": volumes})
}

func (a *API) createVolume(w http.ResponseWriter, r *http.Request) {
	var req instances.VolumeRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.CreateVolume(r.PathValue("cluster"), r.PathValue("pool"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (a *API) deleteVolume(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteVolume(r.PathValue("cluster"), r.PathValue("pool"), r.PathValue("volume")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) attachDevice(w http.ResponseWriter, r *http.Request) {
	var req instances.DeviceRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	detail, err := a.instances.AttachDevice(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

func (a *API) detachDevice(w http.ResponseWriter, r *http.Request) {
	detail, err := a.instances.DetachDevice(r.Context(), r.PathValue("cluster"), r.PathValue("instance"), r.PathValue("device"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

func (a *API) importImage(w http.ResponseWriter, r *http.Request) {
	// Image uploads can be large; the body is bounded well above any
	// realistic image while the multipart parser spills to temp files.
	r.Body = http.MaxBytesReader(w, r.Body, 8<<30)
	if err := r.ParseMultipartForm(8 << 20); err != nil { //nolint:gosec // body bounded via MaxBytesReader
		writeError(w, http.StatusBadRequest, "invalid_request", "multipart form required")
		return
	}

	metaFile, header, err := r.FormFile("image")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "image file required")
		return
	}
	defer metaFile.Close() //nolint:errcheck // upload cleanup

	var rootfs io.Reader
	if rootfsFile, _, rootfsErr := r.FormFile("rootfs"); rootfsErr == nil {
		defer rootfsFile.Close() //nolint:errcheck // upload cleanup
		rootfs = rootfsFile
	}

	query := r.URL.Query()
	fingerprint, err := a.instances.ImportImage(
		r.Context(),
		r.PathValue("cluster"),
		header.Filename,
		query.Get("type"),
		query.Get("alias"),
		metaFile,
		rootfs,
	)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	if fingerprint == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"fingerprint": fingerprint})
}

func (a *API) copyImage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		TargetCluster string `json:"target_cluster"`
		Alias         string `json:"alias"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.CopyImageTo(r.Context(), r.PathValue("cluster"), r.PathValue("fingerprint"), req.TargetCluster, req.Alias); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteImage(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteImage(r.Context(), r.PathValue("cluster"), r.PathValue("fingerprint")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listImages(w http.ResponseWriter, r *http.Request) {
	images, err := a.instances.Images(r.PathValue("cluster"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"images": images})
}

func (a *API) getResources(w http.ResponseWriter, r *http.Request) {
	resources, err := a.instances.Resources(r.PathValue("cluster"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, resources)
}

func (a *API) createStoragePool(w http.ResponseWriter, r *http.Request) {
	var req instances.StoragePoolRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.CreateStoragePool(r.PathValue("cluster"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (a *API) updateStoragePool(w http.ResponseWriter, r *http.Request) {
	var req instances.StoragePoolRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.UpdateStoragePool(r.PathValue("cluster"), r.PathValue("pool"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteStoragePool(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteStoragePool(r.PathValue("cluster"), r.PathValue("pool")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) createNetwork(w http.ResponseWriter, r *http.Request) {
	var req instances.NetworkRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.CreateNetwork(r.PathValue("cluster"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (a *API) updateNetwork(w http.ResponseWriter, r *http.Request) {
	var req instances.NetworkRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.UpdateNetwork(r.PathValue("cluster"), r.PathValue("network"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteNetwork(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteNetwork(r.PathValue("cluster"), r.PathValue("network")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listProfiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := a.instances.Profiles(r.PathValue("cluster"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"profiles": profiles})
}

func (a *API) createProfile(w http.ResponseWriter, r *http.Request) {
	var req instances.ProfileRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.CreateProfile(r.PathValue("cluster"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

func (a *API) updateProfile(w http.ResponseWriter, r *http.Request) {
	var req instances.ProfileRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.UpdateProfile(r.PathValue("cluster"), r.PathValue("profile"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) deleteProfile(w http.ResponseWriter, r *http.Request) {
	if err := a.instances.DeleteProfile(r.PathValue("cluster"), r.PathValue("profile")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) updateMember(w http.ResponseWriter, r *http.Request) {
	var req instances.MemberUpdateRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.UpdateMember(r.PathValue("cluster"), r.PathValue("member"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listMigrations(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit must be a positive integer")
			return
		}
		limit = parsed
	}

	list, err := a.migrations.List(r.Context(), limit)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"migrations": list})
}

func (a *API) submitMigration(w http.ResponseWriter, r *http.Request) {
	var req migrations.Request
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	migration, err := a.migrations.Submit(r.Context(), req)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusAccepted, migration)
}

func (a *API) getMigration(w http.ResponseWriter, r *http.Request) {
	migration, err := a.migrations.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, migration)
}

func (a *API) serviceError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"

	switch {
	case errors.Is(err, migrations.ErrInvalidRequest),
		errors.Is(err, instances.ErrInvalidRequest):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, migrations.ErrConflict):
		status, code = http.StatusConflict, "conflict"
	case errors.Is(err, migrations.ErrNotFound),
		errors.Is(err, instances.ErrNotFound),
		errors.Is(err, store.ErrNotFound),
		errors.Is(err, clusters.ErrUnknownCluster):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, clusters.ErrUnavailable):
		status, code = http.StatusServiceUnavailable, "cluster_unavailable"
	}

	if status >= 500 {
		a.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	}

	writeError(w, status, code, err.Error())
}

func (a *API) staticHandler() http.Handler {
	if a.static == nil {
		return http.NotFoundHandler()
	}

	fileServer := http.FileServer(http.FS(a.static))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}

		if info, err := fs.Stat(a.static, path); err == nil && !info.IsDir() {
			if strings.HasPrefix(path, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			fileServer.ServeHTTP(w, r)
			return
		}

		if strings.Contains(path, ".") {
			http.NotFound(w, r)
			return
		}

		index, err := fs.ReadFile(a.static, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}

func (a *API) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)

		level := slog.LevelDebug
		if r.Method != http.MethodGet || rec.status >= 400 {
			level = slog.LevelInfo
		}
		a.logger.Log(r.Context(), level, "http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration", time.Since(start).Round(time.Millisecond),
		)
	})
}

func (a *API) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; font-src 'self' data:; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

func (a *API) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				a.logger.Error("panic serving request", "path", r.URL.Path, "panic", recovered)
				writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
			}
		}()

		next.ServeHTTP(w, r)
	})
}

func parseForce(raw string) (bool, error) {
	if strings.TrimSpace(raw) == "" {
		return false, nil
	}

	force, err := strconv.ParseBool(raw)
	if err != nil {
		return false, errors.New("force must be true or false")
	}

	return force, nil
}

func parseInstanceType(raw string) (api.InstanceType, error) {
	switch strings.TrimSpace(raw) {
	case "", "any":
		return api.InstanceTypeAny, nil
	case "container":
		return api.InstanceTypeContainer, nil
	case "virtual-machine":
		return api.InstanceTypeVM, nil
	default:
		return "", fmt.Errorf("type must be one of any, container, virtual-machine")
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}

	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("invalid JSON body: multiple values")
	}

	return nil
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

// Hijack lets websocket upgrades pass through the logging wrapper.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("statusRecorder: response writer does not support hijacking")
	}

	r.status = http.StatusSwitchingProtocols

	return hijacker.Hijack()
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(data []byte) (int, error) {
	n, err := r.ResponseWriter.Write(data)
	r.bytes += n

	return n, err
}
