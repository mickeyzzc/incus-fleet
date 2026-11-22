package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/mickeyzzc/incus-fleet/internal/clusters"
	"github.com/mickeyzzc/incus-fleet/internal/guestagent"
	"github.com/mickeyzzc/incus-fleet/internal/hostops"
	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/migrations"
	"github.com/mickeyzzc/incus-fleet/internal/nodeaccess"
	"github.com/mickeyzzc/incus-fleet/internal/nodejoin"
	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
	"github.com/mickeyzzc/incus-fleet/internal/overlay"
	"github.com/mickeyzzc/incus-fleet/internal/store"

	"github.com/lxc/incus/v7/shared/api"
)

func (a *API) serviceError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"

	switch {
	case errors.Is(err, migrations.ErrInvalidRequest),
		errors.Is(err, instances.ErrInvalidRequest),
		errors.Is(err, nodejoin.ErrInvalidRequest),
		errors.Is(err, guestagent.ErrInvalidRequest),
		errors.Is(err, overlay.ErrInvalidRequest),
		errors.Is(err, nodenet.ErrInvalidRequest):
		status, code = http.StatusBadRequest, "invalid_request"
	case errors.Is(err, migrations.ErrConflict),
		errors.Is(err, nodejoin.ErrConflict):
		status, code = http.StatusConflict, "conflict"
	case errors.Is(err, migrations.ErrNotFound),
		errors.Is(err, guestagent.ErrNotFound),
		errors.Is(err, overlay.ErrNotFound),
		errors.Is(err, instances.ErrNotFound),
		errors.Is(err, store.ErrNotFound),
		errors.Is(err, clusters.ErrUnknownCluster),
		errors.Is(err, nodeaccess.ErrUnknownMember):
		status, code = http.StatusNotFound, "not_found"
	case errors.Is(err, clusters.ErrUnavailable):
		status, code = http.StatusServiceUnavailable, "cluster_unavailable"
	case errors.Is(err, nodenet.ErrUnavailable),
		errors.Is(err, hostops.ErrSSHWriteDisabled),
		errors.Is(err, hostops.ErrAgentUnavailable):
		status, code = http.StatusServiceUnavailable, "node_unavailable"
	}

	if status >= 500 {
		a.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	}

	writeError(w, status, code, err.Error())
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
