// Package boxcutter contains the boxcutter integration test suite.
package boxcutter

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"pkg.package-operator.run/cardboard/kubeutils/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultWaitTimeout  = 20 * time.Second
	defaultWaitInterval = 1 * time.Second

	fieldOwner   = "boxcutter.test"
	systemPrefix = "boxcutter.test"
)

var (
	// Client pointing to the e2e test cluster.
	Client client.Client

	// DiscoveryClient pointing to the e2e test cluster.
	DiscoveryClient *discovery.DiscoveryClient

	// Config is the REST config used to connect to the cluster.
	Config *rest.Config
	// Scheme used by created clients.
	Scheme = runtime.NewScheme()

	// Waiter implementation to wait for object states on the cluster.
	Waiter *wait.Waiter
)

func init() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := initClients(ctx); err != nil {
		panic(err)
	}

	Waiter = wait.NewWaiter(Client, Scheme, wait.WithTimeout(defaultWaitTimeout), wait.WithInterval(defaultWaitInterval))
}

func initClients(_ context.Context) error {
	// Client/Scheme setup.
	AddToSchemes := runtime.SchemeBuilder{
		scheme.AddToScheme,
	}
	if err := AddToSchemes.AddToScheme(Scheme); err != nil {
		return fmt.Errorf("could not load schemes: %w", err)
	}

	var err error

	Config, err = ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("get rest config: %w", err)
	}

	Client, err = client.New(Config, client.Options{Scheme: Scheme})
	if err != nil {
		return fmt.Errorf("creating runtime client: %w", err)
	}

	DiscoveryClient, err = discovery.NewDiscoveryClientForConfig(Config)
	if err != nil {
		return fmt.Errorf("creating discovery client: %w", err)
	}

	return nil
}
