package controllers

import (
	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appstate"
	"context"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

func LoadStatefulApp(ctx context.Context, appmgr *ApplicationManagerController, name string) (appstate.StatefulApp, appstate.StateError) {
	var am appv1alpha1.ApplicationManager
	err := appmgr.Get(ctx, types.NamespacedName{Name: name}, &am)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, appstate.NewStateError(err.Error())
	}
	klog.Infof("LoadStatefulApp: state: %v", am.Status.State)
	switch am.Status.State {
	case appv1alpha1.Pending:
		return appstate.NewPendingApp(ctx, appmgr, &am)
	case appv1alpha1.Downloading:
		return appstate.NewDownloadingApp(ctx, appmgr, &am)
	case appv1alpha1.Installing:
		return appstate.NewInstallingApp(ctx, appmgr, &am)
	case appv1alpha1.Initializing:
		return appstate.NewInitializingApp(ctx, appmgr, &am)
	case appv1alpha1.Running:
		klog.Infof("LoadStatefulApp: NewRunningApp")
		return appstate.NewRunningApp(ctx, appmgr, &am)
	case appv1alpha1.Suspending:
		return appstate.NewSuspendingApp(ctx, appmgr, &am)
	case appv1alpha1.Upgrading:
		return appstate.NewUpgradingApp(ctx, appmgr, &am)
	case appv1alpha1.Resuming:
		return appstate.NewResumingApp(ctx, appmgr, &am)
	case appv1alpha1.PendingCanceling:
		return appstate.NewPendingCancelingApp(ctx, appmgr, &am)
	case appv1alpha1.DownloadingCanceling:
		return appstate.NewDownloadingCancelingApp(ctx, appmgr, &am)
	case appv1alpha1.InstallingCanceling:
		return appstate.NewInstallingCancelingApp(ctx, appmgr, &am)
	case appv1alpha1.InitializingCanceling:
		return appstate.NewInitializingCancelingApp(ctx, appmgr, &am)
	case appv1alpha1.ResumingCanceling:
		return appstate.NewResumingCancelingApp(ctx, appmgr, &am)
	case appv1alpha1.UpgradingCanceling:
		return appstate.NewUpgradingCancelingApp(ctx, appmgr, &am)
	case appv1alpha1.Uninstalling:
		return appstate.NewUninstallingApp(ctx, appmgr, &am)
	case appv1alpha1.DownloadFailed:
		return appstate.NewDownloadFailedApp(ctx, appmgr, &am)
	case appv1alpha1.InstallFailed:
		return appstate.NewInstallFailedApp(ctx, appmgr, &am)
	case appv1alpha1.SuspendFailed:
		return appstate.NewSuspendFailedApp(ctx, appmgr, &am)
	case appv1alpha1.UninstallFailed:
		return appstate.NewUninstallFailedApp(ctx, appmgr, &am)
	case appv1alpha1.UpgradeFailed:
		return appstate.NewUpgradeFailedApp(ctx, appmgr, &am)
	case appv1alpha1.ResumeFailed:
		return appstate.NewResumeFailedApp(ctx, appmgr, &am)
	case appv1alpha1.CancelFailed:
		return appstate.NewCancelFailedApp(ctx, appmgr, &am)
	case appv1alpha1.PendingCanceled, appv1alpha1.DownloadingCanceled,
		appv1alpha1.InstallingCanceled, appv1alpha1.InitializingCanceled,
		appv1alpha1.SuspendingCanceled, appv1alpha1.UpgradingCanceled, appv1alpha1.ResumingCanceled:
		return appstate.NewCanceledApp(ctx, appmgr, &am)

	case appv1alpha1.Uninstalled:
		return appstate.NewUninstalledApp(ctx, appmgr, &am)
	case appv1alpha1.Suspended:
		return appstate.NewSuspendedApp(ctx, appmgr, &am)

	}
	klog.Infof("LoadStatefulApp: Not in Switch, state: %v", am.Status.State)

	return nil, appstate.NewErrorUnknownState(nil, nil)
}
