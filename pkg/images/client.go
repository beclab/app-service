package images

import (
	"bytetrade.io/web3os/app-service/pkg/utils"
	"context"
	"errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"math"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"strconv"
	"strings"
	"time"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
)

type ImageManager interface {
	Create(ctx context.Context, am *appv1alpha1.ApplicationManager, refs []appv1alpha1.Ref) error
	UpdateStatus(ctx context.Context, name, state, message string) error
	PollDownloadProgress(ctx context.Context, am *appv1alpha1.ApplicationManager) error
}

type ImageManagerClient struct {
	client.Client
}

func NewImageManager(client client.Client) ImageManager {
	return &ImageManagerClient{client}
}

func (imc *ImageManagerClient) Create(ctx context.Context, am *appv1alpha1.ApplicationManager, refs []appv1alpha1.Ref) error {
	var nodes corev1.NodeList
	err := imc.List(ctx, &nodes, &client.ListOptions{})
	if err != nil {
		return err
	}
	nodeList := make([]string, 0)
	for _, node := range nodes.Items {
		if !utils.IsNodeReady(&node) || node.Spec.Unschedulable {
			continue
		}
		nodeList = append(nodeList, node.Name)
	}
	if len(nodeList) == 0 {
		return errors.New("cluster has no suitable node to schedule")
	}
	var im appv1alpha1.ImageManager
	err = imc.Get(ctx, types.NamespacedName{Name: am.Name}, &im)
	if err == nil {
		err = imc.Delete(ctx, &im)
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
	}
	labels := make(map[string]string)
	if strings.HasSuffix(am.Spec.AppName, "-dev") && am.Spec.Source == "devbox" {
		labels["dev.bytetrade.io/dev-owner"] = am.Spec.AppOwner
	}
	m := appv1alpha1.ImageManager{
		ObjectMeta: metav1.ObjectMeta{
			Name:   am.Name,
			Labels: labels,
		},
		Spec: appv1alpha1.ImageManagerSpec{
			AppName:      am.Spec.AppName,
			AppNamespace: am.Spec.AppNamespace,
			AppOwner:     am.Spec.AppOwner,
			Refs:         refs,
			Nodes:        nodeList,
		},
	}
	err = imc.Client.Create(ctx, &m)
	if err != nil {
		return err
	}
	return nil
}

func (imc *ImageManagerClient) UpdateStatus(ctx context.Context, name, state, message string) error {
	var im appv1alpha1.ImageManager
	err := imc.Get(ctx, types.NamespacedName{Name: name}, &im)
	if err != nil {
		klog.Infof("get im err=%v", err)
		return err
	}

	now := metav1.Now()
	imCopy := im.DeepCopy()
	imCopy.Status.State = state
	imCopy.Status.Message = message
	imCopy.Status.StatusTime = &now
	imCopy.Status.UpdateTime = &now

	err = imc.Status().Patch(ctx, imCopy, client.MergeFrom(&im))
	if err != nil {
		return err
	}
	return nil
}

func (imc *ImageManagerClient) PollDownloadProgress(ctx context.Context, am *appv1alpha1.ApplicationManager) error {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			var im appv1alpha1.ImageManager
			err := imc.Get(ctx, types.NamespacedName{Name: am.Name}, &im)
			if err != nil {
				klog.Infof("Failed to get imanagermanagers name=%s err=%v", am.Name, err)
				return err
			}

			if im.Status.State == appv1alpha1.ImFailed.String() {
				return errors.New(im.Status.Message)
			}

			type progress struct {
				offset int64
				total  int64
			}
			maxImageSize := int64(50173680066)

			nodeMap := make(map[string]*progress)
			for _, nodeName := range im.Spec.Nodes {
				for _, ref := range im.Spec.Refs {
					t := im.Status.Conditions[nodeName][ref.Name]["total"]
					if t == "" {
						t = strconv.FormatInt(maxImageSize, 10)
					}

					total, _ := strconv.ParseInt(t, 10, 64)

					t = im.Status.Conditions[nodeName][ref.Name]["offset"]
					if t == "" {
						t = "0"
					}
					offset, _ := strconv.ParseInt(t, 10, 64)

					if _, ok := nodeMap[nodeName]; ok {
						nodeMap[nodeName].offset += offset
						nodeMap[nodeName].total += total
					} else {
						nodeMap[nodeName] = &progress{offset: offset, total: total}
					}
				}
			}
			ret := math.MaxFloat64
			for _, p := range nodeMap {
				var nodeProgress float64
				if p.total != 0 {
					nodeProgress = float64(p.offset) / float64(p.total)
				}
				if nodeProgress < ret {
					ret = nodeProgress * 100
				}

			}

			var cur appv1alpha1.ApplicationManager
			err = imc.Get(ctx, types.NamespacedName{Name: am.Name}, &cur)
			if err != nil {
				klog.Infof("Failed to get applicationmanagers name=%v, err=%v", am.Name, err)
				continue
			}

			appMgrCopy := cur.DeepCopy()
			oldProgress, _ := strconv.ParseFloat(cur.Status.Progress, 64)
			if oldProgress > ret {
				continue
			}
			cur.Status.Progress = strconv.FormatFloat(ret, 'f', 2, 64)
			err = imc.Status().Patch(ctx, &cur, client.MergeFrom(appMgrCopy))
			if err != nil {
				klog.Infof("Failed to patch applicationmanagers name=%v, err=%v", am.Name, err)
				continue
			}

			klog.Infof("app %s download progress.... %v", am.Spec.AppName, cur.Status.Progress)
			if cur.Status.Progress == "100.00" {
				return nil
			}
		case <-ctx.Done():
			return context.Canceled
		}
	}
}
