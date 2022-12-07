package main

import (
	"testing"

	diff "github.com/r3labs/diff/v2"
	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

var (
	emptyMap        = make(map[string]string)
	annotationsTeam = map[string]string{"company.domain/team": "test"}
	oldChartLabel   = map[string]string{"helm.sh/chart": "specific-chart-name-3.0.0"}
	newChartLabel   = map[string]string{"helm.sh/chart": "specific-chart-name-4.0.0"}
)

func initConfig() {
	cp := "example/config/config.yaml"
	configPath = &cp
	conf = getConfig()
}

func TestContains(t *testing.T) {
	tests := map[string]struct {
		input1 []string
		input2 string
		want   bool
	}{
		"single item":    {input1: []string{"string1"}, input2: "string1", want: true},
		"multiple items": {input1: []string{"string1", "string2"}, input2: "string1", want: true},
		"not present":    {input1: []string{"string1", "string2"}, input2: "not present", want: false},
		"empty list":     {input1: []string{}, input2: "string1", want: false},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := contains(tc.input1, tc.input2)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestLegitimateChange(t *testing.T) {
	tests := map[string]struct {
		oldResource *unstructured.Unstructured
		newResource *unstructured.Unstructured
		want        bool
	}{
		"same resource": {
			oldResource: newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", emptyMap, emptyMap, false),
			newResource: newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", emptyMap, emptyMap, false),
			want:        false,
		},
		"rename": {
			oldResource: newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", emptyMap, emptyMap, false),
			newResource: newUnstructured("extensions/v1beta1", "deployment", "test", "test2", "2", emptyMap, emptyMap, false),
			want:        true,
		},
		"only resource version": {
			oldResource: newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", emptyMap, emptyMap, false),
			newResource: newUnstructured("extensions/v1beta1", "deployment", "test", "test", "2", emptyMap, emptyMap, false),
			want:        false,
		},
	}

	initConfig()

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			objDiff, _ := diff.Diff(tc.oldResource, tc.newResource)
			got := legitimateChange(objDiff)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestHasOwnerRefs(t *testing.T) {
	tests := map[string]struct {
		obj  *unstructured.Unstructured
		want bool
	}{
		"no owner": {
			obj:  newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", emptyMap, emptyMap, false),
			want: false,
		},
		"with owner": {
			obj:  newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", emptyMap, emptyMap, true),
			want: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := hasOwnerRefs(tc.obj)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestEvaluate(t *testing.T) {
	tests := map[string]struct {
		obj        *unstructured.Unstructured
		existing   bool
		resetCount bool // Should the violation counter metric be reset after the test run.
		want       int
	}{
		"success": {
			obj:        newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, newChartLabel, false),
			existing:   false,
			resetCount: false,
			want:       0,
		},
		"failure": {
			obj:        newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, oldChartLabel, false),
			existing:   false,
			resetCount: false,
			want:       1,
		},
		// Should remove series from previous run
		"success updating previous": {
			obj:        newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, newChartLabel, false),
			existing:   true,
			resetCount: true,
			want:       0,
		},
	}

	initConfig()

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			evaluate(tc.obj, tc.existing)

			got := getViolationMetric()

			require.Equal(t, tc.want, got)

			if tc.resetCount {
				// Reset counter for next test
				violation.Reset()
			}
		})
	}
}

func TestOnAdd(t *testing.T) {
	tests := map[string]struct {
		obj        *unstructured.Unstructured
		existing   bool
		resetCount bool // Should the violation counter metric be reset after the test run.
		want       int
	}{
		"add good": {
			obj:        newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, newChartLabel, false),
			resetCount: true,
			want:       0,
		},
		"add bad": {
			obj:        newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, oldChartLabel, false),
			resetCount: true,
			want:       1,
		},
		"add bad child object": {
			obj:        newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, oldChartLabel, true),
			resetCount: true,
			want:       0,
		},
	}

	initConfig()

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			onAdd(tc.obj)
			wg.Wait()
			got := getViolationMetric()

			require.Equal(t, tc.want, got)

			if tc.resetCount {
				// Reset counter for next test
				violation.Reset()
			}
		})
	}
}

func TestOnUpdate(t *testing.T) {

}

func TestOnDelete(t *testing.T) {

}

func getViolationMetric() int {
	// Handle no violation (i.e. no metric increment)
	metricCount := testutil.CollectAndCount(violation)
	var got float64
	if metricCount == 0 {
		got = float64(0)
	} else {
		// Fails if metric has not been incremented
		got = testutil.ToFloat64(violation)
	}

	return int(got)
}

func newUnstructured(apiVersion, kind, namespace, name, resourceVersion string, annotations, labels map[string]string, setOwnerRef bool) *unstructured.Unstructured {
	ret := unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"namespace":       namespace,
				"name":            name,
				"resourceVersion": resourceVersion,
				"annotations":     annotations,
				"labels":          labels,
			},
			"spec": "test",
		},
	}

	if setOwnerRef {
		var ownerReferences []metav1.OwnerReference
		ownerReferences = append(ownerReferences, metav1.OwnerReference{
			APIVersion: apiVersion,
			Kind:       kind,
			Name:       name,
			UID:        "ad834522-d9a5-4841-beac-991ff3798c00",
		})
		ret.SetOwnerReferences(ownerReferences)
	}

	return &ret
}
