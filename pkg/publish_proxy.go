package goversion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// planPublishProxy records whether proxy seeding would run during a dry run.
func planPublishProxy(meta *PublishMeta, disabled bool) {
	if disabled {
		meta.ProxyStatus = PublishStepSkipped
		return
	}
	meta.ProxyStatus = PublishStepPlanned
}

var transientProxyStatus = regexp.MustCompile(`\b(?:429|5[0-9]{2})\b`)

type proxyDownloadResponse struct {
	Path     string               `json:"Path"`
	Version  string               `json:"Version"`
	Sum      string               `json:"Sum,omitempty"`
	GoModSum string               `json:"GoModSum,omitempty"`
	Origin   *proxyDownloadOrigin `json:"Origin,omitempty"`
	Error    string               `json:"Error,omitempty"`
}

type proxyDownloadOrigin struct {
	VCS  string `json:"VCS,omitempty"`
	URL  string `json:"URL,omitempty"`
	Hash string `json:"Hash,omitempty"`
	Ref  string `json:"Ref,omitempty"`
}

// seedPublishProxy verifies the release through an isolated cache backed only by proxy.
func seedPublishProxy(moduleDir, proxy string, meta *PublishMeta, runner publishCommandRunner, progress io.Writer, disabled bool) error {
	if disabled {
		meta.ProxyStatus = PublishStepSkipped
		return nil
	}

	moduleCache, err := runner.TempDir("goversion-modcache-")
	if err != nil {
		return fmt.Errorf("create isolated Go module cache: %w", err)
	}
	defer os.RemoveAll(moduleCache)

	const maxAttempts = 3
	moduleVersion := meta.ModulePath + "@" + meta.Version
	args := []string{"mod", "download", "-json", moduleVersion}
	env := []string{"GOWORK=off", "GOSUMDB=off", "GOPROXY=" + proxy, "GOMODCACHE=" + moduleCache}
	for attempt := 1; ; attempt++ {
		publishProgress(progress, fmt.Sprintf("Go module proxy attempt %d/%d", attempt, maxAttempts))
		output, commandErr := runner.RunCaptured(moduleDir, env, "go", args...)
		download, attemptErr := validateProxyDownload(output, commandErr, meta, args)
		if attemptErr == nil {
			printProxyDownload(progress, download)
			meta.ProxyStatus = PublishStepCompleted
			return nil
		}
		if attempt >= maxAttempts || !retryableProxyFailure(output, attemptErr) {
			return attemptErr
		}
		if err := waitToRetryProxy(runner, progress, attempt, maxAttempts); err != nil {
			return fmt.Errorf("wait to retry Go module proxy: %w", err)
		}
	}
}

// validateProxyDownload verifies that go mod download returned the requested module version.
func validateProxyDownload(output []byte, commandErr error, meta *PublishMeta, args []string) (proxyDownloadResponse, error) {
	if commandErr != nil {
		return proxyDownloadResponse{}, publishCommandError("seed Go module proxy", output, commandErr, "go", args...)
	}

	var download proxyDownloadResponse
	if err := json.Unmarshal(output, &download); err != nil {
		return download, fmt.Errorf("verify Go module proxy response: decode go mod download output: %w", err)
	}
	if download.Error != "" {
		return download, fmt.Errorf("verify Go module proxy response: %s", download.Error)
	}
	if download.Path != meta.ModulePath || download.Version != meta.Version {
		return download, fmt.Errorf("verify Go module proxy response: got %s@%s, want %s@%s", download.Path, download.Version, meta.ModulePath, meta.Version)
	}
	return download, nil
}

// printProxyDownload writes stable fields from a validated response as indented JSON.
func printProxyDownload(output io.Writer, download proxyDownloadResponse) {
	if output == nil {
		return
	}
	formatted, err := json.MarshalIndent(download, "", "\t")
	if err == nil {
		fmt.Fprintln(output, string(formatted))
	}
}

// waitToRetryProxy reports and applies the backoff for a transient proxy failure.
func waitToRetryProxy(runner publishCommandRunner, progress io.Writer, attempt, maxAttempts int) error {
	delay := time.Duration(attempt) * time.Second
	publishProgress(progress, fmt.Sprintf("Go module proxy attempt %d/%d failed transiently; retrying in %s", attempt, maxAttempts, delay))
	return runner.Sleep(delay)
}

// retryableProxyFailure identifies transient HTTP and network failures safe to retry.
func retryableProxyFailure(output []byte, err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	detail := strings.ToLower(string(output) + "\n" + err.Error())
	if transientProxyStatus.MatchString(detail) {
		return true
	}
	for _, marker := range []string{
		"connection refused",
		"connection reset",
		"connection timed out",
		"i/o timeout",
		"server misbehaving",
		"temporary failure",
		"tls handshake timeout",
		"unexpected eof",
		"http2:",
		"stream error",
	} {
		if strings.Contains(detail, marker) {
			return true
		}
	}
	return false
}
