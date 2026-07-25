package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunDispatchesVersionHelp(t *testing.T) {
	var output, errorOutput bytes.Buffer
	if code := run([]string{"-help"}, &output, &errorOutput); code != 0 {
		t.Fatalf("run returned %d: %s", code, errorOutput.String())
	}
	if !strings.Contains(errorOutput.String(), "goversion [options] <version-bump>") {
		t.Fatalf("unexpected version help:\n%s", errorOutput.String())
	}
}

func TestRunDispatchesPublishHelp(t *testing.T) {
	var output, errorOutput bytes.Buffer
	if code := run([]string{"publish", "-help"}, &output, &errorOutput); code != 0 {
		t.Fatalf("run returned %d: %s", code, errorOutput.String())
	}
	if !strings.Contains(output.String(), "goversion publish [options]") {
		t.Fatalf("unexpected publish help:\n%s", output.String())
	}
}
