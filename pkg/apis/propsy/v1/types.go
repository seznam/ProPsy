package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ProPsyServiceSpec struct {
	Service                      string   `json:"service"`
	ServicePort                  int      `json:"servicePort"`
	Listen                       string   `json:"listen"`
	Disabled                     bool     `json:"disabled"`
	Percent                      int      `json:"percent"`
	Nodes                        []string `json:"nodes"`
	CanaryService                string   `json:"canaryService"`
	CanaryPercent                int      `json:"canaryPercent"`
	Timeout                      int      `json:"timeout"`
	ConnectTimeout               int      `json:"connectTimeout"`
	MaxRequestsPerConnection     int      `json:"maxRequestsPerConnection"`
	Type                         string   `json:"type"`
	PathPrefix                   string   `json:"pathPrefix"`
	PrefixRewrite                string   `json:"prefixRewrite"`
	TLSCertificateSecret         string   `json:"tlsCertificateSecret"`
	HealthCheckTimeout           int      `json:"healthCheckTimeout"`
	HealthCheckInterval          int      `json:"healthCheckInterval"`
	HealthCheckUnhealthyTreshold int      `json:"healthCheckUnhealthyTreshold"`
	HealthCheckHealthyTreshold   int      `json:"healthCheckHealthyTreshold"`
	HealthCheckReuseConnection   bool     `json:"healthCheckReuseConnection"`
	HealthCheckHealthChecker     string   `json:"healthCheckType"`
	HealthCheckHTTPPath          string   `json:"healthCheckHTTPPath"`
	HealthCheckHTTPHost          string   `json:"healthCheckHTTPHost"`
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ProPsyService struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ProPsyServiceSpec `json:"spec"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type ProPsyServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ProPsyService `json:"items"`
}
