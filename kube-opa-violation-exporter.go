package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/open-policy-agent/opa/rego"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
)

var (
	policy *string
	configPath *string
	config *Config

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

	config := getConfig()

	// TODO: Move back to incluster when done
	// cfg, err := rest.InClusterConfig()
	// if err != nil {
	// 	klog.Fatalf("error building kubeconfig: %s", err.Error())
	// }
	cfg, err := clientcmd.BuildConfigFromFlags("", "/Users/cmacrae/.kube/config")
	if err != nil {
		klog.Fatalf("error building kubeconfig: %s", err.Error())
	}

	// TODO: Move back to incluster when done

	clientSet, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("error building kubernetes clientset: %s", err.Error())
	}

	klog.InitFlags(nil)

	factory := informers.NewSharedInformerFactory(clientSet, time.Second*30)

	var informers []cache.SharedIndexInformer
	for _, obj := range config.Objects {
		o, err := factory.ForResource(obj)
		if err != nil {
			klog.Fatalf("error building generic informer: %s", err.Error())
		}
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
	r := obj.(metav1.Object)
	namespace := r.GetNamespace()
	name := r.GetName()
	klog.Infof("evaluating: %s/%s", namespace, name)
	evaluate(r.GetAnnotations()["kubectl.kubernetes.io/last-applied-configuration"], namespace, name)
}

func onUpdate(oldObj, newObj interface{}) {
	o := oldObj.(metav1.Object).GetAnnotations()["kubectl.kubernetes.io/last-applied-configuration"]
	n := newObj.(metav1.Object).GetAnnotations()["kubectl.kubernetes.io/last-applied-configuration"]
	if o != n {
		onAdd(newObj)
	}
}

func evaluate(jsonManifest, ns, name string) error {
	var manifest map[string]interface{}

	if err := json.Unmarshal([]byte(jsonManifest), &manifest); err != nil {
		return fmt.Errorf("failed to parse 'last-applied-configuration' annotation of resource %s/%s: %v", ns, name, err)
	}

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

	rs, err := pq.Eval(ctx, rego.EvalInput(manifest))
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
