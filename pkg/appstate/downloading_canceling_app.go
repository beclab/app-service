package appstate

import (
	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"context"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ StatefulApp = &DownloadingCancelingApp{}

type DownloadingCancelingApp struct {
	StatefulApp
	baseStatefulApp
}

func (p *DownloadingCancelingApp) State() string {
	return p.GetManager().Status.State.String()
}

func (p *DownloadingCancelingApp) GetManager() *appsv1.ApplicationManager {
	return p.manager
}

func NewDownloadingCancelingApp(client client.Client,
	manager *appsv1.ApplicationManager) StatefulApp {

	return &DownloadingCancelingApp{
		baseStatefulApp: baseStatefulApp{
			manager: manager,
			client:  client,
		},
	}
}

func (p *DownloadingCancelingApp) Exec(ctx context.Context, c chan<- error) {
	cancelFunc, ok := LoadCancelFunc(p.manager.Name)
	if ok {
		cancelFunc()
	} else {
		klog.Infof("cancelFunc for cancel downloading not found")
	}

	updateErr := p.updateStatus(ctx, p.manager, appsv1.DownloadingCanceled, nil, appsv1.DownloadingCanceled.String())
	if updateErr != nil {
		klog.Errorf("update app manager %s to %s state failed %v", p.manager.Name, appsv1.DownloadingCanceled.String(), updateErr)
		c <- updateErr
		return
	}
	//updateErr = p.updateImStatus(context.TODO(), p.manager.Name)
	//if updateErr != nil {
	//	klog.Errorf("update im manager %s to %s state failed %v", p.manager.Name, appsv1.DownloadingCanceled.String(), updateErr)
	//	c <- updateErr
	//	return
	//}
	c <- nil
	return
}

//
//func (p *DownloadingCancelingApp) updateImStatus(ctx context.Context, name string) error {
//	var im appsv1.ImageManager
//	err := p.client.Get(ctx, types.NamespacedName{Name: name}, &im)
//	if err != nil {
//		klog.Errorf("get im %s failed %v", name, err)
//		return err
//	}
//	imCopy := im.DeepCopy()
//	imCopy.Status.State = appsv1.ImCanceled.String()
//	now := metav1.Now()
//	imCopy.Status.StatusTime = &now
//	imCopy.Status.UpdateTime = &now
//	err = p.client.Status().Patch(ctx, imCopy, client.MergeFrom(&im))
//	if err != nil {
//		klog.Errorf("update im %s failed %v", name, err)
//		return err
//	}
//	klog.Infof("update im to downloadCanceled success.....")
//	return nil
//}

func (p *DownloadingCancelingApp) HandleContext(ctx context.Context, c chan<- error, done chan struct{}) {
	select {
	case <-ctx.Done():
		err := p.Cancel(context.TODO())
		if err != nil {
			klog.Errorf("app manager %s, do cancel operation failed %v", p.manager.Name, err)
		}
		c <- err
	case <-done:
		return
	}
}

func (p *DownloadingCancelingApp) Cancel(ctx context.Context) error {
	return nil
}
