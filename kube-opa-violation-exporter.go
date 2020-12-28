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
	diff "github.com/r3labs/diff/v2"
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
	conf       *config
	ruleSet    string

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

// Initialise our flags and start the metric webserver
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
	// Parse our flags and set up configuration
	flag.Parse()
	klog.InitFlags(nil)
	conf = getConfig()

	// If we're inside the cluster, get our config from there.
	// Otherwise, construct one from a kube config file
	cfg, err := rest.InClusterConfig()
	if kubeconfig := os.Getenv("KUBECONFIG"); kubeconfig != "" {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if err != nil {
		klog.ErrorS(err, "unable to retrieve kube config")
		os.Exit(1)
	}

	// Initiate a dynamic client from our configuration
	dc, err := dynamic.NewForConfig(cfg)
	if err != nil {
		klog.ErrorS(err, "unable to construct kube config")
		os.Exit(1)
	}

	// Construct a dynamic informer from our client.
	// From this, we can grab informers for multiple kinds of kubernetes objects.
	// If a 'namespace' value has been provided in the configuration, this factory
	// will only lease informers for objects in that namespace. Otherwise (if 'namespace' is omitted
	// or an empty string) provide informers for all namespaces
	factory := dynamicinformer.NewFilteredDynamicSharedInformerFactory(dc, 0, conf.Namespace, nil)

	// Log where we're watching
	if conf.Namespace != "" {
		klog.InfoS("monitoring '" + conf.Namespace + "' namespace...")
	} else {
		klog.InfoS("monitoring all namespaces...")
	}

	// Grab an informer for each GVR outlined in our config
	var informers []cache.SharedIndexInformer
	for _, obj := range conf.Objects {
		o := factory.ForResource(obj)
		klog.InfoS("watching " + obj.Resource + "...")
		informers = append(informers, o.Informer())
	}

	// Initiate a stop channel and start our factory with it
	stopCh := make(chan struct{})
	defer close(stopCh)
	defer utilruntime.HandleCrash()
	factory.Start(stopCh)

	// Add generic event handlers for each informer and start them
	klog.InfoS("starting informers...")
	for _, i := range informers {
		i.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    onAdd,
			DeleteFunc: onDelete,
			UpdateFunc: onUpdate,
		})
		go i.Run(stopCh)
	}

	// Wait for a stop
	<-stopCh
	klog.InfoS("shutting down informers")
}

// onAdd evaluates the object
func onAdd(obj interface{}) {
	r := obj.(*unstructured.Unstructured)
	if conf.IgnoreChildren && hasOwnerRefs(r){
		return
	}
	kind := strings.ToLower(r.GetKind())

	klog.InfoS("evaluating object", kind, klog.KObj(r))
	if err := evaluate(r, false); err != nil {
		klog.ErrorS(err, "unable to evaluate", kind, klog.KObj(r))
	}
}

// onUpdate evaluates the object when a legitimate change is observed
func onUpdate(oldObj, newObj interface{}) {
	if conf.IgnoreChildren && hasOwnerRefs(newObj.(*unstructured.Unstructured)){
		return
	}
	objDiff, err := diff.Diff(oldObj, newObj)
	if err != nil {
		klog.ErrorS(err, "unable to diff object generations")
	} 

	// Without this, we see duplicate evaluations
	if legitimateChange(objDiff) {
		r := newObj.(*unstructured.Unstructured)
		kind := strings.ToLower(r.GetKind())

		klog.InfoS("change observed, reevaluating object", kind, klog.KObj(r))
		if err := evaluate(r, true); err != nil {
			klog.ErrorS(err, "unable to evaluate", kind, klog.KObj(r))
		}
	}
}

// onDelete deletes object associated metrics
func onDelete(obj interface{}) {
	r := obj.(*unstructured.Unstructured)
	if conf.IgnoreChildren && hasOwnerRefs(r){
		return
	}
	klog.InfoS("object deleted", r.GetKind(), klog.KObj(r))
	deleteMetric(r)
}

// deleteMetric deletes metrics associated with a kubernetes object.
// We do not check the result or truthiness intetntionally, as this function
// may be called for an object with no associated metric.
func deleteMetric(o *unstructured.Unstructured) {
	name := o.GetName()
	namespace := o.GetNamespace()
	kind := o.GetKind()
	api := o.GetAPIVersion()

	violation.DeleteLabelValues(name, namespace, kind, api, ruleSet)
}

// legitimateChange inspects a diff.Changelog and reports if its a collection of
// kubernetes object changes that can be ignored.
func legitimateChange(cl diff.Changelog) bool {
	var results []bool
	ignorePath := []string{
		"Object/metadata/resourceVersion",
		"Object/metadata/managedFields/0/time",
		"Object/status/observedGeneration",
	}

	for _, v := range cl {
		if (v.Type == "update" && contains(ignorePath, strings.Join(v.Path, "/"))) {
			results = append(results, true)
		}
	}

	if len(results) == 3 {
		if results[0] && results[1] && results[2] {
			return true
		}
	}
	return false
}

// contains is a simple helper func to assert the presence of a string in a slice
func contains(l []string, s string) bool {
	for _, v := range l {
		if v == s {
			return true
		}
	}
	return false
}

// hasOwnerRefs checks if an object has any owner references.
// This is useful for circumstances where you may wish to avoid child objects.
func hasOwnerRefs(obj *unstructured.Unstructured) bool {
	ors := obj.GetOwnerReferences()
	if len(ors) > 0  {
		klog.InfoS("ignoring child object", strings.ToLower(obj.GetKind()), klog.KObj(obj))
		return true
	}
	return false
}

// evaluate evaluates a kubernetes object against a rego policy
func evaluate(obj *unstructured.Unstructured, existing bool) error {
	// Get our context
	ctx := context.Background()

	// Load our policy data from a file
	policyData, err := ioutil.ReadFile(*policy)
	if err != nil {
		klog.ErrorS(err, "unable to read policy")
	}

	// Prepare a rego object for use with our query & policy data
	r := rego.New(rego.Query(conf.RegoQuery))
	rego.Module("policy", string(policyData))(r)
	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		klog.ErrorS(err, "unable to prepare query from policy data")
	}

	// Evaluate the kubernetes object against our prepared query
	rs, err := pq.Eval(ctx, rego.EvalInput(obj.Object))
	if err != nil {
		klog.ErrorS(err, "unable to evaluate prepared query")
	}

	// Set up a var that indicates the presence of a violation
	var vio bool

	// Range the returned rego expressions in our resulting ruleset.
	// Any violations will expose a Prometheus metric with labels providing object details
	for _, r := range rs {
		for _, e := range r.Expressions {
			for _, i := range e.Value.([]interface{}) {
				m := i.(map[string]interface{})
				ruleSet = m["RuleSet"].(string) // Record globally so we can reference elsewhere
				vio = true
				klog.InfoS("violation observed", strings.ToLower(obj.GetKind()), klog.KObj(obj), "ruleset", m["RuleSet"].(string))
				violation.WithLabelValues(
					m["Name"].(string),
					m["Namespace"].(string),
					m["Kind"].(string),
					m["ApiVersion"].(string),
					ruleSet,
				).Set(1)
			}
		}
	}

	// If this is an existing object and no violation is found
	// we delete the associated metric (if there is one... if not
	// we just silently ignore it)
	if (existing && !vio) {
		deleteMetric(obj)
	}

	return nil
}
