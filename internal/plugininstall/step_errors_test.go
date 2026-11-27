package plugininstall

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInstallStepErrorBranches(t *testing.T) {
	ctx := context.Background()
	health := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer health.Close()
	ops := &fakeInstances{ip: "192.0.2.50"}
	i, _, _ := newInstaller(t, ops, health)
	req := testRequest(t)

	noImages := *i
	noImages.cfg.Images = nil
	cp := checkpoint{Cluster: "c1"}
	if err := noImages.runStep(ctx, &req, &cp, 2); err == nil {
		t.Fatal("expected missing image resolver error")
	}

	noInstances := *i
	noInstances.cfg.Instances = nil
	if err := noInstances.runStep(ctx, &req, &cp, 5); err == nil {
		t.Fatal("expected missing instance service error")
	}

	formatFail := &fakeInstances{ip: "192.0.2.50", opsErr: errStep}
	failing := *i
	failing.cfg.Instances = formatFail
	formatCP := checkpoint{Cluster: "c1", Instance: "fleet-vm", Volume: "data"}
	if err := failing.runStep(ctx, &req, &formatCP, 4); err == nil {
		t.Fatal("expected format ops error")
	}
	formatFail.opsErr = nil
	formatFail.opsScriptErr = "set -eu"
	if err := failing.runStep(ctx, &req, &formatCP, 4); err == nil {
		t.Fatal("expected format exit code error")
	}

	uploadFail := &fakeInstances{ip: "192.0.2.50", uploadErr: errStep}
	uploading := *i
	uploading.cfg.Instances = uploadFail
	if err := uploading.runStep(ctx, &req, &formatCP, 6); err == nil {
		t.Fatal("expected config upload error")
	}

	noRegistry := *i
	noRegistry.cfg.Registry = nil
	wiringCP := checkpoint{Cluster: "c1", Instance: "fleet-vm", Upstream: "http://192.0.2.50:8428"}
	if err := noRegistry.runStep(ctx, &req, &wiringCP, 8); err == nil {
		t.Fatal("expected missing registry error")
	}
}

type stepError struct{}

func (stepError) Error() string { return "step failed" }

var errStep = stepError{}
