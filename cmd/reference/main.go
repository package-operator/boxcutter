package main

import (
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"pkg.package-operator.run/boxcutter/cmd/reference/internal"
)

func main() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	ourScheme := runtime.NewScheme()
	if err := scheme.AddToScheme(ourScheme); err != nil {
		panic(err)
	}

	ref := internal.NewReference(ourScheme, ctrl.GetConfigOrDie())
	if err := ref.Start(ctrl.SetupSignalHandler()); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "crashed: ", err)
		os.Exit(1)
	}
}
