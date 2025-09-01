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
	Type             string                    `json:"type"`
	OpType           string                    `json:"opType,omitempty"`
	OpID             string                    `json:"opID,omitempty"`
	State            string                    `json:"state"`
	Progress         string                    `json:"progress,omitempty"`
	User             string                    `json:"user"`
	EntranceStatuses []v1alpha1.EntranceStatus `json:"entranceStatuses,omitempty"`
}

type UserEvent struct {
	Topic   string  `json:"topic"`
	Payload Payload `json:"payload"`
}

type Payload struct {
	User      string    `json:"user"`
	Operator  string    `json:"operator"`
	Timestamp time.Time `json:"timestamp"`
}

func PublishUserEvent(topic, user, operator string) {
	subject := "os.users"
	data := UserEvent{
		Topic: topic,
		Payload: Payload{
			User:      user,
			Operator:  operator,
			Timestamp: time.Now(),
		},
	}
	if err := publish(subject, data); err != nil {
		klog.Errorf("async publish subject %s,data %v, failed %v", subject, data, err)
	} else {
		t, _ := json.Marshal(data)
		klog.Infof("publish user event success. data: %v", string(t))
	}
}

func PublishAppEvent(owner, name, opType, opID, state, progress string, entranceStatuses []v1alpha1.EntranceStatus) {
	subject := fmt.Sprintf("os.application.%s", owner)

	now := time.Now()
	data := Event{
		EventID:    fmt.Sprintf("%s-%s-%d", owner, name, now.UnixMilli()),
		CreateTime: now,
		Name:       name,
		Type:       "app",
		OpType:     opType,
		OpID:       opID,
		State:      state,
		Progress:   progress,
		User:       owner,
	}
	if len(entranceStatuses) > 0 {
		data.EntranceStatuses = entranceStatuses
	}

	if err := publish(subject, data); err != nil {
		klog.Errorf("async publish subject %s,data %v, failed %v", subject, data, err)
	} else {
		klog.Infof("publish event success data: %#v", data)
	}
}

func PublishToNats(subject string, data interface{}) error {
	return publish(subject, data)
}

func publish(subject string, data interface{}) error {
	natsHost := os.Getenv("NATS_HOST")
	natsPort := os.Getenv("NATS_PORT")

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

func PublishMiddlewareEvent(owner, name, opType, opID, state, progress string, entranceStatuses []v1alpha1.EntranceStatus) {
	subject := fmt.Sprintf("os.application.%s", owner)

	now := time.Now()
	data := Event{
		EventID:    fmt.Sprintf("%s-%s-%d", owner, name, now.UnixMilli()),
		CreateTime: now,
		Name:       name,
		Type:       "middleware",
		OpType:     opType,
		OpID:       opID,
		State:      state,
		Progress:   progress,
		User:       owner,
	}
	if len(entranceStatuses) > 0 {
		data.EntranceStatuses = entranceStatuses
	}

	if err := publish(subject, data); err != nil {
		klog.Errorf("async publish subject %s,data %v, failed %v", subject, data, err)
	} else {
		klog.Infof("publish event success data: %#v", data)
	}
}

func PublishAppEventToQueue(owner, name, opType, opID, state, progress string, entranceStatuses []v1alpha1.EntranceStatus) {
	subject := fmt.Sprintf("os.application.%s", owner)

	now := time.Now()
	data := Event{
		EventID:    fmt.Sprintf("%s-%s-%d", owner, name, now.UnixMilli()),
		CreateTime: now,
		Name:       name,
		Type:       "app",
		OpType:     opType,
		OpID:       opID,
		State:      state,
		Progress:   progress,
		User:       owner,
	}
	if len(entranceStatuses) > 0 {
		data.EntranceStatuses = entranceStatuses
	}

	if err := publish(subject, data); err != nil {
		klog.Errorf("async publish subject %s,data %v, failed %v", subject, data, err)
	} else {
		klog.Infof("publish event success data: %#v", data)
	}
}
