package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"time"

	"io/ioutil"

	"github.com/open-policy-agent/opa/rego"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"
)

var (
	policy *string

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

	go func() {
		if err := serveMetrics(3000); err != nil {
			klog.Infof("Unable to serve metric: %v\n", err.Error())
		}
	}()
}

func main() {
	flag.Parse()

	// TODO: Move back to incluster when done
	// cfg, err := rest.InClusterConfig()
	// if err != nil {
	// 	klog.Fatalf("Error building kubeconfig: %s", err.Error())
	// }

	// clientSet, err := kubernetes.NewForConfig(cfg)
	// if err != nil {
	// 	klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	// }
	// TODO: Move back to incluster when done

	cfg, err := clientcmd.BuildConfigFromFlags("", "/Users/cmacrae/.kube/config")
	if err != nil {
		klog.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	clientSet, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		klog.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}

	klog.InitFlags(nil)

	factory := informers.NewSharedInformerFactory(clientSet, time.Second*30)

	inf, err := factory.ForResource(schema.GroupVersionResource{
		Group:    "apps",
		Version:  "v1",
		Resource: "deployments"})
	// Resource: "replicasets"})

	if err != nil {
		klog.Fatalf("Error building generic informer: %s", err.Error())
	}

	informer := inf.Informer()

	stopCh := make(chan struct{})
	defer close(stopCh)
	defer utilruntime.HandleCrash()

	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: onAdd,
		// DeleteFunc: onDelete, TODO
		UpdateFunc: onUpdate,
	})

	klog.Info("Starting informer")
	defer utilruntime.HandleCrash()
	factory.Start(stopCh)
	go informer.Run(stopCh)
	<-stopCh
	klog.Info("Shutting down workers")
}

func onAdd(obj interface{}) {
	r := obj.(metav1.Object)
	namespace := r.GetNamespace()
	name := r.GetName()
	klog.Infof("Evaluating: %s/%s", namespace, name)
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
				klog.Infof("Violation: %s/%s", ns, name)
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
