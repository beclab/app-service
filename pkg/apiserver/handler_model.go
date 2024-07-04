package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/appinstaller"
	"bytetrade.io/web3os/app-service/pkg/client/clientset"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/users/userspace"
	"bytetrade.io/web3os/app-service/pkg/utils"

	"github.com/emicklei/go-restful/v3"
	"github.com/go-resty/resty/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

type modelRequest struct {
	Source  api.AppSource `json:"source"`
	RepoURL string        `json:"repoUrl"`
}

func (h *Handler) submit(req *restful.Request, resp *restful.Response) {
	modelID := req.PathParameter(ParamModelID)

	type modelReq struct {
		RepoURL string `json:"repoUrl"`
	}
	insReq := &modelReq{}
	err := req.ReadEntity(insReq)
	if err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}

	token := req.HeaderParameter(constants.AuthorizationTokenKey)
	modelCfg, _, err := GetModelConfig(req.Request.Context(), modelID, insReq.RepoURL, "", token)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	modelCfgStr, err := json.Marshal(modelCfg)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	client := resty.New()
	response, err := client.R().SetFormData(map[string]string{
		"modelID":     modelID,
		"modelConfig": string(modelCfgStr)}).
		Post("http://nitro/nitro/model_config/submit")
	if err != nil {
		klog.Errorf("Failed to make request to submit modelConfig err=%v", err)
		api.HandleError(resp, req, err)
		return
	}

	if response.StatusCode() == http.StatusOK {
		resp.WriteEntity(api.InstallationResponse{
			Response: api.Response{Code: 200},
			Data:     api.InstallationResponseData{UID: modelID},
		})
		return
	}
	resp.WriteEntity(api.ResponseWithMsg{
		Response: api.Response{Code: int32(response.StatusCode())},
		Message:  string(response.Body()),
	})
}

func (h *Handler) installModel(req *restful.Request, resp *restful.Response) {
	modelID := req.PathParameter(ParamModelID)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	token := req.HeaderParameter(constants.AuthorizationTokenKey)

	insReq := &modelRequest{}
	err := req.ReadEntity(insReq)
	if err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}

	var a *v1alpha1.ApplicationManager
	modelMgr := &v1alpha1.ApplicationManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: utils.FmtModelMgrName(modelID),
		},
		Spec: v1alpha1.ApplicationManagerSpec{
			AppName:  modelID,
			AppOwner: owner,
			Source:   insReq.Source.String(),
			Type:     v1alpha1.Model,
		},
	}
	kClient, _ := utils.GetClient()

	a, err = kClient.AppV1alpha1().ApplicationManagers().Get(req.Request.Context(),
		utils.FmtModelMgrName(modelID), metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			api.HandleError(resp, req, err)
			return
		}
		a, err = kClient.AppV1alpha1().ApplicationManagers().Create(req.Request.Context(),
			modelMgr, metav1.CreateOptions{})
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}
	} else {
		patchData := map[string]interface{}{
			"spec": map[string]interface{}{
				"source": insReq.Source.String(),
			},
		}
		patchByte, err := json.Marshal(patchData)
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}
		_, err = kClient.AppV1alpha1().ApplicationManagers().Patch(req.Request.Context(),
			a.Name, types.MergePatchType, patchByte, metav1.PatchOptions{})
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}
	}
	var modelCfg *appinstaller.ModelConfig
	var appCfg *appinstaller.AppConfiguration

	defer func() {
		if err != nil {
			now := metav1.Now()
			opRecord := v1alpha1.OpRecord{
				OpType:     v1alpha1.InstallOp,
				Message:    fmt.Sprintf(constants.OperationFailedTpl, a.Status.OpType, err.Error()),
				Source:     a.Spec.Source,
				Version:    appCfg.Metadata.Version,
				Status:     v1alpha1.Failed,
				StatusTime: &now,
			}
			e := utils.UpdateStatus(a, v1alpha1.Failed, &opRecord, "", opRecord.Message)
			if e != nil {
				klog.Errorf("Failed to update applicationmanager status name=%s err=%v", a.Name, e)
			}
		}
	}()

	modelCfg, appCfg, err = GetModelConfig(req.Request.Context(), modelID, insReq.RepoURL, "", token)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	modelCfgStr, err := json.Marshal(modelCfg)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	client := resty.New()
	response, err := client.R().SetFormData(map[string]string{
		"modelID":     modelID,
		"modelConfig": string(modelCfgStr)}).
		Post("http://nitro/nitro/model_config/submit")
	if err != nil {
		klog.Errorf("Failed to make request to submit modelConfig err=%v", err)
		api.HandleError(resp, req, err)
		return
	}

	now := metav1.Now()
	statusModel := v1alpha1.ApplicationManagerStatus{
		OpType:  v1alpha1.InstallOp,
		State:   v1alpha1.Installing,
		Message: "try to download",
		Payload: map[string]string{
			"version": appCfg.Metadata.Version,
		},
		StatusTime: &now,
		UpdateTime: &now,
	}

	a, err = utils.UpdateAppMgrStatus(utils.FmtModelMgrName(modelID), statusModel)
	if err != nil {
		klog.Errorf("Failed to update applicationmanager status name=%s err=%v", a.Name, err)
	}

	now = metav1.Now()
	opRecord := v1alpha1.OpRecord{
		OpType:     v1alpha1.InstallOp,
		Message:    fmt.Sprintf(constants.InstallOperationCompletedTpl, a.Status.OpType, a.Spec.AppName),
		Source:     a.Spec.Source,
		Status:     v1alpha1.Completed,
		Version:    appCfg.Metadata.Version,
		StatusTime: &now,
	}
	response, err = client.R().Post(fmt.Sprintf("http://nitro/nitro/model/%s", modelID))
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	if response.StatusCode() != http.StatusOK {
		err = errors.New(response.String())
		resp.WriteEntity(api.ResponseWithMsg{
			Response: api.Response{Code: int32(response.StatusCode())},
			Message:  response.String(),
		})
		return
	}
	time.Sleep(1 * time.Second)
	go func() {
		timer := time.NewTicker(2 * time.Second)
		for {
			select {
			case <-timer.C:
				modelStatus, progress, err := getProgress(modelID, owner)
				if err != nil {
					opRecord.Message = fmt.Sprintf(constants.OperationFailedTpl, a.Status.OpType, err.Error())
					opRecord.Status = v1alpha1.Failed
					e := utils.UpdateStatus(a, v1alpha1.Failed, &opRecord, "", opRecord.Message)
					if e != nil {
						klog.Info("Failed to update applicationmanager status in install model err=%v", e)
					}
					return
				}
				klog.Infof("Get model ID=%s install progress status=%s progress=%v", modelID, modelStatus, progress)

				a, err = kClient.AppV1alpha1().ApplicationManagers().Get(context.TODO(),
					utils.FmtModelMgrName(modelID), metav1.GetOptions{})
				if err != nil {
					klog.Errorf("Failed to get applicationmanager name=%s err=%v", a.Name, err)
					return
				}
				cancelType := a.Status.Payload["cancelType"]

				if modelStatus == "no_installed" && int(progress) == 0 && cancelType != "" {
					opRecord.Message = constants.OperationCanceledByUserTpl
					if cancelType == "timeout" {
						opRecord.Message = constants.OperationCanceledByTerminusTpl
					}
					opRecord.Status = v1alpha1.Canceled
					opRecord.OpType = v1alpha1.CancelOp
					e := utils.UpdateStatus(a, v1alpha1.Canceled, &opRecord, "", opRecord.Message)
					if e != nil {
						klog.Errorf("Failed to update applicationmanager status name=%s err=%v", a.Name, e)
						return
					}
					now = metav1.Now()
					_, e = utils.UpdateAppMgrStatus(utils.FmtModelMgrName(modelID), v1alpha1.ApplicationManagerStatus{
						OpType:     v1alpha1.CancelOp,
						Progress:   "0.00",
						StatusTime: &now,
						UpdateTime: &now,
					})
					if e != nil {
						klog.Errorf("Failed to update applicationmanager status name=%s err=%v", a.Name, e)
					}
					return
				}

				// check applicationmanager again, because it's state can be changed by cancel operation,
				// if task has been canceled, return.
				a, err = kClient.AppV1alpha1().ApplicationManagers().Get(context.TODO(),
					utils.FmtModelMgrName(modelID), metav1.GetOptions{})
				if err != nil {
					klog.Errorf("Failed to get applicationmanager name=%s err=%v", a.Name, err)
					return
				}
				if a.Status.OpType == v1alpha1.CancelOp && a.Status.State == v1alpha1.Canceling {
					return
				}
				now = metav1.Now()
				state := v1alpha1.Installing
				message := "installing"
				modelstatus := v1alpha1.ApplicationManagerStatus{
					OpType:     v1alpha1.InstallOp,
					State:      state,
					Message:    message,
					Progress:   strconv.FormatFloat(progress, 'f', 2, 64),
					StatusTime: &now,
					UpdateTime: &now,
				}
				_, err = utils.UpdateAppMgrStatus(utils.FmtModelMgrName(modelID), modelstatus)
				if err != nil {
					klog.Errorf("Failed to update applicationmanager status name=%s err=%v", a.Name, err)
				}
				if int(progress) == 100 {
					opRecord.Message = fmt.Sprintf(constants.InstallOperationCompletedTpl, a.Spec.Type.String(), a.Spec.AppName)
					opRecord.Status = v1alpha1.Completed
					e := utils.UpdateStatus(a, v1alpha1.Completed, &opRecord, "", opRecord.Message)
					if e != nil {
						klog.Errorf("Failed to update applicationmanager status name=%s err=%v", a.Name, e)
						return
					}
					klog.Infof("Success to install model=%s", modelID)
					return
				}

			}
		}
	}()
	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: modelID},
	})
}

func (h *Handler) progressHandler(req *restful.Request, resp *restful.Response) {
	modelID := req.PathParameter(ParamModelID)
	owner := req.Attribute(constants.UserContextAttribute).(string)

	status, progress, err := getProgress(modelID, owner)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	resp.WriteAsJson(map[string]interface{}{
		"status":   status,
		"progress": strconv.FormatFloat(progress, 'f', 2, 64),
	})
}

func (h *Handler) resumeHandler(req *restful.Request, resp *restful.Response) {
	modelID := req.PathParameter(ParamModelID)

	var err error

	now := metav1.Now()
	status := v1alpha1.ApplicationManagerStatus{
		OpType:     v1alpha1.ResumeOp,
		State:      v1alpha1.Resuming,
		Message:    "try to load",
		StatusTime: &now,
		UpdateTime: &now,
	}

	a, err := utils.UpdateAppMgrStatus(utils.FmtModelMgrName(modelID), status)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	defer func() {
		if err != nil {
			now := metav1.Now()
			opRecord := v1alpha1.OpRecord{
				OpType:     v1alpha1.InstallOp,
				Message:    fmt.Sprintf(constants.ResumeOperationForModelFailedTpl, err.Error()),
				Source:     a.Spec.Source,
				Version:    a.Status.Payload["version"],
				Status:     v1alpha1.Failed,
				StatusTime: &now,
			}
			e := utils.UpdateStatus(a, v1alpha1.Failed, &opRecord, "", opRecord.Message)
			if e != nil {
				klog.Errorf("Failed to update applicationmanager status name=%s err=%v", a.Name, e)
			}
		}

	}()

	client := resty.New()
	response, err := client.SetTimeout(60 * time.Second).R().Post(fmt.Sprintf("http://nitro/nitro/model/%s/load", modelID))
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	now = metav1.Now()
	opRecord := v1alpha1.OpRecord{
		OpType:     v1alpha1.ResumeOp,
		Message:    fmt.Sprintf(constants.ResumeOperationForModelCompletedTpl, a.Spec.AppName),
		Source:     a.Spec.Source,
		Version:    a.Status.Payload["version"],
		Status:     v1alpha1.Completed,
		StatusTime: &now,
	}

	if response.StatusCode() != http.StatusOK {
		err = errors.New(response.String())
		api.HandleError(resp, req, err)
		return
	}

	err = utils.UpdateStatus(a, opRecord.Status, &opRecord, "", opRecord.Message)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: modelID},
	})
}

func (h *Handler) suspendHandler(req *restful.Request, resp *restful.Response) {
	modelID := req.PathParameter(ParamModelID)
	var err error
	now := metav1.Now()
	status := v1alpha1.ApplicationManagerStatus{
		OpType:     v1alpha1.SuspendOp,
		State:      v1alpha1.Stopping,
		Message:    "try to suspend",
		StatusTime: &now,
		UpdateTime: &now,
	}

	a, err := utils.UpdateAppMgrStatus(utils.FmtModelMgrName(modelID), status)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	defer func() {
		if err != nil {
			now := metav1.Now()
			opRecord := v1alpha1.OpRecord{
				OpType:     v1alpha1.InstallOp,
				Message:    fmt.Sprintf(constants.SuspendOperationForModelFailedTpl, err.Error()),
				Source:     a.Spec.Source,
				Version:    a.Status.Payload["version"],
				Status:     v1alpha1.Failed,
				StatusTime: &now,
			}
			e := utils.UpdateStatus(a, v1alpha1.Failed, &opRecord, "", opRecord.Message)
			if e != nil {
				klog.Errorf("Failed to update applicationmanager status name=%s err=%v", a.Name, e)
			}
		}
	}()

	client := resty.New()
	response, err := client.R().Post(fmt.Sprintf("http://nitro/nitro/model/%s/stop", modelID))
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	now = metav1.Now()
	opRecord := v1alpha1.OpRecord{
		OpType:     v1alpha1.SuspendOp,
		Message:    fmt.Sprintf(constants.SuspendOperationForModelCompletedTpl, modelID),
		Source:     a.Spec.Source,
		Version:    a.Status.Payload["version"],
		Status:     v1alpha1.Completed,
		StatusTime: &now,
	}

	if response.StatusCode() != http.StatusOK {
		err = errors.New(response.String())
		api.HandleError(resp, req, err)
		return
	}

	err = utils.UpdateStatus(a, opRecord.Status, &opRecord, "", opRecord.Message)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: modelID},
	})
}

func (h *Handler) uninstallHandler(req *restful.Request, resp *restful.Response) {
	modelID := req.PathParameter(ParamModelID)
	owner := req.Attribute(constants.UserContextAttribute).(string)

	now := metav1.Now()
	status := v1alpha1.ApplicationManagerStatus{
		OpType:     v1alpha1.UninstallOp,
		State:      v1alpha1.Uninstalling,
		Message:    "try to uninstall",
		StatusTime: &now,
		UpdateTime: &now,
	}

	a, err := utils.UpdateAppMgrStatus(utils.FmtModelMgrName(modelID), status)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	defer func() {
		if err != nil {
			now := metav1.Now()
			opRecord := v1alpha1.OpRecord{
				OpType:     v1alpha1.InstallOp,
				Message:    fmt.Sprintf(constants.OperationFailedTpl, a.Status.OpType, err.Error()),
				Source:     a.Spec.Source,
				Version:    a.Status.Payload["version"],
				Status:     v1alpha1.Failed,
				StatusTime: &now,
			}
			e := utils.UpdateStatus(a, v1alpha1.Failed, &opRecord, "", opRecord.Message)
			if e != nil {
				klog.Errorf("Failed to update applicationmanager status name=%s err=%v", a.Name, e)
			}
		}
	}()

	client := resty.New()

	modelStatus, _, _ := getProgress(modelID, owner)
	if modelStatus == v1alpha1.AppRunning.String() {
		r, e := client.R().Post(fmt.Sprintf("http://nitro/nitro/model/%s/stop", modelID))
		if e != nil {
			api.HandleError(resp, req, e)
			return
		}
		if r.StatusCode() != http.StatusOK {
			err = errors.New(r.String())
			api.HandleError(resp, req, err)
			return
		}
	}

	response, err := client.R().Delete(fmt.Sprintf("http://nitro/nitro/model/%s", modelID))
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	now = metav1.Now()
	opRecord := v1alpha1.OpRecord{
		OpType:     v1alpha1.UninstallOp,
		Message:    fmt.Sprintf(constants.UninstallOperationCompletedTpl, a.Spec.Type.String(), a.Spec.AppName),
		Source:     a.Spec.Source,
		Version:    a.Status.Payload["version"],
		Status:     v1alpha1.Completed,
		StatusTime: &now,
	}
	if response.StatusCode() != http.StatusOK {
		err = errors.New(response.String())
		api.HandleError(resp, req, err)
		return
	}

	err = utils.UpdateStatus(a, opRecord.Status, &opRecord, "", opRecord.Message)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: modelID},
	})
}

func (h *Handler) cancelHandler(req *restful.Request, resp *restful.Response) {
	modelID := req.PathParameter(ParamModelID)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	// type = timeout | operate
	cancelType := req.QueryParameter("type")
	if cancelType == "" {
		cancelType = "operate"
	}

	client := resty.New()

	modelStatus, _, _ := getProgress(modelID, owner)
	if modelStatus != v1alpha1.AppInstalling.String() {
		api.HandleBadRequest(resp, req, errors.New("only installing status can do cancel operate"))
		return
	}

	response, err := client.R().Delete(fmt.Sprintf("http://nitro/nitro/model/%s", modelID))
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	if response.StatusCode() != http.StatusOK {
		err = errors.New(response.String())
		api.HandleError(resp, req, err)
		return
	}
	now := metav1.Now()
	_, err = utils.UpdateAppMgrStatus(utils.FmtModelMgrName(modelID), v1alpha1.ApplicationManagerStatus{
		OpType: v1alpha1.CancelOp,
		State:  v1alpha1.Canceling,
		Payload: map[string]string{
			"cancelType": cancelType,
		},
		StatusTime: &now,
		UpdateTime: &now,
	})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: modelID},
	})
}

func (h *Handler) statusHandler(req *restful.Request, resp *restful.Response) {
	modelID := req.PathParameter(ParamModelID)

	client := resty.New()
	response, err := client.R().Get(fmt.Sprintf("http://nitro/nitro/model/%s", modelID))
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	if response.StatusCode() != http.StatusOK {
		resp.WriteEntity(api.ResponseWithMsg{
			Response: api.Response{Code: int32(response.StatusCode())},
			Message:  string(response.Body()),
		})
		return
	}
	var ret map[string]interface{}
	err = json.Unmarshal(response.Body(), &ret)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	resp.WriteAsJson(ret)
}

func (h *Handler) models(req *restful.Request, resp *restful.Response) {
	client := resty.New()
	response, err := client.R().Get("http://nitro/nitro/model")
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	if response.StatusCode() != http.StatusOK {
		resp.WriteEntity(api.ResponseWithMsg{
			Response: api.Response{Code: int32(response.StatusCode())},
			Message:  string(response.Body()),
		})
		return
	}
	var ret []map[string]interface{}
	err = json.Unmarshal(response.Body(), &ret)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	resp.WriteAsJson(ret)
}

func (h *Handler) modelHandler(req *restful.Request, resp *restful.Response) {
	modelID := req.PathParameter(ParamModelID)

	client := resty.New()
	response, err := client.R().Get(fmt.Sprintf("http://nitro/nitro/model/%s", modelID))
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	if response.StatusCode() != http.StatusOK {
		resp.WriteEntity(api.ResponseWithMsg{
			Response: api.Response{Code: int32(response.StatusCode())},
			Message:  string(response.Body()),
		})
		return
	}
	var ret map[string]interface{}
	err = json.Unmarshal(response.Body(), &ret)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	resp.WriteAsJson(ret)
}

func (h *Handler) modelOperate(req *restful.Request, resp *restful.Response) {
	modelID := req.PathParameter(ParamModelID)

	var am v1alpha1.ApplicationManager
	key := types.NamespacedName{Name: utils.FmtModelMgrName(modelID)}
	err := h.ctrlClient.Get(req.Request.Context(), key, &am)
	if err != nil {
		if apierrors.IsNotFound(err) {
			api.HandleNotFound(resp, req, err)
			return
		}
		api.HandleError(resp, req, err)
		return
	}
	progress := am.Status.Progress
	if am.Status.Progress == "" {
		progress = "0"
	}
	operate := appinstaller.Operate{
		AppName:           am.Spec.AppName,
		AppOwner:          am.Spec.AppOwner,
		OpType:            am.Status.OpType,
		ResourceType:      v1alpha1.Model.String(),
		State:             toProcessing(am.Status.State),
		Message:           am.Status.Message,
		CreationTimestamp: am.CreationTimestamp,
		Source:            am.Spec.Source,
		Progress:          progress,
	}

	resp.WriteAsJson(operate)
}

func (h *Handler) modelsOperate(req *restful.Request, resp *restful.Response) {
	client := req.Attribute(constants.KubeSphereClientAttribute).(*clientset.ClientSet)
	owner := req.Attribute(constants.UserContextAttribute).(string)

	// run with request context for incoming client
	ams, err := client.AppClient.AppV1alpha1().ApplicationManagers().List(req.Request.Context(), metav1.ListOptions{})
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	// filter by application's owner
	filteredOperates := make([]appinstaller.Operate, 0)
	for _, am := range ams.Items {

		if am.Spec.AppOwner == owner && am.Spec.Type == v1alpha1.Model {
			progress := am.Status.Progress
			if am.Status.Progress == "" {
				progress = "0"
			}
			operate := appinstaller.Operate{
				AppName:           am.Spec.AppName,
				AppOwner:          am.Spec.AppOwner,
				State:             toProcessing(am.Status.State),
				OpType:            am.Status.OpType,
				ResourceType:      v1alpha1.Model.String(),
				Message:           am.Status.Message,
				CreationTimestamp: am.CreationTimestamp,
				Source:            am.Spec.Source,
				Progress:          progress,
			}
			filteredOperates = append(filteredOperates, operate)
		}
	}

	// sort by create time desc
	sort.Slice(filteredOperates, func(i, j int) bool {
		return filteredOperates[j].CreationTimestamp.Before(&filteredOperates[i].CreationTimestamp)
	})

	resp.WriteAsJson(map[string]interface{}{"result": filteredOperates})
}

func (h *Handler) modelOperateHistory(req *restful.Request, resp *restful.Response) {
	modelID := req.PathParameter(ParamModelID)

	var am v1alpha1.ApplicationManager
	key := types.NamespacedName{Name: utils.FmtModelMgrName(modelID)}
	err := h.ctrlClient.Get(req.Request.Context(), key, &am)

	if err != nil {
		if apierrors.IsNotFound(err) {
			api.HandleNotFound(resp, req, err)
			return
		}
		api.HandleError(resp, req, err)
		return
	}
	ops := make([]appinstaller.OperateHistory, 0, len(am.Status.OpRecords))
	for _, r := range am.Status.OpRecords {
		op := appinstaller.OperateHistory{
			AppName:      am.Spec.AppName,
			AppNamespace: am.Spec.AppNamespace,
			AppOwner:     am.Spec.AppOwner,
			ResourceType: am.Spec.Type.String(),

			OpRecord: v1alpha1.OpRecord{
				OpType:     r.OpType,
				Message:    r.Message,
				Source:     r.Source,
				Version:    r.Version,
				Status:     r.Status,
				StatusTime: r.StatusTime,
			},
		}
		ops = append(ops, op)
	}

	resp.WriteAsJson(map[string]interface{}{"result": ops})
}

func (h *Handler) allModelHistory(req *restful.Request, resp *restful.Response) {
	owner := req.Attribute(constants.UserContextAttribute).(string)

	var ams v1alpha1.ApplicationManagerList
	err := h.ctrlClient.List(req.Request.Context(), &ams)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	ops := make([]appinstaller.OperateHistory, 0)

	for _, am := range ams.Items {
		if am.Spec.AppOwner != owner || userspace.IsSysApp(am.Spec.AppName) || am.Spec.Type != v1alpha1.Model {
			continue
		}
		for _, r := range am.Status.OpRecords {
			op := appinstaller.OperateHistory{
				AppName:      am.Spec.AppName,
				AppNamespace: am.Spec.AppNamespace,
				AppOwner:     am.Spec.AppOwner,
				ResourceType: v1alpha1.Model.String(),

				OpRecord: v1alpha1.OpRecord{
					OpType:     r.OpType,
					Message:    r.Message,
					Source:     r.Source,
					Version:    r.Version,
					Status:     r.Status,
					StatusTime: r.StatusTime,
				},
			}
			ops = append(ops, op)
		}
	}
	sort.Slice(ops, func(i, j int) bool {
		return ops[j].StatusTime.Before(ops[i].StatusTime)
	})

	resp.WriteAsJson(map[string]interface{}{"result": ops})
}

func getProgress(modelID, owner string) (status string, progress float64, err error) {
	client := resty.New()
	response, err := client.R().Get(fmt.Sprintf("http://nitro/nitro/progress?id=%s", modelID))
	if err != nil {
		return status, progress, err
	}
	if response.StatusCode() != http.StatusOK {
		return status, progress, err
	}
	var ret map[string]interface{}
	err = json.Unmarshal(response.Body(), &ret)
	if err != nil {
		return status, progress, err
	}
	status = ret["status"].(string)
	progress = ret["progress"].(float64)
	return status, progress, nil
}

// GetModelConfig get model config by repoURL and modelID.
func GetModelConfig(ctx context.Context, modelID, repoURL, version, token string) (*appinstaller.ModelConfig, *appinstaller.AppConfiguration, error) {
	chartPath, err := GetIndexAndDownloadChart(ctx, modelID, repoURL, version, token)
	if err != nil {
		return nil, nil, err
	}

	f, err := os.Open(filepath.Join(chartPath, ModelCfgFileName))
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	data, err := ioutil.ReadAll(f)
	if err != nil {
		return nil, nil, err
	}
	var modelCfg appinstaller.ModelConfig
	if err = yaml.Unmarshal(data, &modelCfg); err != nil {
		return nil, nil, err
	}

	f, err = os.Open(filepath.Join(chartPath, AppCfgFileName))
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	data, err = ioutil.ReadAll(f)
	if err != nil {
		return nil, nil, err
	}

	var appCfg appinstaller.AppConfiguration
	if err := yaml.Unmarshal(data, &appCfg); err != nil {
		return nil, nil, err
	}

	return &modelCfg, &appCfg, nil
}
