package apiserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"bytetrade.io/web3os/app-service/pkg/client/clientset"
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/utils"
	apputils "bytetrade.io/web3os/app-service/pkg/utils/app"
	"bytetrade.io/web3os/app-service/pkg/workflowinstaller"

	"github.com/emicklei/go-restful/v3"
	"github.com/go-resty/resty/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

type KnowledgeInstallMsg struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

func (h *Handler) notifyKnowledgeInstall(title, name, owner string) error {
	knowledgeAPI := "http://rss-svc.os-framework:3010/knowledge/algorithm/recommend/install"
	klog.Info("Start to notify knowledge to Install ", knowledgeAPI, title, name)

	msg := KnowledgeInstallMsg{
		ID:    name,
		Title: title,
	}
	body, jsonErr := json.Marshal(msg)
	if jsonErr != nil {
		return jsonErr
	}
	client := resty.New()
	resp, err := client.SetTimeout(10*time.Second).R().
		SetHeader(restful.HEADER_ContentType, restful.MIME_JSON).
		SetHeader("X-Bfl-User", owner).
		SetBody(body).Post(knowledgeAPI)
	if err != nil {
		return err
	}
	if resp.StatusCode() != http.StatusOK {
		klog.Errorf("Failed to notify knowledge to Install status=%s", resp.Status())
		return errors.New(resp.Status())
	}
	return nil
}

func (h *Handler) notifyKnowledgeUnInstall(name, owner string) error {
	knowledgeAPI := "http://rss-svc.os-framework:3010/knowledge/algorithm/recommend/uninstall"

	msg := KnowledgeInstallMsg{
		ID: name,
	}
	body, jsonErr := json.Marshal(msg)
	if jsonErr != nil {
		return jsonErr
	}
	klog.Info("Start to notify knowledge to unInstall ", knowledgeAPI)
	client := resty.New()
	resp, err := client.SetTimeout(10*time.Second).R().
		SetHeader(restful.HEADER_ContentType, restful.MIME_JSON).
		SetHeader("X-Bfl-User", owner).
		SetBody(body).Post(knowledgeAPI)

	if err != nil {
		return err
	}
	if resp.StatusCode() != http.StatusOK {
		klog.Errorf("Failed to notify knowledge to Install status=%s", resp.Status())
		return errors.New(resp.Status())
	}
	return nil
}
func (h *Handler) cleanRecommendEntryData(name, owner string) error {
	knowledgeAPI := fmt.Sprintf("http://knowledge-base-api.user-system-%s:3010", owner)
	entryAPI := knowledgeAPI + "/knowledge/entry/algorithm/" + name
	klog.Info("Start to clean recommend entry data ", entryAPI)
	client := resty.New().SetTimeout(10*time.Second).
		SetHeader("X-Bfl-User", owner)
	entryResp, err := client.R().Get(entryAPI)
	if err != nil {
		return err
	}
	if entryResp.StatusCode() != http.StatusOK {
		klog.Errorf("Failed to get knowledge entry list status=%s", entryResp.Status())
		return errors.New(entryResp.Status())
	}
	var ret workflowinstaller.KnowledgeAPIResp
	err = json.Unmarshal(entryResp.Body(), &ret)
	if err != nil {
		return err
	}
	urlsCount := len(ret.Data)
	if urlsCount > 0 {
		limit := 100
		removeClient := resty.New()
		entryRemoveAPI := knowledgeAPI + "/knowledge/entry/" + name
		for i := 0; i*limit < urlsCount; i++ {
			start := i * limit
			end := start + limit
			if end > urlsCount {
				end = urlsCount
			}
			removeList := ret.Data[start:end]
			removeBody, _ := json.Marshal(removeList)
			res, _ := removeClient.SetTimeout(5*time.Second).R().SetHeader(restful.HEADER_ContentType, restful.MIME_JSON).
				SetBody(removeBody).Delete(entryRemoveAPI)

			if res.StatusCode() == http.StatusOK {
				klog.Info("Delete entry success page: ", i, len(removeList))
			} else {
				klog.Info("Clean recommend entry data error:", string(removeBody), string(res.Body()))
			}
		}

	}
	klog.Info("Delete entry success page: ", name, urlsCount)
	return nil
}

func (h *Handler) uninstallRecommend(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamWorkflowName)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	client := req.Attribute(constants.KubeSphereClientAttribute).(*clientset.ClientSet)
	var err error

	//namespace := fmt.Sprintf("%s-%s", app, owner)
	namespace, err := utils.AppNamespace(app, owner, "")
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	workflowCfg := &workflowinstaller.WorkflowConfig{
		WorkflowName: app,
		Namespace:    namespace,
		OwnerName:    owner,
	}
	klog.Infof("Start to uninstall workflow name=%s", workflowCfg.WorkflowName)

	go h.cleanRecommendEntryData(app, owner)
	go h.notifyKnowledgeUnInstall(app, owner)

	now := metav1.Now()
	var recommendMgr *v1alpha1.ApplicationManager
	recommendStatus := v1alpha1.ApplicationManagerStatus{
		OpType:     v1alpha1.UninstallOp,
		State:      v1alpha1.Uninstalling,
		Message:    "try to uninstall a recommend",
		StatusTime: &now,
		UpdateTime: &now,
	}
	name, _ := apputils.FmtAppMgrName(app, owner, namespace)
	recommendMgr, err = apputils.UpdateAppMgrStatus(name, recommendStatus)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	defer func() {
		if err != nil {
			now := metav1.Now()
			message := fmt.Sprintf(constants.OperationFailedTpl, recommendMgr.Status.OpType, err.Error())
			opRecord := v1alpha1.OpRecord{
				OpType:    v1alpha1.UninstallOp,
				Message:   message,
				Source:    recommendMgr.Spec.Source,
				Version:   recommendMgr.Status.Payload["version"],
				Status:    v1alpha1.Failed,
				StateTime: &now,
			}
			e := apputils.UpdateStatus(recommendMgr, "failed", &opRecord, message)
			if e != nil {
				klog.Errorf("Failed to update applicationmanager status in uninstall Recommend name=%s err=%v", recommendMgr.Name, e)
			}
		}
	}()

	err = workflowinstaller.Uninstall(req.Request.Context(), h.kubeConfig, workflowCfg)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	klog.Infof("Start to delete namespace=%s", namespace)
	err = client.KubeClient.Kubernetes().CoreV1().Namespaces().Delete(req.Request.Context(), namespace, metav1.DeleteOptions{})
	if err != nil {
		klog.Errorf("Failed to delete workflow namespace=%s err=%v", namespace, err)
		api.HandleError(resp, req, err)
		return
	}
	go func() {
		timer := time.NewTicker(2 * time.Second)
		for {
			select {
			case <-timer.C:
				_, err := client.KubeClient.Kubernetes().CoreV1().Namespaces().
					Get(context.TODO(), namespace, metav1.GetOptions{})
				if err != nil {
					if apierrors.IsNotFound(err) {
						now := metav1.Now()
						message := fmt.Sprintf(constants.UninstallOperationCompletedTpl, recommendMgr.Spec.Type.String(), recommendMgr.Spec.AppName)
						opRecord := v1alpha1.OpRecord{
							OpType:    v1alpha1.UninstallOp,
							Message:   message,
							Source:    recommendMgr.Spec.Source,
							Version:   recommendMgr.Status.Payload["version"],
							Status:    v1alpha1.Running,
							StateTime: &now,
						}
						err = apputils.UpdateStatus(recommendMgr, opRecord.Status, &opRecord, message)
						if err != nil {
							klog.Errorf("Failed to update applicationmanager name=%s in uninstall Recommend err=%v", recommendMgr.Name, err)
						}
						return
					}

				}
			}
		}
	}()

	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: app},
	})
}

func (h *Handler) upgradeRecommend(req *restful.Request, resp *restful.Response) {
	app := req.PathParameter(ParamWorkflowName)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	token := req.HeaderParameter(constants.AuthorizationTokenKey)
	marketSource := req.HeaderParameter(constants.MarketSource)
	var err error
	upReq := &api.UpgradeRequest{}
	err = req.ReadEntity(upReq)
	if err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}

	var recommendMgr *v1alpha1.ApplicationManager
	var workflowCfg *workflowinstaller.WorkflowConfig

	defer func() {
		now := metav1.Now()
		opRecord := v1alpha1.OpRecord{
			OpType:    v1alpha1.UpgradeOp,
			Message:   fmt.Sprintf(constants.UpgradeOperationCompletedTpl, recommendMgr.Spec.Type.String(), recommendMgr.Spec.AppName),
			Source:    recommendMgr.Spec.Source,
			Version:   workflowCfg.Cfg.Metadata.Version,
			Status:    v1alpha1.Running,
			StateTime: &now,
		}
		if err != nil {
			opRecord.Status = v1alpha1.Failed
			opRecord.Message = fmt.Sprintf(constants.OperationFailedTpl, recommendMgr.Status.OpType, err.Error())
		}
		e := apputils.UpdateStatus(recommendMgr, opRecord.Status, &opRecord, opRecord.Message)
		if e != nil {
			klog.Errorf("Failed to update applicationmanager status in upgrade recommend name=%s err=%v", recommendMgr.Name, e)
		}

	}()

	now := metav1.Now()
	recommendStatus := v1alpha1.ApplicationManagerStatus{
		OpType:     v1alpha1.UpgradeOp,
		State:      v1alpha1.Upgrading,
		Message:    "try to upgrade a recommend",
		StatusTime: &now,
		UpdateTime: &now,
	}

	klog.Infof("Download latest version chart and get workflow config name=%s repoURL=%s", app, upReq.RepoURL)
	workflowCfg, err = getWorkflowConfigFromRepo(req.Request.Context(), owner, app, upReq.RepoURL, "", token, marketSource)
	if err != nil {
		klog.Errorf("Failed to get workflow config name=%s repoURL=%s err=%v, ", app, upReq.RepoURL, err)
		api.HandleError(resp, req, err)
		return
	}
	name, _ := apputils.FmtAppMgrName(app, owner, workflowCfg.Namespace)
	recommendMgr, err = apputils.UpdateAppMgrStatus(name, recommendStatus)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	klog.Infof("Start to upgrade workflow name=%s", workflowCfg.WorkflowName)
	err = workflowinstaller.Upgrade(req.Request.Context(), h.kubeConfig, workflowCfg)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: app},
	})

}
