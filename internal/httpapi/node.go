package httpapi

import (
	"net/http"
	"strings"

	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
)

// nodeSnapshot renders the physical network configuration of the node
// fleet runs on.
func (a *API) nodeSnapshot(w http.ResponseWriter, r *http.Request) {
	if a.nodenet == nil {
		writeError(w, http.StatusServiceUnavailable, "node_unavailable", "node network management is not enabled")
		return
	}

	wifiDevice := strings.TrimSpace(r.URL.Query().Get("wifi"))
	snapshot, err := a.nodenet.Snapshot(r.Context(), wifiDevice)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, snapshot)
}

// nodeNICState brings one NIC up or down.
func (a *API) nodeNICState(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Up bool `json:"up"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.nodenet.SetNICState(r.PathValue("name"), req.Up); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeApplyAddress switches one NIC between DHCP and a static address.
func (a *API) nodeApplyAddress(w http.ResponseWriter, r *http.Request) {
	var req nodenet.AddressRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.nodenet.ApplyAddress(r.Context(), r.PathValue("name"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeWifiConnect associates one Wi-Fi interface with an access point.
func (a *API) nodeWifiConnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SSID     string `json:"ssid"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.nodenet.WifiConnect(r.PathValue("name"), req.SSID, req.Password); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeRoutes lists the routing table.
func (a *API) nodeRoutes(w http.ResponseWriter, r *http.Request) {
	routes, err := a.nodenet.ListRoutes()
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"routes": routes})
}

// nodeAddRoute adds one static route.
func (a *API) nodeAddRoute(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Destination string `json:"destination"`
		Gateway     string `json:"gateway"`
		Device      string `json:"device"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.nodenet.AddRoute(req.Destination, req.Gateway, req.Device); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeDeleteRoute removes one static route.
func (a *API) nodeDeleteRoute(w http.ResponseWriter, r *http.Request) {
	if err := a.nodenet.DeleteRoute(r.PathValue("destination")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeFirewall lists the fleet-managed nftables rules.
func (a *API) nodeFirewall(w http.ResponseWriter, r *http.Request) {
	rules, err := a.nodenet.ListFirewall()
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

// nodeAddFirewallRule adds one input rule.
func (a *API) nodeAddFirewallRule(w http.ResponseWriter, r *http.Request) {
	var req nodenet.FirewallRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := a.nodenet.AddFirewallRule(req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeDeleteFirewallRule removes one input rule by handle.
func (a *API) nodeDeleteFirewallRule(w http.ResponseWriter, r *http.Request) {
	if err := a.nodenet.DeleteFirewallRule(r.PathValue("handle")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
