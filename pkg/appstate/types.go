package appstate

import (
	"bytetrade.io/web3os/app-service/pkg/appcfg"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"context"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sync"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type StatefulApp interface {
	GetManager() *appsv1.ApplicationManager
	State() string
	Cancel(ctx context.Context) error
	Exec(ctx context.Context, c chan<- error)
	HandleContext(ctx context.Context, c chan<- error, done chan struct{})
	StateReconcile(ctx context.Context) error
}

var (
	cancelManager     = make(map[string]context.CancelFunc)
	cancelManagerLock sync.RWMutex
)

func StoreCancelFunc(name string, cancelFunc context.CancelFunc) {
	cancelManagerLock.Lock()
	defer cancelManagerLock.Unlock()
	cancelManager[name] = cancelFunc
}

func LoadCancelFunc(name string) (context.CancelFunc, bool) {
	cancelManagerLock.Lock()
	defer cancelManagerLock.Unlock()
	cancelFunc, ok := cancelManager[name]
	return cancelFunc, ok
}

func DelCancelFunc(name string) {
	cancelManagerLock.Lock()
	defer cancelManagerLock.Unlock()
	delete(cancelManager, name)
}

type baseStatefulApp struct {
	//app     *appsv1.Application
	manager *appsv1.ApplicationManager
	client  client.Client
}

func (b *baseStatefulApp) updateStatus(ctx context.Context, am *appsv1.ApplicationManager, state appsv1.ApplicationManagerState,
	opRecord *appsv1.OpRecord, message string) error {
	var err error

	err = b.client.Get(ctx, types.NamespacedName{Name: am.Name}, am)
	if err != nil {
		return err
	}
	//appState := ""
	//klog.Infof("am.status.state: %s, state: %s", am.Status.State, state)
	//if sta, ok := appsv1.IsAppState(state); ok {
	//	klog.Infof("am.status.statesta...: %s", sta)
	//
	//	appState = sta.String()
	//}
	//klog.Infof("appstate in updateStatus: %v", appState)

	now := metav1.Now()
	amCopy := am.DeepCopy()
	amCopy.Status.State = state
	amCopy.Status.Message = message
	amCopy.Status.StatusTime = &now
	amCopy.Status.UpdateTime = &now
	amCopy.Status.OpGeneration += 1
	if opRecord != nil {
		amCopy.Status.OpRecords = append([]appsv1.OpRecord{*opRecord}, amCopy.Status.OpRecords...)
	}
	if len(amCopy.Status.OpRecords) > 20 {
		amCopy.Status.OpRecords = amCopy.Status.OpRecords[:20:20]
	}
	err = b.client.Status().Patch(ctx, amCopy, client.MergeFrom(am))
	if err != nil {
		return err
	}

	//TODO: remove after
	var nn appsv1.ApplicationManager
	err = b.client.Get(context.TODO(), types.NamespacedName{Name: am.Name}, &nn)
	if err != nil {
		return err
	}
	klog.Infof("nnnn... %v", nn.Status)

	//var app appsv1.Application
	//err = b.client.Get(ctx, types.NamespacedName{Name: am.Name}, &app)
	//if err == nil {
	//	klog.Infof("appstate in updateStatus22222: %v", appState)
	//
	//	if appsv1.AppStateCollect.Has(appState) {
	//		err = utils.UpdateAppState(ctx, am, appState)
	//		klog.Infof("update app state.... %v", err)
	//		if err != nil {
	//			return err
	//		}
	//	}
	//} else if !apierrors.IsNotFound(err) {
	//	return err
	//}
	return nil
}

func (p *baseStatefulApp) forceDeleteApp(ctx context.Context) error {
	token := p.manager.Status.Payload["token"]
	appCfg := &appcfg.ApplicationConfig{
		AppName:   p.manager.Spec.AppName,
		Namespace: p.manager.Spec.AppNamespace,
		OwnerName: p.manager.Spec.AppOwner,
	}

	kubeConfig, err := ctrl.GetConfig()
	if err != nil {
		klog.Errorf("get kube config failed %v", err)
		return err
	}
	ops, err := appinstaller.NewHelmOps(ctx, kubeConfig, appCfg, token, appinstaller.Opt{})
	if err != nil {
		klog.Errorf("make helm ops failed %v", err)
		return err
	}
	err = ops.Uninstall()
	if err != nil {
		klog.Errorf("uninstall app %s failed err %v", appCfg.AppName, err)
		return err
	}
	err = p.updateStatus(ctx, p.manager, appsv1.Uninstalled, nil, appsv1.Uninstalled.String())
	if err != nil {
		klog.Errorf("update app manager %s to state %s failed", p.manager.Name, appsv1.Uninstalled)
		return err
	}
	return nil
}
