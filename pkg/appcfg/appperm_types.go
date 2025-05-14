package appcfg

type PermissionRegister struct {
	App   string              `json:"app"`
	AppID string              `json:"appid"`
	Perm  []SysDataPermission `json:"perm"`
}

type Header struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type RegisterResp struct {
	Header
	Data RegisterRespData `json:"data"`
}

type RegisterRespData struct {
	AppKey    string `json:"app_key"`
	AppSecret string `json:"app_secret"`
}

type UnregisterResp struct {
	Header
}
