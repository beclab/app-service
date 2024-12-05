package apiserver

import (
	"errors"
	"os"
	"time"

	"github.com/emicklei/go-restful/v3"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Describe the status of system upgrade.
const (
	StatusRunning       = "running"
	StatusPaused        = "paused"
	StatusPending       = "pending"
	StatusUpdating      = "updating"
	StatusStopped       = "stopped"
	StatusFailed        = "failed"
	StatusBound         = "bound"
	StatusLost          = "lost"
	StatusComplete      = "completed"
	StatusWarning       = "warning"
	StatusUnschedulable = "unschedulable"

	UpgradeJobName = "system-upgrade"
	JobNamespace   = "os-system"
)

type ResultResponse struct {
	Code int    `json:"code"`
	Data any    `json:"data"`
	Msg  string `json:"message"`
}

func responseError(resp *restful.Response, err error) {
	klog.Infof("Response err=%v", err)
	resp.WriteAsJson(&ResultResponse{
		Code: 1,
		Msg:  err.Error(),
	})
}

func (h *Handler) newVersion(req *restful.Request, resp *restful.Response) {
	mode := req.QueryParameter("dev_mode")

	isNew, curVer, newVer, err := h.upgrader.IsNewVersionValid(req.Request.Context(), h.ctrlClient, mode == "true")

	if err != nil {
		responseError(resp, err)
		return
	}

	newVerStr := ""
	if newVer != nil {
		newVerStr = newVer.String()
	}

	resp.WriteAsJson(
		&ResultResponse{
			Code: 0,
			Data: map[string]interface{}{
				"is_new":          isNew,
				"current_version": curVer.String(),
				"new_version":     newVerStr,
			},
		},
	)
}

func (h *Handler) upgradeState(req *restful.Request, resp *restful.Response) {
	var job batchv1.Job

	err := h.ctrlClient.Get(req.Request.Context(), types.NamespacedName{Namespace: JobNamespace, Name: UpgradeJobName}, &job)
	if err != nil {
		responseError(resp, err)
		return
	}

	state := jobStatus(&job)

	resp.WriteAsJson(
		&ResultResponse{
			Code: 0,
			Data: map[string]interface{}{
				"state": state,
			},
		},
	)
}

func (h *Handler) upgrade(req *restful.Request, resp *restful.Response) {
	var job batchv1.Job
	mode := req.QueryParameter("dev_mode")

	err := h.ctrlClient.Get(req.Request.Context(), types.NamespacedName{Namespace: JobNamespace, Name: UpgradeJobName}, &job)
	if client.IgnoreNotFound(err) != nil {
		responseError(resp, err)
		return
	}

	if err == nil {
		responseError(resp, errors.New("there is a system upgrade job is running. please wait"))
		return
	}

	ttl := int32(time.Duration(5 * time.Minute).Seconds())

	if apierrors.IsNotFound(err) {
		job = batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      UpgradeJobName,
				Namespace: JobNamespace,
			},
			Spec: batchv1.JobSpec{
				TTLSecondsAfterFinished: &ttl,
				BackoffLimit:            pointer.Int32(3),
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						ServiceAccountName: "os-internal",
						RestartPolicy:      corev1.RestartPolicyNever,
						Containers: []corev1.Container{
							{
								Name:            "upgrade",
								Image:           os.Getenv("JOB_IMAGE"),
								ImagePullPolicy: corev1.PullIfNotPresent,
								Env: []corev1.EnvVar{
									{
										Name:  "dev_mode",
										Value: mode,
									},
									{
										Name:  "OLARES_SPACE_URL",
										Value: os.Getenv("OLARES_SPACE_URL"),
									},
								},
							}, // container 1
						}, // end containers
					},
				}, // end template
			},
		} // end job define

		err := h.ctrlClient.Create(req.Request.Context(), &job)
		if err != nil {
			responseError(resp, err)
			return
		}

		resp.WriteAsJson(
			&ResultResponse{
				Code: 0,
				Msg:  "Start to upgrade system",
			},
		)

		return
	} // end if

	resp.WriteAsJson(
		&ResultResponse{
			Code: 1,
			Msg:  "failed to start upgrading",
		},
	)
}

func (h *Handler) upgradeCancel(req *restful.Request, resp *restful.Response) {
	resp.WriteAsJson(
		&ResultResponse{
			Code: 1,
			Msg:  "upgrade system cannot be canceled",
		},
	)
}

func jobStatus(item *batchv1.Job) string {
	status := StatusFailed
	if item.Status.Active > 0 {
		status = StatusRunning
	} else if item.Status.Failed > 0 {
		status = StatusFailed
	} else if item.Status.Succeeded > 0 {
		status = StatusComplete
	}
	return status
}
