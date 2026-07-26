package goversion

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

func planPublishProxy(meta *PublishMeta, disabled bool) {
	if disabled {
		meta.ProxyStatus = PublishStepSkipped
		return
	}
	meta.ProxyStatus = PublishStepPlanned
}

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
	env := []string{"GOWORK=off", "GOPROXY=" + proxy, "GOMODCACHE=" + moduleCache}
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		output, commandErr := runner.Run(moduleDir, env, "go", args...)
		if commandErr != nil {
			if attempt < maxAttempts && retryableProxyFailure(output, commandErr) {
				if err := waitToRetryProxy(runner, progress, attempt, maxAttempts); err != nil {
					return fmt.Errorf("wait to retry Go module proxy: %w", err)
				}
				continue
			}
			return publishCommandError("seed Go module proxy", output, commandErr, "go", args...)
		}

		var download struct {
			Path    string
			Version string
			Error   string
		}
		if err := json.Unmarshal(output, &download); err != nil {
			return fmt.Errorf("verify Go module proxy response: decode go mod download output: %w", err)
		}
		if download.Error != "" {
			if attempt < maxAttempts && retryableProxyFailure([]byte(download.Error), nil) {
				if err := waitToRetryProxy(runner, progress, attempt, maxAttempts); err != nil {
					return fmt.Errorf("wait to retry Go module proxy: %w", err)
				}
				continue
			}
			return fmt.Errorf("verify Go module proxy response: %s", download.Error)
		}
		if download.Path != meta.ModulePath || download.Version != meta.Version {
			return fmt.Errorf("verify Go module proxy response: got %s@%s, want %s@%s", download.Path, download.Version, meta.ModulePath, meta.Version)
		}

		meta.ProxyStatus = PublishStepCompleted
		return nil
	}
	return errors.New("seed Go module proxy: retry attempts exhausted")
}

func waitToRetryProxy(runner publishCommandRunner, progress io.Writer, attempt, maxAttempts int) error {
	delay := time.Duration(attempt) * time.Second
	publishProgress(progress, fmt.Sprintf("Go module proxy attempt %d/%d failed transiently; retrying in %s", attempt, maxAttempts, delay))
	return runner.Sleep(delay)
}

func retryableProxyFailure(output []byte, err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	detail := strings.ToLower(string(output))
	for _, marker := range []string{
		"429 too many requests",
		"500 internal server error",
		"502 bad gateway",
		"503 service unavailable",
		"504 gateway timeout",
		"connection refused",
		"connection reset",
		"temporary failure",
		"tls handshake timeout",
		"unexpected eof",
	} {
		if strings.Contains(detail, marker) {
			return true
		}
	}
	return false
}
