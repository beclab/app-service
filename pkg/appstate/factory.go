package appstate

import (
	"context"
	"sync"
	"time"

	appsv1 "bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type statefulAppFactory struct {
	running map[string]StatefulInProgressApp
	mu      sync.Mutex
}

var once sync.Once
var appFactory statefulAppFactory

func init() {
	once.Do(func() {
		appFactory = statefulAppFactory{
			running: make(map[string]StatefulInProgressApp),
		}
	})
}

func (f *statefulAppFactory) New(
	client client.Client,
	manager *appsv1.ApplicationManager,
	ttl time.Duration,
	create func(client client.Client, manager *appsv1.ApplicationManager, ttl time.Duration) StatefulApp,
) (StatefulApp, StateError) {
	f.mu.Lock()
	defer f.mu.Unlock()

	runningApp, ok := f.running[manager.Name]
	if ok {
		if runningApp.State() != manager.Status.State.String() {
			klog.Infof("app %s is doing something in progress, but state is not match, expected: %s, actual: %s",
				manager.Name, manager.Status.State.String(), runningApp.State())

			return nil, NewErrorUnknownInProgressApp(func(ctx context.Context) error {
				runningApp.Cleanup(ctx)

				// remove the app from the running map
				f.mu.Lock()
				delete(f.running, runningApp.GetManager().Name)
				f.mu.Unlock()

				return nil
			})
		}

		klog.Infof("app %s is already doing operation, state: %s", manager.Name, runningApp.State())
		return runningApp, nil
	}

	return create(client, manager, ttl), nil
}

func (f *statefulAppFactory) waitForPolling(ctx context.Context, app PollableStatefulInProgressApp, success func()) {
	f.mu.Lock()
	defer f.mu.Unlock()

	_, ok := f.running[app.GetManager().Name]
	if !ok {

		go func() {
			err := app.poll(ctx)
			if err != nil {
				klog.Error("poll ", app.State(), " progress error, ", err, ", ", app.GetManager().Name)
			}

			app.stopPolling()

			klog.Error("stop polling, ", "app name: ", app.GetManager().Name)

			// remove the app from the running map
			f.mu.Lock()
			delete(f.running, app.GetManager().Name)
			f.mu.Unlock()

			if err == nil {
				success()
			}
		}()

		f.running[app.GetManager().Name] = app
	}
}

func (f *statefulAppFactory) execAndWatch(
	ctx context.Context,
	app StatefulApp,
	exec func(ctx context.Context) (StatefulInProgressApp, error),
	success func(),
) (StatefulInProgressApp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Check if the app is already running
	if existingApp, ok := f.running[app.GetManager().Name]; ok {
		if existingApp.State() == app.GetManager().Status.State.String() {
			klog.Infof("app %s is already doing operation, state: %s", app.GetManager().Name, existingApp.State())
			return existingApp, nil
		}
	}

	// Execute the app and wait for it to complete
	newApp, err := exec(ctx)
	if err != nil {
		return nil, err
	}

	f.running[newApp.GetManager().Name] = newApp

	go func() {
		if done := newApp.Done(); done != nil {
			<-done
			klog.Infof("app %s has completed", newApp.GetManager().Name)
		}

		if success != nil {
			success()
		}

		f.mu.Lock()
		delete(f.running, newApp.GetManager().Name)
		f.mu.Unlock()
	}()

	return newApp, nil
}

func (f *statefulAppFactory) cancelOperation(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	if app, ok := f.running[name]; ok {
		//
		app.Cleanup(context.Background())
		delete(f.running, name)
		return true
	}

	return false
}
