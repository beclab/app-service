package apiserver

import (
	"bytetrade.io/web3os/app-service/pkg/constants"

	"github.com/emicklei/go-restful/v3"
	"k8s.io/client-go/rest"
)

func newKubeConfigFromRequest(req *restful.Request, kubeHost string) *rest.Config {
	return &rest.Config{
		Host:        kubeHost,
		BearerToken: req.HeaderParameter(constants.AuthorizationTokenKey),
	}
}

func newWebService() *restful.WebService {
	webservice := restful.WebService{}

	webservice.Path("/app-service/v1").
		Produces(restful.MIME_JSON)

	return &webservice
}
