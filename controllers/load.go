package controllers

import (
	"context"
	"time"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appstate"
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

	klog.Infof("LoadStatefulApp name:%s, state: %v", am.Name, am.Status.State)

	retApp, serr := func() (appstate.StatefulApp, appstate.StateError) {
		switch am.Status.State {
		case appv1alpha1.Pending:
			return appstate.NewPendingApp(ctx, appmgr, &am, time.Hour)
		case appv1alpha1.Downloading:
			return appstate.NewDownloadingApp(appmgr, &am, 2*time.Hour)
		case appv1alpha1.Installing:
			return appstate.NewInstallingApp(appmgr, &am, 30*time.Minute)
		case appv1alpha1.Initializing:
			return appstate.NewInitializingApp(appmgr, &am, 30*time.Minute)
			// case appv1alpha1.Running:
			// 	return appstate.NewRunningApp(appmgr, &am), nil
			// case appv1alpha1.Stopping:
			// 	return appstate.NewSuspendingApp(appmgr, &am), nil
			// case appv1alpha1.Upgrading:
			// 	return appstate.NewUpgradingApp(appmgr, &am), nil
			// case appv1alpha1.Resuming:
			// 	return appstate.NewResumingApp(appmgr, &am), nil
			// case appv1alpha1.PendingCanceling:
			// 	return appstate.NewPendingCancelingApp(appmgr, &am), nil
			// case appv1alpha1.DownloadingCanceling:
			// 	return appstate.NewDownloadingCancelingApp(appmgr, &am), nil
			// case appv1alpha1.InstallingCanceling:
			// 	return appstate.NewInstallingCancelingApp(appmgr, &am), nil
			// case appv1alpha1.InitializingCanceling:
			// 	return appstate.NewInitializingCancelingApp(appmgr, &am), nil
			// case appv1alpha1.ResumingCanceling:
			// 	return appstate.NewResumingCancelingApp(appmgr, &am), nil
			// case appv1alpha1.UpgradingCanceling:
			// 	return appstate.NewUpgradingCancelingApp(appmgr, &am), nil
			// case appv1alpha1.Uninstalling:
			// 	return appstate.NewUninstallingApp(appmgr, &am), nil
			// case appv1alpha1.DownloadFailed:
			// 	return appstate.NewDownloadFailedApp(appmgr, &am), nil
			// case appv1alpha1.InstallFailed:
			// 	return appstate.NewInstallFailedApp(appmgr, &am), nil
			// case appv1alpha1.StopFailed:
			// 	return appstate.NewSuspendFailedApp(appmgr, &am), nil
			// case appv1alpha1.UninstallFailed:
			// 	return appstate.NewUninstallFailedApp(appmgr, &am), nil
			// case appv1alpha1.UpgradeFailed:
			// 	return appstate.NewUpgradeFailedApp(appmgr, &am), nil
			// case appv1alpha1.ResumeFailed:
			// 	return appstate.NewResumeFailedApp(appmgr, &am), nil

			// case appv1alpha1.PendingCanceled, appv1alpha1.DownloadingCanceled,
			// 	appv1alpha1.InstallingCanceled, appv1alpha1.InitializingCanceled,
			// 	appv1alpha1.UpgradingCanceled, appv1alpha1.ResumingCanceled:
			// 	return appstate.NewCanceledApp(appmgr, &am), nil
			// case appv1alpha1.PendingCancelFailed:
			// 	return appstate.NewPendingCancelFailedApp(appmgr, &am), nil
			// case appv1alpha1.DownloadingCancelFailed:
			// 	return appstate.NewDownloadingCancelFailedApp(appmgr, &am), nil

			// case appv1alpha1.InstallingCancelFailed:
			// 	return appstate.NewInstallingCancelFailedApp(appmgr, &am), nil
			// //case appv1alpha1.InitializingCancelFailed:
			// //	return appstate.NewInitializingCancelFailedApp(appmgr, &am), nil
			// case appv1alpha1.UpgradingCancelFailed:
			// 	return appstate.NewUpgradingCancelFailedApp(appmgr, &am), nil
			// //case appv1alpha1.ResumingCancelFailed:
			// //	return appstate.NewResumingCancelFailedApp(appmgr, &am), nil
			// case appv1alpha1.Uninstalled:
			// 	return appstate.NewUninstalledApp(appmgr, &am), nil
			// case appv1alpha1.Stopped:
			// 	return appstate.NewSuspendedApp(appmgr, &am), nil
		}
		return nil, appstate.NewErrorUnknownState(nil, nil)
	}()

	if serr != nil {
		klog.Info("LoadStatefulApp: unknown state: ", am.Status.State, ", ", err)
		return nil, serr
	}

	return retApp, nil
}
