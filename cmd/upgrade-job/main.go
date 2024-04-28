package main

import (
	"context"
	"flag"
	"os"

	sysv1alpha1 "bytetrade.io/web3os/app-service/api/sys.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/upgrade"

	"go.uber.org/zap/zapcore"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(sysv1alpha1.AddToScheme(scheme))

}

func main() {
	opts := zap.Options{
		Development: true,
		TimeEncoder: zapcore.RFC3339TimeEncoder,
	}
	opts.BindFlags(flag.CommandLine)

	klog.InitFlags(nil)

	flag.Parse()

	config := ctrl.GetConfigOrDie()

	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		panic(err)
	}

	mainCtx := context.Background()

	upgrader := upgrade.NewUpgrader()
	dev := os.Getenv("dev_mode")
	upgrader.DevMode = dev == "true"

	klog.Info("Start to upgrade")
	err = upgrader.UpgradeSystem(mainCtx, k8sClient)

	if err != nil {
		panic(err)
	}

	klog.Info("Success to upgrade")
}
