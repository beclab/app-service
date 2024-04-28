package workflowinstaller

import "bytetrade.io/web3os/app-service/pkg/appinstaller"

// WorkflowConfig contains details of a workflow.
type WorkflowConfig struct {
	Namespace    string
	ChartsName   string
	RepoURL      string
	Title        string
	Version      string
	WorkflowName string // name of application displayed on shortcut
	OwnerName    string // name of owner who installed application
	Cfg          *appinstaller.AppConfiguration
}
