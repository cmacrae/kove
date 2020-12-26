package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/open-policy-agent/opa/rego"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
)

var (
	policy     *string
	configPath *string
	config     *Config

	// The one metric type we serve to surface offending manifests
	violation = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "policy_violation",
			Help: "Kubernetes manifest violating policy evaluation.",
		},
		[]string{"name", "namespace", "kind", "api_version", "ruleset"},
	)
)

// Healthcheck endpoint
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// Web server helper
func serveMetrics(port int) error {
	prometheus.MustRegister(violation)

	http.HandleFunc("/healthz", healthz)
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(fmt.Sprintf(":%d", port), nil)

	return nil
}

func init() {
	policy = flag.String("policy", "policy.rego", "Path to the policy to evaluate")
	configPath = flag.String("config", "config.yaml", "Path to the configuration")

	go func() {
		if err := serveMetrics(3000); err != nil {
			klog.Infof("unable to serve metric: %v\n", err.Error())
		}
	}()
}

func main() {
	flag.Parse()
	klog.InitFlags(nil)
	config := getConfig()

	cfg, err := rest.InClusterConfig()
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if err != nil {
		klog.Fatalf("unable to retrieve kube config: %s", err.Error())
	}

	dc, err := dynamic.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("unable to construct kube config: %s", err.Error())
	}

	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dc, 0, config.Namespace, nil)

	if config.Namespace != "" {
		klog.Infof("monitoring '%s' namespace...", config.Namespace)
	} else {
		klog.Info("monitoring all namespaces...")
	}
	var informers []cache.SharedIndexInformer
	for _, obj := range config.Objects {
		o := factory.ForResource(obj)
		klog.Infof("watching %s...", obj.Resource)
		informers = append(informers, o.Informer())
	}

	stopCh := make(chan struct{})
	defer close(stopCh)
	defer utilruntime.HandleCrash()
	factory.Start(stopCh)

	klog.Info("starting informers")
	for _, i := range informers {
		i.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: onAdd,
			// DeleteFunc: onDelete, TODO
			UpdateFunc: onUpdate,
		})
		go i.Run(stopCh)
	}

	<-stopCh
	klog.Info("shutting down informers")
}

func onAdd(obj interface{}) {
	r := obj.(*unstructured.Unstructured)
	namespace := r.GetNamespace()
	name := r.GetName()

	klog.Infof("evaluating: %s/%s", namespace, name)
	if err := evaluate(r, namespace, name); err != nil {
		klog.Infof("unable to evaluate %s/%s: %s", namespace, name, err.Error())
	}
}

func onUpdate(oldObj, newObj interface{}) {
	o := oldObj.(metav1.Object).GetAnnotations()["kubectl.kubernetes.io/last-applied-configuration"]
	n := newObj.(metav1.Object).GetAnnotations()["kubectl.kubernetes.io/last-applied-configuration"]
	if o != n {
		onAdd(newObj)
	}
}

func evaluate(obj *unstructured.Unstructured, ns, name string) error {
	ctx := context.Background()

	policyData, err := ioutil.ReadFile(*policy)
	if err != nil {
		klog.Fatal(err.Error())
	}

	r := rego.New(
		rego.Query("data[_].main"),
	)

	rego.Module("policy", string(policyData))(r)
	if err != nil {
		klog.Fatal(err.Error())
	}

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		klog.Fatal(err.Error())
	}

	rs, err := pq.Eval(ctx, rego.EvalInput(obj.Object))
	if err != nil {
		klog.Fatal(err.Error())
	}

	for _, r := range rs {
		for _, e := range r.Expressions {
			for _, i := range e.Value.([]interface{}) {
				m := i.(map[string]interface{})
				klog.Infof("violation: %s/%s", ns, name)
				violation.WithLabelValues(
					m["Name"].(string),
					m["Namespace"].(string),
					m["Kind"].(string),
					m["ApiVersion"].(string),
					m["RuleSet"].(string),
				).Set(1)
			}
		}
	}
	return nil
}
