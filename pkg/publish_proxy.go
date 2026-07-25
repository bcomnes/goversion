package goversion

import (
	"encoding/json"
	"fmt"
	"os"
)

func planPublishProxy(meta *PublishMeta, disabled bool) {
	if disabled {
		meta.ProxyStatus = PublishStepSkipped
		return
	}
	meta.ProxyStatus = PublishStepPlanned
}

func seedPublishProxy(moduleDir, proxy string, meta *PublishMeta, runner publishCommandRunner, disabled bool) error {
	if disabled {
		meta.ProxyStatus = PublishStepSkipped
		return nil
	}

	moduleCache, err := runner.TempDir("goversion-modcache-")
	if err != nil {
		return fmt.Errorf("create isolated Go module cache: %w", err)
	}
	defer os.RemoveAll(moduleCache)

	moduleVersion := meta.ModulePath + "@" + meta.Version
	args := []string{"mod", "download", "-json", moduleVersion}
	env := []string{"GOWORK=off", "GOPROXY=" + proxy, "GOMODCACHE=" + moduleCache}
	output, err := runner.Run(moduleDir, env, "go", args...)
	if err != nil {
		return publishCommandError("seed Go module proxy", output, err, "go", args...)
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
		return fmt.Errorf("verify Go module proxy response: %s", download.Error)
	}
	if download.Path != meta.ModulePath || download.Version != meta.Version {
		return fmt.Errorf("verify Go module proxy response: got %s@%s, want %s@%s", download.Path, download.Version, meta.ModulePath, meta.Version)
	}

	meta.ProxyStatus = PublishStepCompleted
	return nil
}
