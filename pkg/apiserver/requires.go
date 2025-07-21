package apiserver

import (
	"errors"

	"bytetrade.io/web3os/app-service/pkg/constants"
	"bytetrade.io/web3os/app-service/pkg/kubesphere"

	"github.com/emicklei/go-restful/v3"
)

func requireAdmin(h *Handler, next func(req *restful.Request, resp *restful.Response)) func(req *restful.Request, resp *restful.Response) {
	return func(req *restful.Request, resp *restful.Response) {
		user := req.Attribute(constants.UserContextAttribute)

		role, err := kubesphere.GetUserRole(req.Request.Context(), user.(string))
		if err != nil {
			responseError(resp, err)
			return
		}

		if role != "owner" && role != "admin" {
			responseError(resp, errors.New("only admin user can upgrade system"))
			return
		}

		next(req, resp)
	}
}
