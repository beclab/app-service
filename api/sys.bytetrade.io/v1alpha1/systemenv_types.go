package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//+genclient
//+genclient:nonNamespaced
//+kubebuilder:object:root=true
//+kubebuilder:resource:scope=Cluster, shortName={sysenv}, categories={all}
//+kubebuilder:printcolumn:JSONPath=.spec.name, name=env name, type=string
//+kubebuilder:printcolumn:JSONPath=.spec.value, name=value, type=string
//+kubebuilder:printcolumn:JSONPath=.spec.editable, name=editable, type=boolean
//+kubebuilder:printcolumn:JSONPath=.spec.required, name=required, type=boolean
//+kubebuilder:printcolumn:JSONPath=.metadata.creationTimestamp, name=age, type=date

// SystemEnv is the Schema for the system environment variables API
type SystemEnv struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SystemEnvSpec   `json:"spec,omitempty"`
	Status SystemEnvStatus `json:"status,omitempty"`
}

type SystemEnvSpec struct {
	Name        string `json:"name" validate:"required"`
	Value       string `json:"value,omitempty"`
	Default     string `json:"default,omitempty"`
	Editable    bool   `json:"editable,omitempty"`
	Type        string `json:"type,omitempty"`
	Required    bool   `json:"required,omitempty"`
	Description string `json:"description,omitempty"`
}

type SystemEnvStatus struct{}

//+kubebuilder:object:root=true
//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// SystemEnvList contains a list of SystemEnv
type SystemEnvList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SystemEnv `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SystemEnv{}, &SystemEnvList{})
}
