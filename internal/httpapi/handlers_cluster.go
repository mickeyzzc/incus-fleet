package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/mickeyzzc/incus-fleet/internal/instances"
	"github.com/mickeyzzc/incus-fleet/internal/migrations"
	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
)

func (a *API) listClusters(w http.ResponseWriter, r *http.Request) {
	a.applyClusterDisplayNames(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"clusters": a.clusters.List()})
}

func (a *API) getCluster(w http.ResponseWriter, r *http.Request) {
	a.applyClusterDisplayNames(r.Context())
	summary, err := a.clusters.Get(r.PathValue("cluster"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

// applyClusterDisplayNames keeps every center in a shared deployment on the
// same operator-facing labels. Renames are rare, so one small read per list or
// detail request is cheaper than a cross-center watch.
func (a *API) applyClusterDisplayNames(ctx context.Context) {
	if a.store == nil {
		return
	}

	overrides, err := a.store.ListClusterDisplayNames(ctx)
	if err != nil {
		return
	}
	for name, displayName := range overrides {
		_ = a.clusters.SetDisplayName(name, displayName)
	}
}

func (a *API) reconnectCluster(w http.ResponseWriter, r *http.Request) {
	summary, err := a.clusters.Reconnect(r.Context(), r.PathValue("cluster"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

func (a *API) getResources(w http.ResponseWriter, r *http.Request) {
	resources, err := a.instances.Resources(r.PathValue("cluster"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	// Bond slaves cannot serve as macvlan parents while the bond owns them;
	// hide them from the candidate lists. Unmanaged NICs often carry no
	// locations in the Incus listing, so the per-member link inventory also
	// attributes them to the members that expose them (the UI groups
	// physical NICs per member). Node access may be unavailable, in which
	// case the unfiltered list is served as before.
	members, memberErr := a.instances.MemberNames(r.PathValue("cluster"))
	if memberErr != nil {
		members = nil
	}
	resources.Networks = enrichNetworks(resources.Networks, members, a.linkInventoryLookup(r.Context(), r.PathValue("cluster")))

	writeJSON(w, http.StatusOK, resources)
}

// linkInventoryLookup builds a member→interface-master-map resolver over
// node access. Members whose lookup fails (SSH unconfigured, node offline)
// contribute no map; the caller keeps their NICs visible.
func (a *API) linkInventoryLookup(ctx context.Context, cluster string) func(member string) (map[string]string, error) {
	return func(member string) (map[string]string, error) {
		runner, closeNode, err := a.nodenet.HostRunner(ctx, cluster, member)
		if err != nil {
			return nil, err
		}
		defer closeNode()

		return nodenet.LinkMasters(ctx, runner)
	}
}

// enrichNetworks drops bond slaves from the unmanaged NIC list and fills in
// missing locations from the per-member link inventories — a NIC's own
// locations win when Incus reports them; otherwise every member whose link
// table contains the interface is recorded. Managed networks are never
// touched, and a failing lookup degrades to keeping the entry unlocated.
func enrichNetworks(networks []instances.Network, members []string, lookup func(member string) (map[string]string, error)) []instances.Network {
	inventories := make(map[string]map[string]string, len(members))
	for _, member := range members {
		links, err := lookup(member)
		if err != nil || links == nil {
			continue
		}
		inventories[member] = links
	}

	out := make([]instances.Network, 0, len(networks))
	for _, network := range networks {
		if network.Managed {
			out = append(out, network)
			continue
		}
		// Loopback cannot be a macvlan parent; offering it in the physical
		// NIC dropdown only produces a runtime device-start failure.
		if network.Name == "lo" {
			continue
		}

		located := network.Locations
		if len(located) == 0 {
			for _, member := range members {
				if _, seen := inventories[member][network.Name]; seen {
					located = append(located, member)
				}
			}
		}

		slave := false
		for _, member := range located {
			if inventories[member][network.Name] != "" {
				slave = true
				break
			}
		}
		if slave {
			continue
		}

		network.Locations = located
		out = append(out, network)
	}

	return out
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

// renameClusterMember changes an Incus cluster member name. The Incus API
// updates the member record and every instance location in one operation.
func (a *API) renameClusterMember(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.instances.RenameMember(r.PathValue("cluster"), r.PathValue("member"), req.Name); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) listMigrations(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			writeError(w, http.StatusBadRequest, "invalid_request", "limit must be between 1 and 200")
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

func (a *API) cancelMigration(w http.ResponseWriter, r *http.Request) {
	migration, err := a.migrations.Cancel(r.Context(), r.PathValue("id"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusAccepted, migration)
}

func (a *API) retryMigration(w http.ResponseWriter, r *http.Request) {
	migration, err := a.migrations.Retry(r.Context(), r.PathValue("id"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusAccepted, migration)
}
