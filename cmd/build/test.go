package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"pkg.package-operator.run/cardboard/run"
	"pkg.package-operator.run/cardboard/sh"
)

// Test is a collection of test related functions.
type Test struct{}

// Integration runs local integration tests in a KinD cluster.
func (t Test) Integration(ctx context.Context, jsonOutput bool, filter string) error {
	self := run.Meth2(t, t.Integration, jsonOutput, filter)
	if err := mgr.ParallelDeps(ctx, self,
		run.Meth(cluster, cluster.Create),
	); err != nil {
		return err
	}

	kubeconfigPath, err := cluster.KubeconfigPath()
	if err != nil {
		return err
	}
	env := sh.WithEnvironment{
		"CGO_ENABLED": "1",
		"KUBECONFIG":  kubeconfigPath,
	}

	if env["PKO_TEST_LATEST_BOOTSTRAP_JOB"] == "" {
		url := "https://github.com/package-operator/package-operator/releases/latest/download/self-bootstrap-job.yaml"
		env["PKO_TEST_LATEST_BOOTSTRAP_JOB"] = url
	}

	if err := os.MkdirAll(filepath.Join(cacheDir, "integration"), 0o755); err != nil {
		return err
	}

	// standard integration tests
	var f string
	if len(filter) > 0 {
		f = "-run " + filter
	}
	goTestCmd := t.makeGoIntTestCmd("integration", f, jsonOutput)

	err = shr.New(env).Bash(goTestCmd)
	eErr := cluster.ExportLogs(filepath.Join(cacheDir, "integration", "logs"))

	switch {
	case err != nil:
		return err
	case eErr != nil:
		return eErr
	}
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

func (Test) makeGoIntTestCmd(tags string, filter string, jsonOutput bool) string {
	args := []string{
		"go", "test",
		"-tags=" + tags,
		"-coverprofile=" + filepath.Join(cacheDir, "integration", "cover.txt"),
		filter,
		"-race",
		"-test.v",
		"-failfast",
		"-timeout=20m",
		"-count=1",
	}

	if jsonOutput {
		args = append(args, "-json")
	}

	args = append(args,
		"-coverpkg=./...,./apis/...,./pkg/...",
		"./test/...",
	)

	if jsonOutput {
		args = append(args, "|", "gotestfmt", "--hide=empty-packages")
	}

	return strings.Join(args, " ")
}
