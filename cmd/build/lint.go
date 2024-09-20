package main

import (
	"context"

	"pkg.package-operator.run/cardboard/run"
	"pkg.package-operator.run/cardboard/sh"
)

// Lint is a collection of lint related functions.
type Lint struct{}

func (l Lint) goModTidy(workdir string) error {
	return shr.New(sh.WithWorkDir(workdir)).Run("go", "mod", "tidy")
}

func (l Lint) goModTidyAll(ctx context.Context) error {
	return mgr.ParallelDeps(ctx, run.Meth(l, l.goModTidyAll),
		run.Meth1(l, l.goModTidy, "."),
	)
}

func (Lint) glciFix() error {
	return shr.Run("golangci-lint", "run", "--fix", "./...")
}

func (Lint) glciCheck() error {
	return shr.Run("golangci-lint", "run", "./...")
}

func (Lint) validateGitClean() error {
	return shr.Run("git", "diff", "--quiet", "--exit-code")
}
