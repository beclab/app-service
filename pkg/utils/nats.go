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

type Event struct {
	EventID          string                    `json:"eventID"`
	CreateTime       time.Time                 `json:"createTime"`
	Name             string                    `json:"name"`
	OpType           string                    `json:"opType,omitempty"`
	OpID             string                    `json:"opID,omitempty"`
	State            string                    `json:"state"`
	Progress         string                    `json:"progress,omitempty"`
	User             string                    `json:"user"`
	EntranceStatuses []v1alpha1.EntranceStatus `json:"entranceStatuses,omitempty"`
}

func PublishAsync(owner, name, opType, opID, state, progress string, entranceStatuses []v1alpha1.EntranceStatus) {
	subject := fmt.Sprintf("os.application.%s", owner)

	now := time.Now()
	data := Event{
		EventID:    fmt.Sprintf("%s-%s-%d", owner, name, now.UnixMilli()),
		CreateTime: now,
		Name:       name,
		OpType:     opType,
		OpID:       opID,
		State:      state,
		Progress:   progress,
		User:       owner,
	}
	if len(entranceStatuses) > 0 {
		data.EntranceStatuses = entranceStatuses
	}

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
