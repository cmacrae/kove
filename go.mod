module github.com/cmacrae/kube-opa-violation-exporter

go 1.14

require (
	github.com/open-policy-agent/opa v0.25.2
	github.com/prometheus/client_golang v1.7.1
	github.com/spf13/viper v1.7.1
	k8s.io/apimachinery v0.20.1
	k8s.io/client-go v0.20.1
	k8s.io/klog/v2 v2.4.0
)
