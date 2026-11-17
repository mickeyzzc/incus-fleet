package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/mickeyzzc/incus-fleet/internal/bootstrap"
)

var packagePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9.+-]{0,63}$`)

type bootstrapState struct {
	TaskID string   `json:"task_id"`
	Steps  []string `json:"steps"`
}

func handleBootstrap(req request) (response, int) {
	if strings.TrimSpace(req.Spec) == "" {
		return failure(req, errors.New("bootstrap spec is required")), 1
	}
	var spec bootstrap.Spec
	if err := json.Unmarshal([]byte(req.Spec), &spec); err != nil {
		return failure(req, fmt.Errorf("invalid bootstrap spec: %w", err)), 1
	}
	caPath := os.Getenv("INCUS_FLEET_BOOTSTRAP_CA")
	if caPath == "" {
		caPath = "/var/lib/incus-fleet-agent/identity/ca.pem"
	}
	caPEM, err := os.ReadFile(caPath) //nolint:gosec // operator-configured CA path
	if err != nil {
		return failure(req, fmt.Errorf("read bootstrap CA: %w", err)), 1
	}
	if err := bootstrap.Verify(caPEM, &spec); err != nil {
		return failure(req, err), 1
	}
	if err := spec.Validate(time.Now()); err != nil {
		return failure(req, err), 1
	}
	steps, err := runBootstrap(&spec)
	if err != nil {
		return response{OK: false, Action: req.Action, TaskID: spec.TaskID, Backend: "helper", Steps: steps, Error: err.Error()}, 1
	}

	return response{OK: true, Action: req.Action, TaskID: spec.TaskID, Backend: "helper",
		Message: "Incus bootstrap complete", Steps: steps}, 0
}

func runBootstrap(spec *bootstrap.Spec) ([]string, error) {
	statePath := filepath.Join(stateDir(), "bootstrap-"+spec.TaskID+".json")
	state := bootstrapState{TaskID: spec.TaskID}
	if data, err := os.ReadFile(statePath); err == nil { //nolint:gosec // fleet-owned state
		_ = json.Unmarshal(data, &state)
	}
	done := make(map[string]bool, len(state.Steps))
	for _, step := range state.Steps {
		done[step] = true
	}
	record := func(step string) error {
		if done[step] {
			return nil
		}
		state.Steps = append(state.Steps, step)
		done[step] = true
		if err := os.MkdirAll(stateDir(), 0o700); err != nil {
			return err
		}
		data, _ := json.MarshalIndent(state, "", "  ")
		return os.WriteFile(statePath, data, 0o600)
	}

	if spec.InstallMode != "existing" {
		if !done["install"] {
			if err := installIncus(spec); err != nil {
				return state.Steps, err
			}
			if err := record("install"); err != nil {
				return state.Steps, err
			}
		}
	}
	if !done["service"] {
		if err := enableIncusService(); err != nil {
			return state.Steps, err
		}
		if err := record("service"); err != nil {
			return state.Steps, err
		}
	}
	if spec.VersionPin != "" {
		version, err := commandOutput("incus", "--version")
		if err != nil {
			return state.Steps, fmt.Errorf("read Incus version: %w", err)
		}
		if !strings.HasPrefix(strings.TrimSpace(version), spec.VersionPin) {
			return state.Steps, fmt.Errorf("incus version %q does not match pinned %q", strings.TrimSpace(version), spec.VersionPin)
		}
	}
	if !done["preseed"] {
		if err := runCommandInput(spec.Preseed, "incus", "admin", "init", "--preseed"); err != nil {
			return state.Steps, fmt.Errorf("incus preseed: %w", err)
		}
		if err := record("preseed"); err != nil {
			return state.Steps, err
		}
	}
	if !done["ready"] {
		deadline := time.Now().Add(60 * time.Second)
		for {
			if _, err := commandOutput("incus", "query", "/1.0"); err == nil {
				break
			}
			if time.Now().After(deadline) {
				return state.Steps, errors.New("incus did not become ready within 60s")
			}
			time.Sleep(time.Second)
		}
		if err := record("ready"); err != nil {
			return state.Steps, err
		}
	}

	return state.Steps, nil
}

func installIncus(spec *bootstrap.Spec) error {
	if spec.RepoURL != "" {
		return errors.New("repo_url is not supported by this helper build; configure the distribution repository before enrollment")
	}
	packages := spec.Packages
	if len(packages) == 0 {
		packages = []string{"incus"}
	}
	for _, pkg := range packages {
		if !packagePattern.MatchString(pkg) {
			return fmt.Errorf("invalid package %q", pkg)
		}
	}

	switch {
	case executable("apt-get"):
		args := append([]string{"DEBIAN_FRONTEND=noninteractive", "apt-get", "install", "-y"}, packages...)
		if err := runCommandInput("", "env", args...); err != nil {
			return err
		}
	case executable("dnf"):
		if err := runCommandInput("", "dnf", append([]string{"install", "-y"}, packages...)...); err != nil {
			return err
		}
	case executable("zypper"):
		if err := runCommandInput("", "zypper", append([]string{"--non-interactive", "install"}, packages...)...); err != nil {
			return err
		}
	case executable("apk"):
		if err := runCommandInput("", "apk", append([]string{"add"}, packages...)...); err != nil {
			return err
		}
	case executable("pacman"):
		if err := runCommandInput("", "pacman", append([]string{"-S", "--noconfirm"}, packages...)...); err != nil {
			return err
		}
	default:
		return errors.New("no supported package manager found")
	}

	return nil
}

func enableIncusService() error {
	if executable("systemctl") {
		return runCommandInput("", "systemctl", "enable", "--now", "incus.socket", "incus")
	}
	if executable("rc-update") && executable("rc-service") {
		if err := runCommandInput("", "rc-update", "add", "incusd"); err != nil {
			return err
		}
		return runCommandInput("", "rc-service", "incusd", "start")
	}

	return errors.New("no supported service manager found")
}

func executable(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func commandOutput(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput() //nolint:gosec // fixed allowlisted package/service commands
	if err != nil {
		return string(out), fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

func runCommandInput(stdin, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...) //nolint:gosec // fixed allowlisted package/service commands
	if stdin != "" {
		cmd.Stdin = bytes.NewBufferString(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
