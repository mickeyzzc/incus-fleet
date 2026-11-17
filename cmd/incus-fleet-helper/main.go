// Command incus-fleet-helper is the short-lived privileged network helper.
// It reads one JSON request from stdin and writes one JSON response to stdout.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"

	"github.com/mickeyzzc/incus-fleet/internal/nodenet"
)

const defaultStateDir = "/var/lib/incus-fleet-helper"

var taskIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

type request struct {
	Action         string `json:"action"`
	TaskID         string `json:"task_id,omitempty"`
	Interface      string `json:"interface,omitempty"`
	Up             *bool  `json:"up,omitempty"`
	DHCP           bool   `json:"dhcp,omitempty"`
	Address        string `json:"address,omitempty"`
	Gateway        string `json:"gateway,omitempty"`
	Persist        bool   `json:"persist,omitempty"`
	Manager        string `json:"manager,omitempty"`
	Destination    string `json:"destination,omitempty"`
	Device         string `json:"device,omitempty"`
	Port           int    `json:"port,omitempty"`
	Proto          string `json:"proto,omitempty"`
	FirewallAction string `json:"firewall_action,omitempty"`
	Handle         string `json:"handle,omitempty"`
	Comment        string `json:"comment,omitempty"`
	SSID           string `json:"ssid,omitempty"`
	Password       string `json:"password,omitempty"`
	Spec           string `json:"spec,omitempty"`
	RollbackAfter  int    `json:"rollback_after_seconds,omitempty"`
}

type response struct {
	OK          bool              `json:"ok"`
	Action      string            `json:"action,omitempty"`
	TaskID      string            `json:"task_id,omitempty"`
	Backend     string            `json:"backend"`
	Persistence string            `json:"persistence,omitempty"`
	Message     string            `json:"message,omitempty"`
	Steps       []string          `json:"steps,omitempty"`
	Snapshot    *nodenet.Snapshot `json:"snapshot,omitempty"`
	Error       string            `json:"error,omitempty"`
}

type savedState struct {
	TaskID         string            `json:"task_id"`
	Interface      string            `json:"interface"`
	Manager        string            `json:"manager,omitempty"`
	Persistence    string            `json:"persistence_path,omitempty"`
	PersistContent string            `json:"persistence_content,omitempty"`
	Up             bool              `json:"up"`
	Addresses      []nodenet.Address `json:"addresses"`
	Routes         []nodenet.Route   `json:"routes"`
}

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--help" || os.Args[1] == "-h") {
		fmt.Fprintln(os.Stderr, "usage: incus-fleet-helper < request.json")
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "net-watchdog" {
		runWatchdog(os.Args[2:])
		return
	}
	if os.Geteuid() != 0 && os.Getenv("INCUS_FLEET_HELPER_ALLOW_NONROOT") != "1" {
		fmt.Fprintln(os.Stderr, "incus-fleet-helper must run as root")
		os.Exit(1)
	}

	resp, code := handle()
	_ = json.NewEncoder(os.Stdout).Encode(resp)
	os.Exit(code)
}

func handle() (response, int) {
	return handleReader(os.Stdin)
}

func handleReader(input io.Reader) (response, int) {
	var req request
	decoder := json.NewDecoder(io.LimitReader(input, 1<<20))
	if err := decoder.Decode(&req); err != nil {
		return response{OK: false, Error: "invalid JSON request", Backend: "helper"}, 1
	}
	if req.TaskID != "" && !taskIDPattern.MatchString(req.TaskID) {
		return response{OK: false, Error: "invalid task_id", Backend: "helper"}, 1
	}

	service := nodenet.NewService(nodenet.DirectRunner{})
	ctx := context.Background()

	switch req.Action {
	case "snapshot":
		snapshot, err := service.Snapshot(ctx, "")
		if err != nil {
			return failure(req, err), 1
		}
		return response{OK: true, Action: req.Action, Backend: "helper", Snapshot: snapshot}, 0
	case "link-state":
		if req.Up == nil || req.Interface == "" {
			return failure(req, errors.New("up and interface are required")), 1
		}
		if req.TaskID != "" {
			if err := captureState(ctx, service, req, req.TaskID); err != nil {
				return failure(req, err), 1
			}
		}
		var err error
		if runtime.GOOS == "linux" {
			err = netlinkSetLink(req.Interface, *req.Up)
		} else {
			err = service.SetNICState(req.Interface, *req.Up)
		}
		if err != nil {
			return failure(req, err), 1
		}
		return success(req, ""), 0
	case "address":
		if req.Interface == "" {
			return failure(req, errors.New("interface is required")), 1
		}
		if req.TaskID != "" {
			if err := captureState(ctx, service, req, req.TaskID); err != nil {
				return failure(req, err), 1
			}
		}
		manager := managerFor(req)
		apply := req
		apply.Persist = false
		if runtime.GOOS == "linux" && !req.DHCP {
			if err := netlinkAddAddress(req.Interface, req.Address); err != nil {
				return failure(req, err), 1
			}
			if req.Gateway != "" {
				if err := netlinkAddDefaultRoute(req.Interface, req.Gateway); err != nil {
					return failure(req, err), 1
				}
			}
		} else if err := service.ApplyAddress(ctx, req.Interface, nodenet.AddressRequest{
			DHCP: apply.DHCP, Address: apply.Address, Gateway: apply.Gateway,
		}); err != nil {
			return failure(req, err), 1
		}
		if req.Persist {
			if err := nodenet.PersistAddress(nodenet.DirectRunner{}, "/", manager, req.Interface, nodenet.AddressRequest{
				DHCP: req.DHCP, Address: req.Address, Gateway: req.Gateway, Persist: true,
			}); err != nil {
				_ = rollbackState(req.TaskID)
				return failure(req, err), 1
			}
		}
		return success(req, string(manager)), 0
	case "route-add":
		if req.TaskID != "" {
			if err := captureState(ctx, service, req, req.TaskID); err != nil {
				return failure(req, err), 1
			}
		}
		var err error
		if runtime.GOOS == "linux" {
			err = netlinkAddFleetRoute(req.Destination, req.Gateway, req.Device)
		} else {
			err = service.AddFleetRoute(req.Destination, req.Gateway, req.Device)
		}
		if err != nil {
			return failure(req, err), 1
		}
		return success(req, ""), 0
	case "route-delete":
		if req.TaskID != "" {
			if err := captureState(ctx, service, req, req.TaskID); err != nil {
				return failure(req, err), 1
			}
		}
		var err error
		if runtime.GOOS == "linux" {
			err = netlinkDeleteFleetRoute(req.Destination)
		} else {
			err = service.DeleteFleetRoute(req.Destination)
		}
		if err != nil {
			return failure(req, err), 1
		}
		return success(req, ""), 0
	case "firewall-add":
		if err := service.AddFirewallRule(nodenet.FirewallRequest{
			Port: req.Port, Proto: req.Proto, Action: req.FirewallAction, Comment: req.Comment,
		}); err != nil {
			return failure(req, err), 1
		}
		return success(req, ""), 0
	case "firewall-delete":
		if err := service.DeleteFirewallRule(req.Handle); err != nil {
			return failure(req, err), 1
		}
		return success(req, ""), 0
	case "wifi-connect":
		if req.Interface == "" || req.SSID == "" {
			return failure(req, errors.New("interface and ssid are required")), 1
		}
		if err := service.WifiConnect(req.Interface, req.SSID, req.Password); err != nil {
			return failure(req, err), 1
		}
		return success(req, ""), 0
	case "incus-bootstrap":
		return handleBootstrap(req)
	case "confirm":
		if err := removeState(req.TaskID); err != nil {
			return failure(req, err), 1
		}
		return success(req, ""), 0
	case "rollback":
		if err := rollbackState(req.TaskID); err != nil {
			return failure(req, err), 1
		}
		return success(req, ""), 0
	default:
		return failure(req, fmt.Errorf("unsupported action %q", req.Action)), 1
	}
}

func managerFor(req request) nodenet.Manager {
	if req.Manager != "" {
		return nodenet.Manager(req.Manager)
	}
	return nodenet.DetectManager("/")
}

func success(req request, persistence string) response {
	if isMutation(req.Action) && req.TaskID != "" {
		startWatchdog(req.TaskID, req.RollbackAfter)
	}
	return response{OK: true, Action: req.Action, TaskID: req.TaskID, Backend: "helper", Persistence: persistence}
}

func isMutation(action string) bool {
	switch action {
	case "link-state", "address", "route-add", "route-delete", "firewall-add", "firewall-delete", "wifi-connect":
		return true
	default:
		return false
	}
}

func failure(req request, err error) response {
	return response{OK: false, Action: req.Action, TaskID: req.TaskID, Backend: "helper", Error: err.Error()}
}

func stateDir() string {
	if value := os.Getenv("INCUS_FLEET_HELPER_STATE_DIR"); value != "" {
		return value
	}
	return defaultStateDir
}

func statePath(taskID string) string {
	return filepath.Join(stateDir(), taskID+".json")
}

func captureState(ctx context.Context, service *nodenet.Service, req request, taskID string) error {
	iface := req.Interface
	if iface == "" {
		iface = req.Device
	}
	if iface == "" {
		return errors.New("interface is required for rollback state")
	}
	nics, err := service.ListNICs(ctx)
	if err != nil {
		return err
	}
	state := savedState{TaskID: taskID, Interface: iface, Manager: string(managerFor(req)), Up: true}
	for _, nic := range nics {
		if nic.Name == iface {
			state.Up = nic.State == "up"
			state.Addresses = append([]nodenet.Address(nil), nic.Addresses...)
			break
		}
	}
	routes, err := service.ListRoutes()
	if err != nil {
		return err
	}
	for _, route := range routes {
		if route.Device == iface || (route.Destination == "default" && route.Device == iface) {
			state.Routes = append(state.Routes, route)
		}
	}
	manager := managerFor(req)
	if path := nodenet.PersistencePath("/", manager, iface); path != "" {
		state.Persistence = path
		if data, err := os.ReadFile(path); err == nil { //nolint:gosec // fleet-owned persistence path
			state.PersistContent = string(data)
		}
	}
	if err := os.MkdirAll(stateDir(), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(statePath(taskID), data, 0o600)
}

func removeState(taskID string) error {
	if taskID == "" {
		return errors.New("task_id is required")
	}
	err := os.Remove(statePath(taskID))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func rollbackState(taskID string) error {
	if taskID == "" {
		return errors.New("task_id is required")
	}
	data, err := os.ReadFile(statePath(taskID))
	if err != nil {
		return err
	}
	var state savedState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	service := nodenet.NewService(nodenet.DirectRunner{})
	runner := nodenet.DirectRunner{}
	if _, err := runner.Run("ip", "addr", "flush", "dev", state.Interface); err != nil {
		return fmt.Errorf("flush addresses: %w", err)
	}
	for _, addr := range state.Addresses {
		value := fmt.Sprintf("%s/%d", addr.Address, addr.Prefixlen)
		if err := service.ApplyAddress(context.Background(), state.Interface, nodenet.AddressRequest{Address: value}); err != nil {
			return err
		}
	}
	for _, route := range state.Routes {
		// Connected/kernel routes are recreated by adding the addresses above.
		if route.Protocol != "" && route.Protocol != "99" {
			continue
		}
		if err := service.AddFleetRoute(route.Destination, route.Gateway, state.Interface); err != nil {
			return err
		}
	}
	if err := service.SetNICState(state.Interface, state.Up); err != nil {
		return err
	}
	if state.Persistence != "" {
		if state.PersistContent == "" {
			_ = os.Remove(state.Persistence)
		} else if err := os.WriteFile(state.Persistence, []byte(state.PersistContent), 0o644); err != nil { //nolint:gosec // restore manager config
			return err
		}
	}

	return removeState(taskID)
}
