package controllers

import (
	"bytetrade.io/web3os/app-service/pkg/utils/registry"
	"context"
	"errors"
	"fmt"

	"os"
	"time"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/utils"
	imagetypes "github.com/containers/image/v5/types"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"

	"github.com/containerd/containerd"
	"github.com/containerd/containerd/errdefs"
	refdocker "github.com/containerd/containerd/reference/docker"
	"github.com/containers/image/v5/image"
	"github.com/containers/image/v5/transports/alltransports"
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

// AppImageInfoController represents a controller for managing the lifecycle of appimage.
type AppImageInfoController struct {
	client.Client
	imageClient *containerd.Client
}

// SetupWithManager sets up the ImageManagerController with the provided controller manager
func (r *AppImageInfoController) SetupWithManager(mgr ctrl.Manager) error {
	c, err := controller.New("app-image-controller", mgr, controller.Options{
		Reconciler: r,
	})
	if err != nil {
		klog.Errorf("set up app-image-controller failed %v", err)
		return err
	}
	if r.imageClient == nil {
		r.imageClient, err = containerd.New("/var/run/containerd/containerd.sock", containerd.WithDefaultNamespace("k8s.io"),
			containerd.WithTimeout(10*time.Second))
		if err != nil {
			klog.Errorf("create image client failed %v", err)
			return err
		}
	}

	err = c.Watch(
		&source.Kind{Type: &appv1alpha1.AppImage{}},
		handler.EnqueueRequestsFromMapFunc(
			func(h client.Object) []reconcile.Request {
				app, ok := h.(*appv1alpha1.AppImage)
				if !ok {
					return nil
				}
				return []reconcile.Request{{NamespacedName: types.NamespacedName{
					Name: app.Name,
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

// Reconcile implements the reconciliation loop for the ImageManagerController
func (r *AppImageInfoController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)
	ctrl.Log.Info("reconcile app image request", "name", req.Name)
	klog.Infof("reconcile appimage request app: %s, time: %v", req.Name, time.Now())

	var am appv1alpha1.AppImage
	err := r.Get(ctx, req.NamespacedName, &am)

	if err != nil {
		ctrl.Log.Error(err, "get app image error", "name", req.Name)
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		// unexpected error, retry after 5s
		return ctrl.Result{}, err
	}

	err = r.reconcile(ctx, &am)
	if err != nil {
		ctrl.Log.Error(err, "get app image info error", "name", req.Name)
	}

	return reconcile.Result{}, nil
}

func (r *AppImageInfoController) reconcile(ctx context.Context, instance *appv1alpha1.AppImage) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var err error

	var cur appv1alpha1.AppImage
	err = r.Get(ctx, types.NamespacedName{Name: instance.Name}, &cur)
	if err != nil {
		klog.Errorf("Failed to get app manager name=%s err=%v", instance.Name, err)
		return err
	}
	if areAllNodesCompleted(cur.Spec, cur.Status.Conditions) {
		klog.Infof("all node completed app %s", instance.Name)
		err = r.updateStatus(ctx, &cur, []appv1alpha1.ImageInfo{}, "completed", "completed")
		if err != nil {
			klog.Errorf("update appimage status failed %v", err)
		}
		return nil
	}

	state, message := "completed", "completed"
	klog.Infof("start to get image and layer info app: %s, time: %v", instance.Name, time.Now())
	imageInfos, err := r.GetImageInfo(ctx, cur.Spec.Refs)
	if err != nil {
		state = "failed"
		message = err.Error()
		klog.Errorf("get image info failed %v", err)
	}
	klog.Infof("finished to get image and layer info app: %s, time: %v", instance.Name, time.Now())

	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err = r.Get(ctx, types.NamespacedName{Name: instance.Name}, &cur)
		if err != nil {
			return err
		}
		err = r.updateStatus(ctx, &cur, imageInfos, state, message)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		klog.Infof("update app image status failed %v", err)
		return err
	}

	klog.Infof("get app %s image info success", cur.Name)
	return nil
}

func (r *AppImageInfoController) preEnqueueCheckForCreate(obj client.Object) bool {
	am, _ := obj.(*appv1alpha1.AppImage)
	klog.Infof("enqueue check: %v", am.Status.State)
	if am.Status.State == "completed" || am.Status.State == "failed" {
		return false
	}
	return true
}

func (r *AppImageInfoController) preEnqueueCheckForUpdate(old, new client.Object) bool {
	return false
}

func (r *AppImageInfoController) updateStatus(ctx context.Context, am *appv1alpha1.AppImage, imageInfos []appv1alpha1.ImageInfo, state, message string) error {
	var err error
	err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
		err = r.Get(ctx, types.NamespacedName{Name: am.Name}, am)
		if err != nil {
			return err
		}

		now := metav1.Now()
		amCopy := am.DeepCopy()
		amCopy.Status.State = state
		amCopy.Status.StatueTime = &now
		amCopy.Status.Images = append(amCopy.Status.Images, imageInfos...)
		amCopy.Status.Message = message
		node := os.Getenv("NODE_NAME")
		amCopy.Status.Conditions = append(amCopy.Status.Conditions, appv1alpha1.Condition{
			Node:      node,
			Completed: true,
		})

		err = r.Status().Patch(ctx, amCopy, client.MergeFrom(am))
		if err != nil {
			return err
		}
		return nil
	})

	return err
}

func areAllNodesCompleted(spec appv1alpha1.ImageSpec, conditions []appv1alpha1.Condition) bool {
	conditionMap := make(map[string]bool)
	for _, condition := range conditions {
		conditionMap[condition.Node] = condition.Completed
	}
	for _, node := range spec.Nodes {
		completed := conditionMap[node]
		if !completed {
			return false
		}
	}
	return true
}

func parseImageSource(ctx context.Context, name string) (imagetypes.ImageSource, error) {
	ref, err := alltransports.ParseImageName(name)
	if err != nil {
		return nil, err
	}
	sys := newSystemContext()
	if err != nil {
		return nil, err
	}
	return ref.NewImageSource(ctx, sys)
}

func (r *AppImageInfoController) GetManifest(ctx context.Context, imageName string) (*imagetypes.ImageInspectInfo, error) {
	var src imagetypes.ImageSource
	srcImageName := "docker://" + imageName
	sysCtx := newSystemContext()
	fmt.Printf("imageName: %s\n", imageName)
	src, err := parseImageSource(ctx, srcImageName)
	if err != nil {
		klog.Infof("parse Image Source: %v", err)
		return nil, err
	}
	unparsedInstance := image.UnparsedInstance(src, nil)
	_, _, err = unparsedInstance.Manifest(ctx)
	if err != nil {
		klog.Infof("parse manifest: %v", err)

		return nil, err
	}
	img, err := image.FromUnparsedImage(ctx, sysCtx, unparsedInstance)
	if err != nil {
		klog.Infof("from unparsed image: %v", err)

		return nil, err
	}
	imgInspect, err := img.Inspect(ctx)
	if err != nil {
		klog.Infof("inspect image: %v", err)

		return nil, err
	}

	return imgInspect, err
}

func newSystemContext() *imagetypes.SystemContext {
	return &imagetypes.SystemContext{}
}

func (r *AppImageInfoController) GetImageInfo(ctx context.Context, refs []string) ([]appv1alpha1.ImageInfo, error) {
	nodeName := os.Getenv("NODE_NAME")
	imageInfos := make([]appv1alpha1.ImageInfo, 0)
	for _, originRef := range refs {
		name, _ := refdocker.ParseDockerRef(originRef)
		replacedRef, _ := utils.ReplacedImageRef(registry.GetMirrors(), name.String(), false)

		manifest, err := r.GetManifest(ctx, replacedRef)
		if err != nil {
			klog.Infof("get image %s manifest failed %v", name.String(), err)
			return imageInfos, err
		}

		imageInfo := appv1alpha1.ImageInfo{
			Node:         nodeName,
			Name:         originRef,
			Architecture: manifest.Architecture,
			Variant:      manifest.Architecture,
			Os:           manifest.Architecture,
		}
		imageLayers := make([]appv1alpha1.ImageLayer, 0)
		for _, layer := range manifest.LayersData {
			imageLayer := appv1alpha1.ImageLayer{
				MediaType:   layer.MIMEType,
				Digest:      layer.Digest.String(),
				Size:        layer.Size,
				Annotations: layer.Annotations,
			}
			_, err = r.imageClient.ContentStore().Info(ctx, layer.Digest)
			if err == nil {
				imageLayer.Offset = layer.Size
				imageLayers = append(imageLayers, imageLayer)
				// go next layer
				continue
			}
			if errors.Is(err, errdefs.ErrNotFound) {
				statuses, err := r.imageClient.ContentStore().ListStatuses(ctx)
				if err != nil {
					klog.Errorf("list statuses failed %v", err)
					return imageInfos, err
				}
				for _, status := range statuses {
					s := "layer-" + layer.Digest.String()
					if s == status.Ref {
						imageLayer.Offset = status.Offset
						break
					}
				}
			} else {
				klog.Infof("get content info failed %v", err)
				return imageInfos, err
			}
			imageLayers = append(imageLayers, imageLayer)
		}
		imageInfo.LayersData = imageLayers
		imageInfos = append(imageInfos, imageInfo)

	}
	return imageInfos, nil
}
