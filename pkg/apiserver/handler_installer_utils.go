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
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/utils"
	apputils "bytetrade.io/web3os/app-service/pkg/utils/app"
	"bytetrade.io/web3os/app-service/pkg/workflowinstaller"
	"github.com/emicklei/go-restful/v3"
	"github.com/go-resty/resty/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UpdateAppState update applicationmanager state, message
func (h *Handler) UpdateAppState(ctx context.Context, name string, state v1alpha1.ApplicationManagerState, message string) error {
	var appMgr v1alpha1.ApplicationManager
	key := types.NamespacedName{Name: name}
	err := h.ctrlClient.Get(ctx, key, &appMgr)
	if err != nil {
		return err
	}
	appMgrCopy := appMgr.DeepCopy()
	now := metav1.Now()
	appMgr.Status.State = state
	appMgr.Status.Message = message
	appMgr.Status.StatusTime = &now
	appMgr.Status.UpdateTime = &now
	err = h.ctrlClient.Status().Patch(ctx, &appMgr, client.MergeFrom(appMgrCopy))
	return err
}

func (h *Handler) checkDependencies(req *restful.Request, resp *restful.Response) {
	owner := req.Attribute(constants.UserContextAttribute) // get owner from request token
	var err error
	depReq := depRequest{}
	err = req.ReadEntity(&depReq)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	unSatisfiedDeps, _ := CheckDependencies(req.Request.Context(), depReq.Data, h.ctrlClient, owner.(string), true)
	klog.Infof("Check application dependencies unSatisfiedDeps=%v", unSatisfiedDeps)

	data := make([]api.DependenciesRespData, 0)
	for _, dep := range unSatisfiedDeps {
		data = append(data, api.DependenciesRespData{
			Name:    dep.Name,
			Version: dep.Version,
			Type:    dep.Type,
		})
	}
	resp.WriteEntity(api.DependenciesResp{
		Response: api.Response{Code: 200},
		Data:     data,
	})
}

func (h *Handler) installRecommend(req *restful.Request, resp *restful.Response) {
	insReq := &api.InstallRequest{}
	err := req.ReadEntity(insReq)
	if err != nil {
		api.HandleBadRequest(resp, req, err)
		return
	}

	app := req.PathParameter(ParamWorkflowName)
	token := req.HeaderParameter(constants.AuthorizationTokenKey)
	owner := req.Attribute(constants.UserContextAttribute).(string)
	marketSource := req.HeaderParameter(constants.MarketSource)

	klog.Infof("Download chart and get workflow config appName=%s repoURL=%s", app, insReq.RepoURL)
	workflowCfg, err := getWorkflowConfigFromRepo(req.Request.Context(), owner, app, insReq.RepoURL, "", token, marketSource)
	if err != nil {
		klog.Errorf("Failed to get workflow config appName=%s repoURL=%s err=%v", app, insReq.RepoURL, err)
		api.HandleError(resp, req, err)
		return
	}

	satisfied, err := CheckMiddlewareRequirement(req.Request.Context(), h.kubeConfig, workflowCfg.Cfg.Middleware)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}
	if !satisfied {
		resp.WriteHeaderAndEntity(http.StatusBadRequest, api.RequirementResp{
			Response: api.Response{Code: 400},
			Resource: "middleware",
			Message:  fmt.Sprintf("middleware requirement can not be satisfied"),
		})
		return
	}

	go h.notifyKnowledgeInstall(workflowCfg.Cfg.Metadata.Title, app, owner)

	client, _ := utils.GetClient()

	var a *v1alpha1.ApplicationManager
	//appNamespace, _ := utils.AppNamespace(app, owner, workflowCfg.Namespace)
	name, _ := apputils.FmtAppMgrName(app, owner, workflowCfg.Namespace)
	recommendMgr := &v1alpha1.ApplicationManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-%s", workflowCfg.Namespace, app),
		},
		Spec: v1alpha1.ApplicationManagerSpec{
			AppName:      app,
			AppNamespace: workflowCfg.Namespace,
			AppOwner:     owner,
			Source:       insReq.Source.String(),
			Type:         v1alpha1.Recommend,
		},
	}
	a, err = client.AppV1alpha1().ApplicationManagers().Get(req.Request.Context(), name, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			api.HandleError(resp, req, err)
			return
		}
		a, err = client.AppV1alpha1().ApplicationManagers().Create(req.Request.Context(), recommendMgr, metav1.CreateOptions{})
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
		_, err = client.AppV1alpha1().ApplicationManagers().Patch(req.Request.Context(),
			a.Name, types.MergePatchType, patchByte, metav1.PatchOptions{})
		if err != nil {
			api.HandleError(resp, req, err)
			return
		}
	}
	now := metav1.Now()
	recommendStatus := v1alpha1.ApplicationManagerStatus{
		OpType:  v1alpha1.InstallOp,
		State:   v1alpha1.Installing,
		Message: "installing recommend",
		Payload: map[string]string{
			"version": workflowCfg.Cfg.Metadata.Version,
		},
		StatusTime: &now,
		UpdateTime: &now,
	}
	a, err = apputils.UpdateAppMgrStatus(a.Name, recommendStatus)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	opRecord := v1alpha1.OpRecord{
		OpType:    v1alpha1.InstallOp,
		Version:   workflowCfg.Cfg.Metadata.Version,
		Source:    a.Spec.Source,
		Status:    v1alpha1.Running,
		StateTime: &now,
	}

	klog.Info("Start to install workflow, ", workflowCfg)
	err = workflowinstaller.Install(req.Request.Context(), h.kubeConfig, workflowCfg)
	if err != nil {
		opRecord.Status = v1alpha1.Failed
		opRecord.Message = fmt.Sprintf(constants.OperationFailedTpl, a.Status.OpType, err.Error())
		e := apputils.UpdateStatus(a, opRecord.Status, &opRecord, opRecord.Message)
		if e != nil {
			klog.Errorf("Failed to update applicationmanager status name=%s err=%v", a.Name, e)
		}
		api.HandleError(resp, req, err)
		return
	}

	now = metav1.Now()
	opRecord.Message = fmt.Sprintf(constants.InstallOperationCompletedTpl, a.Spec.Type.String(), a.Spec.AppName)
	err = apputils.UpdateStatus(a, v1alpha1.Running, &opRecord, opRecord.Message)
	if err != nil {
		api.HandleError(resp, req, err)
		return
	}

	resp.WriteEntity(api.InstallationResponse{
		Response: api.Response{Code: 200},
		Data:     api.InstallationResponseData{UID: app},
	})
}

func (h *Handler) cleanRecommendFeedData(name, owner string) error {
	knowledgeAPI := fmt.Sprintf("http://knowledge-base-api.user-system-%s:3010", owner)
	feedAPI := knowledgeAPI + "/knowledge/feed/algorithm/" + name

	client := resty.New()
	response, err := client.R().Get(feedAPI)
	if err != nil {
		return err
	}
	if response.StatusCode() != http.StatusOK {
		klog.Errorf("Failed to get knowledge feed list status=%s body=%s", response.Status(), response.String())
		return errors.New(response.Status())
	}
	var ret workflowinstaller.KnowledgeAPIResp
	err = json.Unmarshal(response.Body(), &ret)
	if err != nil {
		return err
	}
	feedUrls := ret.Data
	klog.Info("Start to clean recommend feed data ", feedAPI, len(feedUrls))
	if len(feedUrls) > 0 {
		limit := 10
		removeClient := resty.New()
		for i := 0; i*limit < len(feedUrls); i++ {
			start := i * limit
			end := start + limit
			if end > len(feedUrls) {
				end = len(feedUrls)
			}
			removeList := feedUrls[start:end]
			reqData := workflowinstaller.KnowledgeFeedDelReq{FeedUrls: removeList}
			removeBody, _ := json.Marshal(reqData)
			res, _ := removeClient.SetTimeout(5*time.Second).R().SetHeader(restful.HEADER_ContentType, restful.MIME_JSON).
				SetBody(removeBody).Delete(feedAPI)

			if res.StatusCode() == http.StatusOK {
				klog.Info("Delete feed success: ", i, len(removeList))
			} else {
				klog.Errorf("Failed to clean recommend feed data err=%s", string(res.Body()))
			}
		}
	}
	klog.Info("Delete entry success page: ", name, len(feedUrls))
	return nil
}
