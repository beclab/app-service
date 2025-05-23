package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//+genclient
//+genclient:nonNamespaced
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster, shortName={appmgr}, categories={all}
//+kubebuilder:printcolumn:JSONPath=.spec.appName, name=application name, type=string
//+kubebuilder:printcolumn:JSONPath=.spec.appNamespace, name=namespace, type=string
//+kubebuilder:printcolumn:JSONPath=.status.state, name=state, type=string
//+kubebuilder:printcolumn:JSONPath=.metadata.creationTimestamp, name=age, type=date
//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ApplicationManager is the Schema for the application managers API
type ApplicationManager struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ApplicationManagerSpec   `json:"spec,omitempty"`
	Status ApplicationManagerStatus `json:"status,omitempty"`
}

// ApplicationManagerStatus defines the observed state of ApplicationManager
type ApplicationManagerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	OpType       OpType                  `json:"opType"`
	OpGeneration int64                   `json:"opGeneration"`
	State        ApplicationManagerState `json:"state,omitempty"`
	OpRecords    []OpRecord              `json:"opRecords,omitempty"`
	Message      string                  `json:"message,omitempty"`
	Payload      map[string]string       `json:"payload,omitempty"`
	Progress     string                  `json:"progress,omitempty"`
	UpdateTime   *metav1.Time            `json:"updateTime"`
	StatusTime   *metav1.Time            `json:"statusTime"`
	Completed    bool                    `json:"completed,omitempty"`
	OpTime       *metav1.Time            `json:"opTime,omitempty"`
	LastState    ApplicationManagerState `json:"lastState,omitempty"`
}

// ApplicationManagerSpec defines the desired state of ApplicationManager
type ApplicationManagerSpec struct {
	AppName      string `json:"appName"`
	AppNamespace string `json:"appNamespace,omitempty"`
	AppOwner     string `json:"appOwner,omitempty"`
	Config       string `json:"config,omitempty"`
	Source       string `json:"source"`
	Type         Type   `json:"type"`
}

// OpRecord contains details of an operation.
type OpRecord struct {
	OpType    OpType                  `json:"opType"`
	Message   string                  `json:"message"`
	Version   string                  `json:"version"`
	Source    string                  `json:"source"`
	Status    ApplicationManagerState `json:"status"`
	StateTime *metav1.Time            `json:"statusTime"`
}

//+kubebuilder:object:root=true
//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ApplicationManagerList contains a list of ApplicationManager
type ApplicationManagerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ApplicationManager `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ApplicationManager{}, &ApplicationManagerList{})
}
