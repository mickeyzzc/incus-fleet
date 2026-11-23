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

// operatorError marks an error that is safe and useful to show to the caller
// verbatim even on a 5xx: host-command diagnostics (SSH, script exit, QEMU
// monitor failures) tell the operator what to fix, unlike raw store or
// encoding internals.
type operatorError interface {
	error
	operatorMarker()
}

type opError struct{ err error }

func (e *opError) Error() string   { return e.err.Error() }
func (e *opError) Unwrap() error   { return e.err }
func (e *opError) operatorMarker() {}

// operatorFacing tags an error as caller-visible diagnostics.
func operatorFacing(err error) error { return &opError{err: err} }

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
		errors.Is(err, nodejoin.ErrConflict),
		errors.Is(err, store.ErrConflict):
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

	// Mapped errors are classified diagnostics and stay caller-visible; an
	// unclassified 500 is an internal failure (store, HTTP client, encoding)
	// whose raw text must not leak, unless it is explicitly marked as
	// operator-facing (host-command paths hand back such diagnostics).
	facing := status != http.StatusInternalServerError
	var opErr operatorError
	if errors.As(err, &opErr) {
		facing = true
	}

	if status >= 500 {
		a.logger.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	}

	if !facing && status >= 500 {
		writeError(w, status, code, "internal server error")

		return
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
