package controllers

import (
	"context"
	"time"

	appv1alpha1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"bytetrade.io/web3os/app-service/pkg/appstate"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
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

	switch {
	case am.Status.State == appv1alpha1.Pending:
		return appstate.NewPendingApp(ctx, appmgr, &am, time.Hour)
	}

	return nil, appstate.NewErrorUnknownState(nil, nil)
}
