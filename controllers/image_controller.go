package controllers

//
import (
	"context"
	"errors"
	"sync"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/images"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// ImageManagerController represents a controller for managing the lifecycle of applicationmanager.
type ImageManagerController struct {
	client.Client
}

// SetupWithManager sets up the ImageManagerController with the provided controller manager
func (r *ImageManagerController) SetupWithManager(mgr ctrl.Manager) error {
	c, err := controller.New("image-controller", mgr, controller.Options{
		Reconciler: r,
	})
	if err != nil {
		return err
	}

	err = c.Watch(
		&source.Kind{Type: &appv1alpha1.ImageManager{}},
		handler.EnqueueRequestsFromMapFunc(
			func(h client.Object) []reconcile.Request {
				app, ok := h.(*appv1alpha1.ImageManager)
				if !ok {
					return nil
				}
				return []reconcile.Request{{NamespacedName: types.NamespacedName{
					Name:      app.Name,
					Namespace: app.Spec.AppOwner,
				}}}
			}),
		predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return r.preEnqueueCheckForCreate(e.Object)
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return r.preEnqueueCheckForUpdate(e.ObjectOld, e.ObjectNew)
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
		},
	)

	if err != nil {
		klog.Errorf("Failed to add watch err=%v", err)
		return err
	}

	return nil
}

var imageManager map[string]context.CancelFunc

// Reconcile implements the reconciliation loop for the ImageManagerController
func (r *ImageManagerController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	ctrl.Log.Info("reconcile image manager request", "name", req.Name)

	var im appv1alpha1.ImageManager
	err := r.Get(ctx, req.NamespacedName, &im)

	if err != nil {
		ctrl.Log.Error(err, "get application manager error", "name", req.Name)
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		// unexpected error, retry after 5s
		return ctrl.Result{}, err
	}
	r.reconcile(&im)

	return reconcile.Result{}, nil
}

func (r *ImageManagerController) reconcile(instance *appv1alpha1.ImageManager) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	defer func() {
		delete(imageManager, instance.Name)
	}()
	var err error

	var cur appv1alpha1.ImageManager
	err = r.Get(ctx, types.NamespacedName{Name: instance.Name}, &cur)
	if err != nil {
		klog.Errorf("Failed to get imagemanagers name=%s err=%v", instance.Name, err)
		return
	}

	if imageManager == nil {
		imageManager = make(map[string]context.CancelFunc)
	}
	if _, ok := imageManager[instance.Name]; ok {
		return
	}
	imageManager[instance.Name] = cancel

	refs := cur.Spec.Refs

	conditions := make([]appv1alpha1.ImageProgress, 0)

	var nodes corev1.NodeList
	err = r.List(ctx, &nodes, &client.ListOptions{})
	if err != nil {
		klog.Infof("Failed to list err=%v", err)
		return
	}
	for _, ref := range refs {
		for _, node := range nodes.Items {
			conditions = append(conditions, appv1alpha1.ImageProgress{
				NodeName: node.Name,
				ImageRef: ref,
				Progress: "0.00",
			})
		}
	}
	now := metav1.Now()
	curCopy := cur.DeepCopy()
	curCopy.Status.Conditions = conditions
	curCopy.Status.StatusTime = &now
	curCopy.Status.UpdateTime = &now
	curCopy.Status.State = appv1alpha1.Downloading.String()
	err = r.Status().Patch(ctx, curCopy, client.MergeFrom(&cur))
	if err != nil {
		klog.Error(err)
		return
	}
	err = r.Get(ctx, types.NamespacedName{Name: instance.Name}, &cur)
	if err != nil {
		klog.Error(err)
		return
	}

	err = r.download(ctx, refs, images.PullOptions{AppName: instance.Spec.AppName, OwnerName: instance.Spec.AppOwner})
	if err != nil {
		klog.Infof("download failed err=%v", err)

		state := appv1alpha1.Failed.String()
		if errors.Is(err, context.Canceled) {
			state = appv1alpha1.Canceled.String()
		}
		err = r.updateStatus(instance, state, err.Error())
		if err != nil {
			klog.Infof("Failed to update status err=%v", err)
		}
		return
	}
	err = r.updateStatus(instance, appv1alpha1.Completed.String(), "success")
	//err = r.install(ctx, instance)
	if err != nil {
		klog.Error(err)
	}

	klog.Infof("download image success")
	return
}

func (r *ImageManagerController) preEnqueueCheckForCreate(obj client.Object) bool {
	im, _ := obj.(*appv1alpha1.ImageManager)
	klog.Infof("enqueue check: %v", im.Status)
	if im.Status.State == appv1alpha1.Failed.String() || im.Status.State == appv1alpha1.Canceled.String() || im.Status.State == appv1alpha1.Completed.String() {
		return false
	}
	return true
}

func (r *ImageManagerController) preEnqueueCheckForUpdate(old, new client.Object) bool {
	im, _ := new.(*appv1alpha1.ImageManager)
	if im.Status.State == "canceled" {
		go r.reconcile2(im)
	}
	return false
}
func (r *ImageManagerController) reconcile2(cur *appv1alpha1.ImageManager) {
	r.cancel(cur)
}

func (r *ImageManagerController) download(ctx context.Context, refs []string, opts images.PullOptions) (err error) {
	var wg sync.WaitGroup
	var errs []error
	for _, ref := range refs {
		wg.Add(1)
		go func(ref string) {
			defer wg.Done()
			iClient, ctx, cancel := images.NewClientOrDie(ctx)
			defer cancel()
			_, err = iClient.PullImage(ctx, ref, opts)
			if err != nil {
				errs = append(errs, err)
				klog.Infof("pull image failed name=%v err=%v", ref, err)
			}
		}(ref)
	}
	klog.Infof("waiting image %v to download", refs)
	wg.Wait()
	return err
}

func (r *ImageManagerController) updateStatus(im *appv1alpha1.ImageManager, state string, message string) error {
	var err error
	err = r.Get(context.TODO(), types.NamespacedName{Name: im.Name}, im)
	if err != nil {
		return err
	}

	now := metav1.Now()
	imCopy := im.DeepCopy()
	imCopy.Status.State = state
	imCopy.Status.Message = message
	imCopy.Status.StatusTime = &now
	imCopy.Status.UpdateTime = &now

	err = r.Status().Patch(context.TODO(), imCopy, client.MergeFrom(im))
	if err != nil {
		return err
	}
	return nil
}

func (r *ImageManagerController) cancel(im *appv1alpha1.ImageManager) error {
	cancel, ok := imageManager[im.Name]
	if !ok {
		return errors.New("can not execute cancel")
	}
	cancel()
	return nil
}
