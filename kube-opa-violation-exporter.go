package main

import (
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"

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
	klog "k8s.io/klog/v2"
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
			klog.ErrorS(err, "unable to serve metric")
			os.Exit(1)
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
		klog.ErrorS(err, "unable to retrieve kube config")
		os.Exit(1)
	}

	dc, err := dynamic.NewForConfig(cfg)
	if err != nil {
		klog.ErrorS(err, "unable to construct kube config")
		os.Exit(1)
	}

	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dc, 0, config.Namespace, nil)

	if config.Namespace != "" {
		klog.InfoS("monitoring '" + config.Namespace + "' namespace...")
	} else {
		klog.InfoS("monitoring all namespaces...")
	}
	var informers []cache.SharedIndexInformer
	for _, obj := range config.Objects {
		o := factory.ForResource(obj)
		klog.InfoS("watching " + obj.Resource + "...")
		informers = append(informers, o.Informer())
	}

	stopCh := make(chan struct{})
	defer close(stopCh)
	defer utilruntime.HandleCrash()
	factory.Start(stopCh)

	klog.InfoS("starting informers...")
	for _, i := range informers {
		i.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: onAdd,
			// DeleteFunc: onDelete, TODO
			UpdateFunc: onUpdate,
		})
		go i.Run(stopCh)
	}

	<-stopCh
	klog.InfoS("shutting down informers")
}

func onAdd(obj interface{}) {
	r := obj.(*unstructured.Unstructured)
	kind := strings.ToLower(r.GetKind())

	klog.InfoS("evaluating object", kind, klog.KObj(r))
	if err := evaluate(r); err != nil {
		klog.ErrorS(err, "unable to evaluate", kind, klog.KObj(r), )
	}
}

// TODO: Write this properly...
func onUpdate(oldObj, newObj interface{}) {
	o := oldObj.(metav1.Object).GetAnnotations()["kubectl.kubernetes.io/last-applied-configuration"]
	n := newObj.(metav1.Object).GetAnnotations()["kubectl.kubernetes.io/last-applied-configuration"]
	if o != n {
		onAdd(newObj)
	}
}

func evaluate(obj *unstructured.Unstructured) error {
	ctx := context.Background()

	policyData, err := ioutil.ReadFile(*policy)
	if err != nil {
		klog.ErrorS(err, "unable to read policy")
	}

	r := rego.New(
		rego.Query("data[_].main"),
	)

	rego.Module("policy", string(policyData))(r)

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		klog.ErrorS(err, "unable to prepare query from policy data")
	}

	rs, err := pq.Eval(ctx, rego.EvalInput(obj.Object))
	if err != nil {
		klog.ErrorS(err, "unable to evaluate prepared query")
	}

	for _, r := range rs {
		for _, e := range r.Expressions {
			for _, i := range e.Value.([]interface{}) {
				m := i.(map[string]interface{})
				klog.InfoS("violation observed", strings.ToLower(obj.GetKind()), klog.KObj(obj), "ruleset", m["RuleSet"].(string))
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
