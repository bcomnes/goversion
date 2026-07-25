package goversion

import (
	"errors"
	"strings"
	"testing"
)

func TestPlanPublishProxy(t *testing.T) {
	for _, test := range []struct {
		name       string
		disabled   bool
		wantStatus PublishStepStatus
	}{
		{name: "enabled", wantStatus: PublishStepPlanned},
		{name: "disabled", disabled: true, wantStatus: PublishStepSkipped},
	} {
		t.Run(test.name, func(t *testing.T) {
			meta := PublishMeta{ProxyStatus: PublishStepPending}
			planPublishProxy(&meta, test.disabled)
			if meta.ProxyStatus != test.wantStatus {
				t.Fatalf("got proxy status %q, want %q", meta.ProxyStatus, test.wantStatus)
			}
		})
	}
}

func TestSeedPublishProxy(t *testing.T) {
	runner := &publishTestRunner{t: t, commands: []publishTestCommand{
		{name: "go", args: []string{"mod", "download", "-json", "example.com/acme/tool@v1.2.3"}, env: []string{"GOWORK=off", "GOPROXY=https://proxy.example"}, out: "{\"Path\":\"example.com/acme/tool\",\"Version\":\"v1.2.3\"}\n"},
	}}
	meta := PublishMeta{ModulePath: "example.com/acme/tool", Version: "v1.2.3", ProxyStatus: PublishStepPending}

	if err := seedPublishProxy(t.TempDir(), "https://proxy.example", &meta, runner, false); err != nil {
		t.Fatalf("seedPublishProxy returned error: %v", err)
	}
	runner.done()
	if meta.ProxyStatus != PublishStepCompleted {
		t.Fatalf("got proxy status %q, want %q", meta.ProxyStatus, PublishStepCompleted)
	}
}

func TestSeedPublishProxyRejectsMismatchedResponse(t *testing.T) {
	runner := &publishTestRunner{t: t, commands: []publishTestCommand{
		{name: "go", args: []string{"mod", "download", "-json", "example.com/acme/tool@v1.2.3"}, env: []string{"GOWORK=off", "GOPROXY=https://proxy.example"}, out: "{\"Path\":\"example.com/acme/other\",\"Version\":\"v1.2.3\"}\n"},
	}}
	meta := PublishMeta{ModulePath: "example.com/acme/tool", Version: "v1.2.3", ProxyStatus: PublishStepPending}

	err := seedPublishProxy(t.TempDir(), "https://proxy.example", &meta, runner, false)
	if err == nil || !strings.Contains(err.Error(), "verify Go module proxy response") {
		t.Fatalf("expected proxy verification error, got %v", err)
	}
	runner.done()
	if meta.ProxyStatus != PublishStepPending {
		t.Fatalf("failed proxy status = %q, want %q", meta.ProxyStatus, PublishStepPending)
	}
}

func TestSeedPublishProxyDisabled(t *testing.T) {
	runner := &publishTestRunner{t: t}
	meta := PublishMeta{ProxyStatus: PublishStepPending}

	if err := seedPublishProxy(t.TempDir(), "https://proxy.example", &meta, runner, true); err != nil {
		t.Fatalf("seedPublishProxy returned error: %v", err)
	}
	if meta.ProxyStatus != PublishStepSkipped {
		t.Fatalf("got proxy status %q, want %q", meta.ProxyStatus, PublishStepSkipped)
	}
}

func TestSeedPublishProxyError(t *testing.T) {
	runner := &publishTestRunner{t: t, commands: []publishTestCommand{
		{name: "go", args: []string{"mod", "download", "-json", "example.com/acme/tool@v1.2.3"}, env: []string{"GOWORK=off", "GOPROXY=https://proxy.example"}, out: "not found\n", err: errors.New("exit 1")},
	}}
	meta := PublishMeta{ModulePath: "example.com/acme/tool", Version: "v1.2.3", ProxyStatus: PublishStepPending}

	err := seedPublishProxy(t.TempDir(), "https://proxy.example", &meta, runner, false)
	if err == nil || !strings.Contains(err.Error(), "seed Go module proxy") {
		t.Fatalf("expected proxy error, got %v", err)
	}
	runner.done()
	if meta.ProxyStatus != PublishStepPending {
		t.Fatalf("failed proxy status = %q, want %q", meta.ProxyStatus, PublishStepPending)
	}
}
