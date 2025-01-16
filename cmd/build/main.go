package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"

	"pkg.package-operator.run/cardboard/modules/kind"
	"pkg.package-operator.run/cardboard/run"
	"pkg.package-operator.run/cardboard/sh"
)

var (
	shr = sh.New()
	mgr = run.New(run.WithSources(source))

	// internal modules.
	// generate Generate.
	test    Test
	lint    Lint
	cluster = kind.NewCluster("boxcutter")

	//go:embed *.go
	source embed.FS
)

func main() {
	ctx := context.Background()
	err := errors.Join(
		mgr.RegisterGoTool("gotestfmt", "github.com/gotesttools/gotestfmt/v2/cmd/gotestfmt", "2.5.0"),
		mgr.RegisterGoTool("golangci-lint", "github.com/golangci/golangci-lint/cmd/golangci-lint", "1.63.4"),
		mgr.Register(&Dev{}, &CI{}),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n%s\n", err)
		os.Exit(1)
	}
	if err := mgr.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "\n%s\n", err)
		os.Exit(1)
	}
}
