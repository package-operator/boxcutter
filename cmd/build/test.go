package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pkg.package-operator.run/cardboard/sh"
)

// Test is a collection of test related functions.
type Test struct{}

// Integration runs local integration tests in a KinD cluster.
func (t Test) Integration(_ context.Context, _ bool, _ string) error {
	//nolint:forbidigo
	fmt.Println("integration tests are not yet implemented")
	return nil
}

// Unit runs unittests, the filter argument is passed via -run="".
func (t Test) Unit(_ context.Context, filter string) error {
	if err := os.MkdirAll(filepath.Join(cacheDir, "unit"), 0o755); err != nil {
		return err
	}

	gotestArgs := []string{"-coverprofile=" + filepath.Join(cacheDir, "unit", "cover.txt"), "-race", "-json"}
	if len(filter) > 0 {
		gotestArgs = append(gotestArgs, "-run="+filter)
	}

	argStr := strings.Join(gotestArgs, " ")
	logPath := filepath.Join(cacheDir, "unit", "gotest.log")

	return sh.New(
		sh.WithEnvironment{"CGO_ENABLED": "1"},
	).Bash(
		"set -euo pipefail",
		fmt.Sprintf(`go test %s ./... 2>&1 | tee "%s" | gotestfmt --hide=empty-packages`, argStr, logPath),
	)
}
