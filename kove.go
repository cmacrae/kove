package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	strimzi "github.com/RedHatInsights/strimzi-client-go/apis/kafka.strimzi.io/v1beta2"
	"github.com/open-policy-agent/opa/rego"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	diff "github.com/r3labs/diff/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	klog "k8s.io/klog/v2"
)

var (
	configPath *string
	conf       *config
	ruleSet    string
	data       string

	wg = new(sync.WaitGroup)

	// Metric type we serve to surface offending objects
	violation = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "opa_policy_violation",
			Help: "Kubernetes object violating policy evaluation.",
		},
		[]string{"name", "namespace", "kind", "api_version", "ruleset", "data"},
	)

	totalViolations = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "opa_policy_violations_total",
			Help: "Total count of policy violations observed.",
		},
	)

	totalViolationsResolved = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "opa_policy_violations_resolved_total",
			Help: "Total count of policy violation resolutions observed.",
		},
	)

	totalObjectEvaluations = prometheus.NewCounter(
		prometheus.CounterOpts{
			Name: "opa_object_evaluations_total",
			Help: "Total count of Kubernetes object evaluations conducted.",
		},
	)
)

// Healthcheck endpoint
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// Web server helper
func serveMetrics(port int) error {
	prometheus.MustRegister(violation)
	prometheus.MustRegister(totalViolations)
	prometheus.MustRegister(totalViolationsResolved)
	prometheus.MustRegister(totalObjectEvaluations)

	http.HandleFunc("/healthz", healthz)
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(fmt.Sprintf(":%d", port), nil)

	return nil
}

// Initialise our flags and start the metric webserver
func init() {
	configPath = flag.String("config", "", "Path to the configuration")

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

	// Disable deprecation warning logs
	rest.SetDefaultWarningHandler(rest.NoWarnings{})

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

	discover, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		klog.ErrorS(err, "unable to construct discovery client")
	}

	var toWatch []schema.GroupVersionResource
	// Log if any of the provided objects aren't supported
	if len(conf.Objects) > 0 {
		for _, r := range conf.Objects {
			if err := discovery.ServerSupportsVersion(discover, r.GroupVersion()); err != nil {
				klog.ErrorS(err, "unsupported object")
			}
		}
		toWatch = conf.Objects
	} else {
		toWatch, err = getRegisteredResources(discover)
		if err != nil {
			klog.ErrorS(err, "unable to retrieve list of registered resources")
		}
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

	startInformers(toWatch, factory)

	// Initiate a stop channel and start our factory with it
	stopCh := make(chan struct{})
	defer close(stopCh)
	defer utilruntime.HandleCrash()
	factory.Start(stopCh)

	// Wait for a stop
	<-stopCh
	klog.InfoS("shutting down informers")
}

// onAdd evaluates the object
func onAdd(obj interface{}) {
	r := obj.(*unstructured.Unstructured)
	if conf.IgnoreChildren && hasOwnerRefs(r) {
		return
	}
	kind := strings.ToLower(r.GetKind())

	klog.InfoS("evaluating object", kind, klog.KObj(r))

	// Allows tests to wait for backgrounded go routine to complete before checking result
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := evaluate(r, 0); err != nil {
			klog.ErrorS(err, "unable to evaluate", kind, klog.KObj(r))
		}
	}()
}

// onUpdate evaluates the object when a legitimate change is observed
func onUpdate(oldObj, newObj interface{}) {
	if conf.IgnoreChildren && hasOwnerRefs(newObj.(*unstructured.Unstructured)) {
		return
	}
	objDiff, err := diff.Diff(oldObj, newObj)
	if err != nil {
		klog.ErrorS(err, "unable to diff object generations")
	}

	// Without this, we see duplicate evaluations
	if legitimateChange(objDiff) {
		metricsRemoved := deleteAllMetricsForObject(oldObj.(*unstructured.Unstructured))
		r := newObj.(*unstructured.Unstructured)
		kind := strings.ToLower(r.GetKind())

		klog.InfoS("change observed, reevaluating object", kind, klog.KObj(r))

		// Allows tests to wait for backgrounded go routine to complete before checking result
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := evaluate(r, metricsRemoved); err != nil {
				klog.ErrorS(err, "unable to evaluate", kind, klog.KObj(r))
			}
		}()
	}
}

// onDelete deletes object associated metrics
func onDelete(obj interface{}) {
	r := obj.(*unstructured.Unstructured)
	if conf.IgnoreChildren && hasOwnerRefs(r) {
		return
	}
	klog.InfoS("object deleted", r.GetKind(), klog.KObj(r))
	deleteAllMetricsForObject(r)
}

func startInformers(toWatch []schema.GroupVersionResource, factory dynamicinformer.DynamicSharedInformerFactory) {
	// Grab an informer for each GVR outlined in our config
	// Add generic event handlers for each informer and start them
	klog.InfoS("starting informers...")
	for _, obj := range toWatch {
		var o informers.GenericInformer

		// Check if we're watching a Strimzi resource type and if so use the
		// the CRD definition in the WithResource call
		if obj.Group == "kafka.strimzi.io" && obj.Version == "v1beta2" {
			klog.Info("using Strimzi v1beta2 CRD resource type %s", obj.Resource)
			o = factory.ForResource(strimzi.GroupVersion.WithResource(obj.Resource))
		} else {
			o = factory.ForResource(obj)
		}

		klog.Infof("watching %s...", strings.TrimPrefix(strings.Join([]string{obj.Group, obj.Version, obj.Resource}, "/"), "/"))
		o.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    onAdd,
			DeleteFunc: onDelete,
			UpdateFunc: onUpdate,
		})
	}
}

// deleteAllMetricsForObjects removes and series associated with a kubernetes object.
// We do not check the result or truthiness intetntionally, as this function
// may be called for an object with no associated metric.
func deleteAllMetricsForObject(obj *unstructured.Unstructured) int {
	return violation.DeletePartialMatch(prometheus.Labels{
		"name":        obj.GetName(),
		"namespace":   obj.GetNamespace(),
		"kind":        obj.GetKind(),
		"api_version": obj.GetAPIVersion(),
	})
}

// legitimateChange inspects a diff.Changelog and reports if its a collection of
// kubernetes object changes that should be considered legitimate
func legitimateChange(cl diff.Changelog) bool {
	if len(cl) == 0 {
		return false
	}

	var ignorable int
	for _, v := range cl {
		if v.Type == "update" && contains(conf.IgnoreDifferingPaths, strings.Join(v.Path, "/")) {
			ignorable++
		}
	}

	if len(cl) == ignorable {
		return false
	}

	return true
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
	if len(ors) > 0 {
		klog.InfoS("ignoring child object", strings.ToLower(obj.GetKind()), klog.KObj(obj))
		return true
	}
	return false
}

// evaluate evaluates a kubernetes object against a rego policy
func evaluate(obj *unstructured.Unstructured, previousViolations int) error {
	// Get our context
	ctx := context.Background()

	// Prepare a rego object for use with our query & policy data
	r := rego.New(rego.Query(conf.RegoQuery), rego.Load(conf.Policies, nil))
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
	violations := 0

	// Range the returned rego expressions in our resulting ruleset.
	// Any violations will expose a Prometheus metric with labels providing object details
	for _, r := range rs {
		for _, e := range r.Expressions {
			for _, i := range e.Value.([]interface{}) {
				m := i.(map[string]interface{})

				if _, ok := m["RuleSet"]; ok {
					ruleSet = m["RuleSet"].(string) // Record globally so we can reference elsewhere
				}

				if _, ok := m["Data"]; ok {
					data = m["Data"].(string) // Record globally so we can reference elsewhere
				}

				violations += 1
				klog.InfoS("violation observed", strings.ToLower(obj.GetKind()), klog.KObj(obj), "ruleset", ruleSet, "data", data)
				registerViolation(
					m["Name"].(string),
					m["Namespace"].(string),
					m["Kind"].(string),
					m["ApiVersion"].(string),
					ruleSet,
					data,
				)
			}
		}
	}

	// If this is an existing object and no violation is found
	// we delete the associated metric (if there is one... if not
	// we just silently ignore it)
	resolvedViolations := previousViolations - violations
	for resolvedViolations > 0 {
		totalViolationsResolved.Inc()
		resolvedViolations -= 1
	}

	// Record the evaluation in the total counter
	totalObjectEvaluations.Inc()

	return nil
}

func registerViolation(name, namespace, kind, apiVersion, ruleset, data string) {
	violation.WithLabelValues(name, namespace, kind, apiVersion, ruleset, data).Set(1)

	// Record the violation in the total counter
	totalViolations.Inc()
}

func getRegisteredResources(discover *discovery.DiscoveryClient) ([]schema.GroupVersionResource, error) {
	var r []schema.GroupVersionResource
	_, resources, err := discover.ServerGroupsAndResources()
	if err != nil {
		return nil, fmt.Errorf("unable to discover server-groups-and-resources")
	}

	// Here, we reason about the sort of resources that should be watched based on verbs.
	// The logic is such that - if it is a user-managed resource (and thus controllable) - it'll
	// likely support said verbs.
	// Along with this, if configured to monitor a specific namespace, we need to only discover
	// namespaced resources
	var filtered []*metav1.APIResourceList
	wantedVerbs := []string{
		"create", "delete", "get", "list", "patch", "update", "watch",
	}
	if conf.Namespace != "" {
		filtered = discovery.FilteredBy(namespacedImportantResource{Verbs: wantedVerbs, NotKind: conf.IgnoreKinds}, resources)
	} else {
		filtered = discovery.FilteredBy(importantResource{Verbs: wantedVerbs, NotKind: conf.IgnoreKinds}, resources)
	}

	gvrs, err := discovery.GroupVersionResources(filtered)
	if err != nil {
		return nil, fmt.Errorf("unable to convert discovered resources to GVRs")
	}

	for k := range gvrs {
		r = append(r, k)
	}

	return r, nil
}

type importantResource struct {
	Verbs   []string
	NotKind []string
}

func (i importantResource) Match(groupVersion string, r *metav1.APIResource) bool {
	return !contains(i.NotKind, strings.ToLower(r.Kind)) &&
		sets.NewString([]string(r.Verbs)...).HasAll(i.Verbs...)
}

type namespacedImportantResource struct {
	Verbs   []string
	NotKind []string
}

func (n namespacedImportantResource) Match(groupVersion string, r *metav1.APIResource) bool {
	return !contains(n.NotKind, strings.ToLower(r.Kind)) &&
		sets.NewString([]string(r.Verbs)...).HasAll(n.Verbs...) &&
		r.Namespaced
}
