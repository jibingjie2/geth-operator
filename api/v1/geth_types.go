package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// GethSpec 定义用户在 CR 中指定的配置
type GethSpec struct {
	Image string `json:"image"`
	//NodeType string   `json:"nodeType"`
	Cmd  []string          `json:"cmd,omitempty"`
	Args []string          `json:"args"`
	Env  map[string]string `json:"env,omitempty"`
	// +kubebuilder:default:="123456"
	Password string `json:"password,omitempty"`
	//Phase    string `json:"phase,omitempty" protobuf:"bytes,1,opt,name=phase"`
	PVCName string `json:"pvcName,omitempty"`
	//GenesisConfigMap string            `json:"genesisConfigMap"`
}

// GethStatus 定义 Geth 的当前运行状态
type GethStatus struct {
	Phase          string `json:"phase,omitempty"`
	AccountAddress string `json:"accountAddress,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Geth 是 Geth 的 Schema
type Geth struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   GethSpec   `json:"spec,omitempty"`
	Status GethStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// GethList 是 Geth 资源的列表
type GethList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Geth `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Geth{}, &GethList{})
}
