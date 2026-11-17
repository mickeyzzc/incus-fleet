package httpapi

import (
	"net/http"
	"strings"

	"github.com/mickeyzzc/incus-fleet/internal/nodeaccess"
	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
)

// nodeService returns the node network service or writes a 503.
func (a *API) nodeService(w http.ResponseWriter) (*nodeaccess.Service, bool) {
	if a.nodenet == nil {
		writeError(w, http.StatusServiceUnavailable, "node_unavailable", "node network management is not enabled")
		return nil, false
	}

	return a.nodenet, true
}

// nodeSnapshot renders the physical network configuration of one member.
func (a *API) nodeSnapshot(w http.ResponseWriter, r *http.Request) {
	service, ok := a.nodeService(w)
	if !ok {
		return
	}

	wifiDevice := strings.TrimSpace(r.URL.Query().Get("wifi"))
	snapshot, err := service.Snapshot(r.Context(), r.PathValue("cluster"), r.PathValue("member"), wifiDevice)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, snapshot)
}

// nodeNICState brings one member NIC up or down.
func (a *API) nodeNICState(w http.ResponseWriter, r *http.Request) {
	service, ok := a.nodeService(w)
	if !ok {
		return
	}

	var req struct {
		Up bool `json:"up"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := service.SetNICState(r.PathValue("cluster"), r.PathValue("member"), r.PathValue("name"), req.Up); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeApplyAddress switches one member NIC between DHCP and a static address.
func (a *API) nodeApplyAddress(w http.ResponseWriter, r *http.Request) {
	service, ok := a.nodeService(w)
	if !ok {
		return
	}

	var req nodenet.AddressRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := service.ApplyAddress(r.Context(), r.PathValue("cluster"), r.PathValue("member"), r.PathValue("name"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeWifiConnect associates one member Wi-Fi interface with an access point.
func (a *API) nodeWifiConnect(w http.ResponseWriter, r *http.Request) {
	service, ok := a.nodeService(w)
	if !ok {
		return
	}

	var req struct {
		SSID     string `json:"ssid"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := service.WifiConnect(r.PathValue("cluster"), r.PathValue("member"), r.PathValue("name"), req.SSID, req.Password); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeRoutes lists the routing table of one member.
func (a *API) nodeRoutes(w http.ResponseWriter, r *http.Request) {
	service, ok := a.nodeService(w)
	if !ok {
		return
	}

	routes, err := service.ListRoutes(r.PathValue("cluster"), r.PathValue("member"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"routes": routes})
}

// nodeAddRoute adds one static route on a member.
func (a *API) nodeAddRoute(w http.ResponseWriter, r *http.Request) {
	service, ok := a.nodeService(w)
	if !ok {
		return
	}

	var req struct {
		Destination string `json:"destination"`
		Gateway     string `json:"gateway"`
		Device      string `json:"device"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := service.AddRoute(r.PathValue("cluster"), r.PathValue("member"), req.Destination, req.Gateway, req.Device); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeDeleteRoute removes one static route from a member.
func (a *API) nodeDeleteRoute(w http.ResponseWriter, r *http.Request) {
	service, ok := a.nodeService(w)
	if !ok {
		return
	}

	if err := service.DeleteRoute(r.PathValue("cluster"), r.PathValue("member"), r.PathValue("destination")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeFirewall lists the fleet-managed nftables rules of one member.
func (a *API) nodeFirewall(w http.ResponseWriter, r *http.Request) {
	service, ok := a.nodeService(w)
	if !ok {
		return
	}

	rules, err := service.ListFirewall(r.PathValue("cluster"), r.PathValue("member"))
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

// nodeAddFirewallRule adds one input rule on a member.
func (a *API) nodeAddFirewallRule(w http.ResponseWriter, r *http.Request) {
	service, ok := a.nodeService(w)
	if !ok {
		return
	}

	var req nodenet.FirewallRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	if err := service.AddFirewallRule(r.PathValue("cluster"), r.PathValue("member"), req); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeDeleteFirewallRule removes one input rule by handle.
func (a *API) nodeDeleteFirewallRule(w http.ResponseWriter, r *http.Request) {
	service, ok := a.nodeService(w)
	if !ok {
		return
	}

	if err := service.DeleteFirewallRule(r.PathValue("cluster"), r.PathValue("member"), r.PathValue("handle")); err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
