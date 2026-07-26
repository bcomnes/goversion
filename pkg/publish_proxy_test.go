package goversion

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func proxyTestCommand(output string, err error) publishTestCommand {
	return publishTestCommand{
		name: "go",
		args: []string{"mod", "download", "-json", "example.com/acme/tool@v1.2.3"},
		env:  []string{"GOWORK=off", "GOSUMDB=off", "GOPROXY=https://proxy.example"},
		out:  output,
		err:  err,
	}
}

func proxyTestMeta() PublishMeta {
	return PublishMeta{ModulePath: "example.com/acme/tool", Version: "v1.2.3", ProxyStatus: PublishStepPending}
}

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
		proxyTestCommand(`{"Path":"example.com/acme/tool","Version":"v1.2.3","Info":"/tmp/cache/v1.2.3.info","Sum":"h1:module","GoModSum":"h1:gomod","Origin":{"VCS":"git","URL":"https://example.com/acme/tool","Hash":"abc123","Ref":"refs/tags/v1.2.3"}}`, nil),
	}}
	meta := proxyTestMeta()
	var progress bytes.Buffer

	if err := seedPublishProxy(t.TempDir(), "https://proxy.example", &meta, runner, &progress, false); err != nil {
		t.Fatalf("seedPublishProxy returned error: %v", err)
	}
	runner.done()
	if meta.ProxyStatus != PublishStepCompleted {
		t.Fatalf("got proxy status %q, want %q", meta.ProxyStatus, PublishStepCompleted)
	}
	for _, want := range []string{`"Path": "example.com/acme/tool"`, `"Sum": "h1:module"`, `"Origin": {`} {
		if !strings.Contains(progress.String(), want) {
			t.Errorf("formatted proxy output does not contain %q: %s", want, progress.String())
		}
	}
	if strings.Contains(progress.String(), "Info") || strings.Contains(progress.String(), "/tmp/cache") {
		t.Errorf("formatted proxy output contains temporary cache details: %s", progress.String())
	}
}

func TestSeedPublishProxyRetriesTransientFailure(t *testing.T) {
	runner := &publishTestRunner{t: t, commands: []publishTestCommand{
		proxyTestCommand(`{"Error":"reading proxy: 500 Internal Server Error"}`, errors.New("exit 1")),
		proxyTestCommand(`{"Path":"example.com/acme/tool","Version":"v1.2.3"}`, nil),
	}}
	meta := proxyTestMeta()
	var progress bytes.Buffer

	if err := seedPublishProxy(t.TempDir(), "https://proxy.example", &meta, runner, &progress, false); err != nil {
		t.Fatalf("seedPublishProxy returned error: %v", err)
	}
	runner.done()
	if len(runner.sleeps) != 1 || runner.sleeps[0] != time.Second {
		t.Fatalf("unexpected retry delays: %v", runner.sleeps)
	}
	for _, message := range []string{
		"Go module proxy attempt 1/3...",
		"attempt 1/3 failed transiently; retrying in 1s",
		"Go module proxy attempt 2/3...",
	} {
		if !strings.Contains(progress.String(), message) {
			t.Fatalf("proxy progress does not contain %q: %q", message, progress.String())
		}
	}
	if meta.ProxyStatus != PublishStepCompleted {
		t.Fatalf("got proxy status %q, want %q", meta.ProxyStatus, PublishStepCompleted)
	}
}

func TestSeedPublishProxyExhaustsTransientRetries(t *testing.T) {
	failure := proxyTestCommand(`{"Error":"reading proxy: 503 Service Unavailable"}`, errors.New("exit 1"))
	runner := &publishTestRunner{t: t, commands: []publishTestCommand{failure, failure, failure}}
	meta := proxyTestMeta()

	err := seedPublishProxy(t.TempDir(), "https://proxy.example", &meta, runner, nil, false)
	if err == nil || !strings.Contains(err.Error(), "503 Service Unavailable") {
		t.Fatalf("expected final proxy error, got %v", err)
	}
	runner.done()
	if len(runner.sleeps) != 2 || runner.sleeps[0] != time.Second || runner.sleeps[1] != 2*time.Second {
		t.Fatalf("unexpected retry delays: %v", runner.sleeps)
	}
}

func TestSeedPublishProxyStopsWhenBackoffIsCanceled(t *testing.T) {
	runner := &publishTestRunner{
		t:        t,
		sleepErr: context.Canceled,
		commands: []publishTestCommand{
			proxyTestCommand(`{"Error":"reading proxy: 500 Internal Server Error"}`, errors.New("exit 1")),
		},
	}
	meta := proxyTestMeta()

	err := seedPublishProxy(t.TempDir(), "https://proxy.example", &meta, runner, nil, false)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled backoff, got %v", err)
	}
	runner.done()
}

func TestRetryableProxyFailure(t *testing.T) {
	tests := []struct {
		name   string
		output string
		err    error
		want   bool
	}{
		{name: "server error", output: "500 Internal Server Error", err: errors.New("exit 1"), want: true},
		{name: "transport timeout", err: errors.New("dial tcp: i/o timeout"), want: true},
		{name: "missing version", output: "404 Not Found", err: errors.New("exit 1")},
		{name: "command timeout", output: "500 Internal Server Error", err: context.DeadlineExceeded},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := retryableProxyFailure([]byte(test.output), test.err); got != test.want {
				t.Fatalf("retryableProxyFailure() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestSeedPublishProxyRejectsMismatchedResponse(t *testing.T) {
	runner := &publishTestRunner{t: t, commands: []publishTestCommand{
		proxyTestCommand(`{"Path":"example.com/acme/other","Version":"v1.2.3"}`, nil),
	}}
	meta := proxyTestMeta()

	err := seedPublishProxy(t.TempDir(), "https://proxy.example", &meta, runner, nil, false)
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

	if err := seedPublishProxy(t.TempDir(), "https://proxy.example", &meta, runner, nil, true); err != nil {
		t.Fatalf("seedPublishProxy returned error: %v", err)
	}
	if meta.ProxyStatus != PublishStepSkipped {
		t.Fatalf("got proxy status %q, want %q", meta.ProxyStatus, PublishStepSkipped)
	}
}

func TestSeedPublishProxyError(t *testing.T) {
	runner := &publishTestRunner{t: t, commands: []publishTestCommand{
		proxyTestCommand("not found\n", errors.New("exit 1")),
	}}
	meta := proxyTestMeta()

	err := seedPublishProxy(t.TempDir(), "https://proxy.example", &meta, runner, nil, false)
	if err == nil || !strings.Contains(err.Error(), "seed Go module proxy") {
		t.Fatalf("expected proxy error, got %v", err)
	}
	runner.done()
	if meta.ProxyStatus != PublishStepPending {
		t.Fatalf("failed proxy status = %q, want %q", meta.ProxyStatus, PublishStepPending)
	}
}
