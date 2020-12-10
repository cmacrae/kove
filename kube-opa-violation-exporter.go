package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/open-policy-agent/opa/rego"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

var (
	policy *string

	// The one metric type we serve to surface offending manifests
	violation = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "deprecated_k8s_manifest",
			Help: "Kubernetes manifest offending deprecation evaluation.",
		},
		[]string{"name", "namespace", "kind", "api_version", "ruleset"},
	)
)

// Just a healthcheck endpoint function
func healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

// Web server helper function called in init
func serveMetrics(port int) error {
	prometheus.MustRegister(violation)

	http.HandleFunc("/healthz", healthz)
	http.Handle("/metrics", promhttp.Handler())
	http.ListenAndServe(fmt.Sprintf(":%d", port), nil)

	return nil
}

// Collects live manifests from within the Kubernetes cluster
func collectManifests(ctx context.Context) ([]map[string]interface{}, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	clientSet, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	gvrs := []schema.GroupVersionResource{
		{Group: "apps", Version: "v1", Resource: "daemonsets"},
		{Group: "apps", Version: "v1", Resource: "deployments"},
		{Group: "apps", Version: "v1", Resource: "replicasets"},
		{Group: "apps", Version: "v1", Resource: "statefulsets"},
		{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"},
		{Group: "policy", Version: "v1beta1", Resource: "podsecuritypolicies"},
		{Group: "extensions", Version: "v1beta1", Resource: "ingresses"},
	}

	var results []map[string]interface{}
	for _, g := range gvrs {
		ri := clientSet.Resource(g)
		rs, err := ri.List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}

		for _, r := range rs.Items {
			if jsonManifest, ok := r.GetAnnotations()["kubectl.kubernetes.io/last-applied-configuration"]; ok {
				var manifest map[string]interface{}

				err := json.Unmarshal([]byte(jsonManifest), &manifest)
				if err != nil {
					err := fmt.Errorf("failed to parse 'last-applied-configuration' annotation of resource %s/%s: %v", r.GetNamespace(), r.GetName(), err)
					return nil, err
				}
				results = append(results, manifest)
			}
		}
	}

	return results, nil
}

func init() {
	policy = flag.String("policy", "policy.rego", "Path to the policy to evaluate")
	flag.Parse()

	go func() {
		if err := serveMetrics(3000); err != nil {
			log.Printf("Unable to serve metric: %v\n", err)
		}
	}()
}

func main() {
	ctx := context.Background()

	policyData, err := ioutil.ReadFile(*policy)
	if err != nil {
		log.Fatal(err)
	}

	r := rego.New(
		rego.Query("data[_].main"),
	)

	rego.Module("policy", string(policyData))(r)
	if err != nil {
		log.Fatal(err)
	}

	pq, err := r.PrepareForEval(ctx)
	if err != nil {
		log.Fatal(err)
	}

	for range time.NewTicker(1 * time.Minute).C {
		input, err := collectManifests(ctx)
		if err != nil {
			log.Fatal(err)
		}

		rs, err := pq.Eval(ctx, rego.EvalInput(input))
		if err != nil {
			log.Fatal(err)
		}

		if len(rs) > 0 {
			for _, r := range rs {
				for _, e := range r.Expressions {
					for _, i := range e.Value.([]interface{}) {
						m := i.(map[string]interface{})
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
		} else {
			log.Println("no violations found")
		}
	}
}
