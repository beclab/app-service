package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"bytetrade.io/web3os/app-service/api/app.bytetrade.io/v1alpha1"

	"github.com/nats-io/nats.go"
	"k8s.io/klog/v2"
)

func PublishAsync(subject string, name string, state v1alpha1.ApplicationManagerState, status v1alpha1.ApplicationManagerStatus) {
	now := time.Now()
	data := map[string]interface{}{
		"name":       name,
		"state":      state,
		"opID":       status.OpID,
		"opType":     status.OpType,
		"createTime": now,
	}
	go func() {
		if err := publish(subject, data); err != nil {
			klog.Errorf("async publish subject %s,data %v, failed %v", subject, data, err)
		}
	}()
}

func publishAsync(subject string, data interface{}) {
	go func() {
		if err := publish(subject, data); err != nil {
			klog.Errorf("async publish subject %s,data %v, failed %v", subject, data, err)
		}
	}()
}

func publish(subject string, data interface{}) error {
	klog.Infof("receive request")
	natsHost := os.Getenv("NATS_HOST")
	natsPort := os.Getenv("NATS_PORT")

	//subject = os.Getenv("NATS_SUBJECT")
	username := os.Getenv("NATS_USERNAME")
	password := os.Getenv("NATS_PASSWORD")

	natsURL := fmt.Sprintf("nats://%s:%s", natsHost, natsPort)
	nc, err := nats.Connect(natsURL, nats.UserInfo(username, password))
	if err != nil {
		klog.Infof("connect error: err=%v", err)
		return err
	}
	defer nc.Drain()
	d, err := json.Marshal(data)
	if err != nil {
		klog.Errorf("marshal failed: %v", err)
		return err
	}
	err = nc.Publish(subject, d)
	if err != nil {
		klog.Infof("publish err=%v", err)
		return err
	}
	return nil
}
