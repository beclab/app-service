package apiserver

import (
	"sync"

	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/appwatchers"
	"bytetrade.io/web3os/app-service/pkg/utils"

	"github.com/emicklei/go-restful/v3"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const backupCancelCode = 493

func (h *Handler) backupNew(req *restful.Request, resp *restful.Response) {
	pendingOrRunningTask, err := utils.GetPendingOrRunningTask(req.Request.Context())
	if err != nil {
		api.HandleError(resp, req, &errors.StatusError{
			ErrStatus: metav1.Status{Code: backupCancelCode, Message: "can not get pending/running task"},
		})
		return
	}
	switch {
	case len(pendingOrRunningTask) > 0,
		h.userspaceManager.HasRunningTask():

		api.HandleError(resp, req, &errors.StatusError{
			ErrStatus: metav1.Status{Code: backupCancelCode, Message: "app installer running"},
		})

		return
	}

	lockAppInstaller()
	resp.WriteAsJson(map[string]int{"code": 0})
}

func (h *Handler) backupFinish(req *restful.Request, resp *restful.Response) {
	klog.Info("Backup finished callback")
	unlockAppInstaller()

	resp.WriteAsJson(map[string]int{"code": 0})
}

var singleTask *sync.Once = &sync.Once{}

func (h *Handler) highload(req *restful.Request, resp *restful.Response) {
	var load struct {
		CPU    float64 `json:"cpu"`
		Memory float64 `json:"memory"`
	}

	err := req.ReadEntity(&load)
	if err != nil {
		klog.Errorf("Failed to read request err=%v", err)
		api.HandleBadRequest(resp, req, err)
		return
	}

	klog.Infof("System resources high load cpu=%v memory=%v", load.CPU, load.Memory)

	// start application suspending task
	singleTask.Do(func() {
		go func() {
			err := appwatchers.SuspendTopApp(h.serviceCtx, h.ctrlClient)
			if err != nil {
				klog.Errorf("Failed to suspend applications err=%v", err)
			}
			singleTask = &sync.Once{}
		}()
	})

	resp.WriteAsJson(map[string]int{"code": 0})

}

var userSingleTask = &sync.Once{}

func (h *Handler) userHighLoad(req *restful.Request, resp *restful.Response) {
	var load struct {
		CPU    float64 `json:"cpu"`
		Memory float64 `json:"memory"`
		User   string  `json:"user"`
	}

	err := req.ReadEntity(&load)
	if err != nil {
		klog.Errorf("Failed to read request err=%v", err)
		api.HandleBadRequest(resp, req, err)
		return
	}
	klog.Infof("User: %s resources high load, cpu %.2f, mem %.2f", load.User, load.CPU, load.Memory)

	userSingleTask.Do(func() {
		go func() {
			err := appwatchers.SuspendUserTopApp(h.serviceCtx, h.ctrlClient, load.User)
			if err != nil {
				klog.Errorf("Failed to suspend application user=%s err=%v", load.User, err)
			}
			userSingleTask = &sync.Once{}
		}()
	})
	resp.WriteAsJson(map[string]int{"code": 0})
}
