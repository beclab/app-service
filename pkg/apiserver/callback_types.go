package apiserver

import (
	"bytetrade.io/web3os/app-service/pkg/apiserver/api"
	"errors"
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
	// 入队？
	return func(req *restful.Request, resp *restful.Response) {
		// 入队
		// if len(queue) > 0 {
		//		next(req, resp)
		// }

		if !appinstallerLock.Load() {
			next(req, resp)
		} else {
			api.HandleForbidden(resp, req, errors.New("system is busy"))
		}
	}
}

//func queued(next func(req *restful.Request, resp *restful.Response)) func(req *restful.Request, resp *restful.Response) {
//	return func(req *restful.Request, resp *restful.Response) {
//		// 入队
//		// if len(queue) > 0 {
//		//		next(req, resp)
//		// }
//		WQ.Add(func() { next(req, resp) })
//	}
//}
