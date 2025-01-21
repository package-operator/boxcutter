package main

import (
	"fmt"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"pkg.package-operator.run/boxcutter/test/reference"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func main() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))

	ourScheme := runtime.NewScheme()
	if err := scheme.AddToScheme(ourScheme); err != nil {
		panic(err)
	}

	ref := reference.NewReference(ourScheme, ctrl.GetConfigOrDie())
	if err := ref.Start(ctrl.SetupSignalHandler()); err != nil {
		fmt.Println("crashed: %w", err)
		os.Exit(1)
	}
}
