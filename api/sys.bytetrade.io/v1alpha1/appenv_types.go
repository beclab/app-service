package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//+genclient
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Namespaced, shortName={appenv}, categories={all}
//+kubebuilder:printcolumn:JSONPath=.spec.appName, name=app name, type=string
//+kubebuilder:printcolumn:JSONPath=.spec.appOwner, name=owner, type=string
//+kubebuilder:printcolumn:JSONPath=.metadata.creationTimestamp, name=age, type=date

// AppEnv is the Schema for the application environment variables API
type AppEnv struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AppEnvSpec   `json:"spec,omitempty"`
	Status AppEnvStatus `json:"status,omitempty"`
}

type AppEnvSpec struct {
	AppName    string         `json:"appName" validate:"required"`
	AppOwner   string         `json:"appOwner" validate:"required"`
	SystemEnvs []SystemEnvRef `json:"systemEnvs,omitempty"`
	Envs       []AppEnvVar    `json:"envs,omitempty"`
	NeedApply  bool           `json:"needApply,omitempty"`
}

// SystemEnvRef defines a reference to a system environment variable
type SystemEnvRef struct {
	Name          string `json:"name" validate:"required"`
	Value         string `json:"value,omitempty"`
	ApplyOnChange bool   `json:"applyOnChange,omitempty"`
	Status        string `json:"status,omitempty"`
}

type AppEnvVar struct {
	Name          string `json:"name" validate:"required"`
	Value         string `json:"value,omitempty"`
	Default       string `json:"default,omitempty"`
	Editable      bool   `json:"editable,omitempty"`
	Type          string `json:"type,omitempty"`
	Required      bool   `json:"required,omitempty"`
	ApplyOnChange bool   `json:"applyOnChange,omitempty"`
	Description   string `json:"description,omitempty"`
}

type AppEnvStatus struct {
}

//+kubebuilder:object:root=true
//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// AppEnvList contains a list of AppEnv
type AppEnvList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AppEnv `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AppEnv{}, &AppEnvList{})
}
