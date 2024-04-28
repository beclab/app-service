package apiserver

import (
	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"
	"errors"
	ctrl "sigs.k8s.io/controller-runtime"

	"bytetrade.io/web3os/app-service/pkg/apiserver/api"

	"github.com/emicklei/go-restful/v3"
	"go.uber.org/atomic"
)

var appinstallerLock atomic.Bool

func init() {
	appinstallerLock.Store(false)
}

func lockAppInstaller()   { appinstallerLock.Store(true) }
func unlockAppInstaller() { appinstallerLock.Store(false) }
func tryAppInstall(next func(req *restful.Request, resp *restful.Response)) func(req *restful.Request, resp *restful.Response) {
	return func(req *restful.Request, resp *restful.Response) {
		if !appinstallerLock.Load() {
			next(req, resp)
		} else {
			api.HandleForbidden(resp, req, errors.New("system is busy"))
		}
	}
}

func validateOpRole(next func(req *restful.Request, resp *restful.Response)) func(req *restful.Request, resp *restful.Response) {
	return func(req *restful.Request, resp *restful.Response) {
		user := req.Attribute(constants.UserContextAttribute)
		kubeConfig, _ := ctrl.GetConfig()
		role, _ := kubesphere.GetUserRole(req.Request.Context(), kubeConfig, user.(string))
		if role == "platform-admin" {
			next(req, resp)
		} else {
			api.HandleForbidden(resp, req, errors.New("you don't have permission to do this operate"))
		}
	}
}
