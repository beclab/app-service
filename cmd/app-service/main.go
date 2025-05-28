package main

import (
	"bytetrade.io/web3os/app-service/pkg/images"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/generated/clientset/versioned"

	sysv1alpha1 "bytetrade.io/web3os/app-service/api/sys.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/controllers"
	"bytetrade.io/web3os/app-service/pkg/apiserver"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(appv1alpha1.AddToScheme(scheme))
	utilruntime.Must(sysv1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}

const (
	kubeSphereHostAddr = "KS_APISERVER_SERVICE_HOST" // env name in cluster
	kubeSphereHostPort = "KS_APISERVER_SERVICE_PORT"
)

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":6080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":6081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.RFC3339TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	config := ctrl.GetConfigOrDie()

	mgr, err := ctrl.NewManager(config, ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "5117a667.bytetrade.io",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "Unable to start manager")
		os.Exit(1)
	}

	appClient := versioned.NewForConfigOrDie(config)
	ictx, cancelFunc := context.WithCancel(context.Background())

	if err = (&controllers.ApplicationReconciler{
		Client:       mgr.GetClient(),
		Scheme:       mgr.GetScheme(),
		AppClientset: appClient,
		Kubeconfig:   config,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Unable to create controller", "controller", "Application")
		os.Exit(1)
	}

	if err = (&controllers.SecurityReconciler{
		Client:        mgr.GetClient(),
		Scheme:        mgr.GetScheme(),
		DynamicClient: dynamic.NewForConfigOrDie(config),
	}).SetupWithManager(ictx, mgr); err != nil {
		setupLog.Error(err, "Unable to create controller", "controller", "Security")
		os.Exit(1)
	}

	if err = (&controllers.ApplicationManagerController{
		Client:      mgr.GetClient(),
		KubeConfig:  config,
		ImageClient: images.NewImageManager(mgr.GetClient()),
		//Manager:    make(map[string]context.CancelFunc),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Unable to create controller", "controller", "Application Manager")
		os.Exit(1)
	}

	if err = (&controllers.EntranceStatusManagerController{
		Client: mgr.GetClient(),
	}).SetUpWithManager(mgr); err != nil {
		setupLog.Error(err, "Unable to create controller", "controller", "EntranceStatus Manager")
		os.Exit(1)
	}

	if err = (&controllers.EvictionManagerController{
		Client: mgr.GetClient(),
	}).SetUpWithManager(mgr); err != nil {
		setupLog.Error(err, "Unable to create controller", "controller", "Eviction Manager")
		os.Exit(1)
	}

	if err = (&controllers.TailScaleACLController{
		Client: mgr.GetClient(),
	}).SetUpWithManager(mgr); err != nil {
		setupLog.Error(err, "Unable to create controller", "controller", "tailScaleACLA manager")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Unable to set up ready check")
		os.Exit(1)
	}

	// sync the api server and the manager with context
	errCh := make(chan error) // api server error
	defer close(errCh)

	c := make(chan os.Signal, 2)
	signal.Notify(c, shutdownSignals...)
	go func() {
		select {
		case <-c:
			cancelFunc()
			<-c
			os.Exit(1) // second signal. Exit directly.
		case err := <-errCh:
			cancelFunc()
			setupLog.Error(err, "Unable to running api server")
			os.Exit(1)
		}
	}()

	// api server run with request's token
	// get kubesphere host from env or config file
	ksHost := os.Getenv(kubeSphereHostAddr)
	ksPort := os.Getenv(kubeSphereHostPort)
	if ksHost == "" || ksPort == "" {
		cancelFunc()
		setupLog.Error(err, "Failed to get the kubesphere api server host from env")
		os.Exit(1)
	}

	// start api server
	func(ctx context.Context, errCh chan error, ksHost string, kubeConfig *rest.Config) {
		go func() {
			if err := runAPIServer(ctx, ksHost, kubeConfig, mgr.GetClient()); err != nil {
				errCh <- err
			}
		}()
	}(ictx, errCh, fmt.Sprintf("%s:%s", ksHost, ksPort), config)

	setupLog.Info("Starting manager")
	if err := mgr.Start(ictx); err != nil {
		cancelFunc()
		setupLog.Error(err, "Unable to running manager")
		os.Exit(1)
	}

	cancelFunc()
}

func runAPIServer(ctx context.Context, ksHost string, kubeConfig *rest.Config, client client.Client) error {
	server, err := apiserver.New(ctx)
	if err != nil {
		return err
	}

	stopCh := make(chan struct{})
	defer close(stopCh)

	err = server.PrepareRun(ksHost, kubeConfig, client, stopCh)
	if err != nil {
		return err
	}

	err = server.Run()
	return err
}
