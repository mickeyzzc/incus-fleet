package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/mickeyzzc/incus-fleet/internal/hostops"
	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
)

const (
	actionNodeNICState       = "node.nic.state"
	actionNodeAddress        = "node.address.apply"
	actionNodeWifi           = "node.wifi.connect"
	actionNodeRouteAdd       = "node.route.add"
	actionNodeRouteDelete    = "node.route.delete"
	actionNodeFirewallAdd    = "node.firewall.add"
	actionNodeFirewallDelete = "node.firewall.delete"
)

// nodeSnapshot renders the physical network configuration of one member.
func (a *API) nodeSnapshot(w http.ResponseWriter, r *http.Request) {
	operator, backend, closeNode, ok := a.resolveNode(w, r, hostops.Read)
	if !ok {
		return
	}
	defer closeNode()

	wifiDevice := strings.TrimSpace(r.URL.Query().Get("wifi"))
	snapshot, err := operator.Snapshot(r.Context(), wifiDevice)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}
	if snapshot == nil {
		snapshot = &nodenet.Snapshot{}
	}
	snapshot.Backend = string(backend)

	writeJSON(w, http.StatusOK, snapshot)
}

// nodeNICState brings one member NIC up or down.
func (a *API) nodeNICState(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Up bool `json:"up"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	operator, backend, closeNode, ok := a.resolveNode(w, r, hostops.Write)
	if !ok {
		return
	}
	defer closeNode()

	name := r.PathValue("name")
	plan := hostPlan("iface="+name, "up="+strconv.FormatBool(req.Up))
	task, ok := a.beginHostTask(w, r, actionNodeNICState, plan)
	if !ok {
		return
	}

	err := operator.SetNICState(name, req.Up)
	a.finishHostTask(r, task, backend, actionNodeNICState, plan, err)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeApplyAddress switches one member NIC between DHCP and a static address.
func (a *API) nodeApplyAddress(w http.ResponseWriter, r *http.Request) {
	var req nodenet.AddressRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	operator, backend, closeNode, ok := a.resolveNode(w, r, hostops.Write)
	if !ok {
		return
	}
	defer closeNode()

	name := r.PathValue("name")
	plan := hostPlan("iface="+name, "dhcp="+strconv.FormatBool(req.DHCP), "address="+req.Address,
		"gateway="+req.Gateway, "persist="+strconv.FormatBool(req.Persist))
	task, ok := a.beginHostTask(w, r, actionNodeAddress, plan)
	if !ok {
		return
	}

	err := operator.ApplyAddress(r.Context(), name, req)
	a.finishHostTask(r, task, backend, actionNodeAddress, plan, err)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeWifiConnect associates one member Wi-Fi interface with an access point.
func (a *API) nodeWifiConnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SSID     string `json:"ssid"`
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	operator, backend, closeNode, ok := a.resolveNode(w, r, hostops.Write)
	if !ok {
		return
	}
	defer closeNode()

	name := r.PathValue("name")
	// Never put the Wi-Fi password in the task or audit record.
	plan := hostPlan("iface="+name, "ssid="+req.SSID)
	task, ok := a.beginHostTask(w, r, actionNodeWifi, plan)
	if !ok {
		return
	}

	err := operator.WifiConnect(name, req.SSID, req.Password)
	a.finishHostTask(r, task, backend, actionNodeWifi, plan, err)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeRoutes lists the routing table of one member.
func (a *API) nodeRoutes(w http.ResponseWriter, r *http.Request) {
	operator, backend, closeNode, ok := a.resolveNode(w, r, hostops.Read)
	if !ok {
		return
	}
	defer closeNode()

	routes, err := operator.ListRoutes()
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"routes": routes, "backend": backend})
}

// nodeAddRoute adds one static route on a member.
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

	operator, backend, closeNode, ok := a.resolveNode(w, r, hostops.Write)
	if !ok {
		return
	}
	defer closeNode()

	plan := hostPlan("destination="+req.Destination, "gateway="+req.Gateway, "device="+req.Device)
	task, ok := a.beginHostTask(w, r, actionNodeRouteAdd, plan)
	if !ok {
		return
	}

	err := operator.AddRoute(req.Destination, req.Gateway, req.Device)
	a.finishHostTask(r, task, backend, actionNodeRouteAdd, plan, err)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeDeleteRoute removes one static route from a member.
func (a *API) nodeDeleteRoute(w http.ResponseWriter, r *http.Request) {
	operator, backend, closeNode, ok := a.resolveNode(w, r, hostops.Write)
	if !ok {
		return
	}
	defer closeNode()

	destination := r.PathValue("destination")
	plan := hostPlan("destination=" + destination)
	task, ok := a.beginHostTask(w, r, actionNodeRouteDelete, plan)
	if !ok {
		return
	}

	err := operator.DeleteRoute(destination)
	a.finishHostTask(r, task, backend, actionNodeRouteDelete, plan, err)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeFirewall lists the fleet-managed nftables rules of one member.
func (a *API) nodeFirewall(w http.ResponseWriter, r *http.Request) {
	operator, backend, closeNode, ok := a.resolveNode(w, r, hostops.Read)
	if !ok {
		return
	}
	defer closeNode()

	rules, err := operator.ListFirewall()
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"rules": rules, "backend": backend})
}

// nodeAddFirewallRule adds one input rule on a member.
func (a *API) nodeAddFirewallRule(w http.ResponseWriter, r *http.Request) {
	var req nodenet.FirewallRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}

	operator, backend, closeNode, ok := a.resolveNode(w, r, hostops.Write)
	if !ok {
		return
	}
	defer closeNode()

	plan := hostPlan("port="+strconv.Itoa(req.Port), "proto="+req.Proto, "action="+req.Action, "comment="+req.Comment)
	task, ok := a.beginHostTask(w, r, actionNodeFirewallAdd, plan)
	if !ok {
		return
	}

	err := operator.AddFirewallRule(req)
	a.finishHostTask(r, task, backend, actionNodeFirewallAdd, plan, err)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// nodeDeleteFirewallRule removes one input rule by handle.
func (a *API) nodeDeleteFirewallRule(w http.ResponseWriter, r *http.Request) {
	operator, backend, closeNode, ok := a.resolveNode(w, r, hostops.Write)
	if !ok {
		return
	}
	defer closeNode()

	handle := r.PathValue("handle")
	plan := hostPlan("handle=" + handle)
	task, ok := a.beginHostTask(w, r, actionNodeFirewallDelete, plan)
	if !ok {
		return
	}

	err := operator.DeleteFirewallRule(handle)
	a.finishHostTask(r, task, backend, actionNodeFirewallDelete, plan, err)
	if err != nil {
		a.serviceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
