package main

import (
	"testing"

	diff "github.com/r3labs/diff/v2"
	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

var (
	emptyMap = make(map[string]string)
)

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
			oldResource: newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", emptyMap, emptyMap),
			newResource: newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", emptyMap, emptyMap),
			want:        false,
		},
		"rename": {
			oldResource: newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", emptyMap, emptyMap),
			newResource: newUnstructured("extensions/v1beta1", "deployment", "test", "test2", "2", emptyMap, emptyMap),
			want:        true,
		},
		"only resource version": {
			oldResource: newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", emptyMap, emptyMap),
			newResource: newUnstructured("extensions/v1beta1", "deployment", "test", "test", "2", emptyMap, emptyMap),
			want:        false,
		},
	}

	cp := "example/config/config.yaml"
	configPath = &cp
	conf = getConfig()

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			objDiff, _ := diff.Diff(tc.oldResource, tc.newResource)
			got := legitimateChange(objDiff)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestEvaluate(t *testing.T) {
	annotationsTeam := map[string]string{"company.domain/team": "test"}
	oldChartLabel := map[string]string{"helm.sh/chart": "specific-chart-name-3.0.0"}
	newChartLabel := map[string]string{"helm.sh/chart": "specific-chart-name-4.0.0"}

	tests := map[string]struct {
		obj      *unstructured.Unstructured
		existing bool
		want     int
	}{
		"failure": {
			obj:      newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, oldChartLabel),
			existing: false,
			want:     1,
		},
		"success": {
			obj:      newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, newChartLabel),
			existing: false,
			want:     0,
		},
	}

	cp := "example/config/config.yaml"
	configPath = &cp
	conf = getConfig()

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			evaluate(tc.obj, tc.existing)

			// Handle no violation (i.e. no metric increment)
			metricCount := testutil.CollectAndCount(violation)
			var got float64
			if metricCount == 0 {
				got = float64(0)
			} else {
				// Fails if metric has not been incremented
				got = testutil.ToFloat64(violation)
			}

			require.Equal(t, tc.want, int(got))

			// Reset counter for next test
			violation.Reset()
		})
	}
}

func newUnstructured(apiVersion, kind, namespace, name, resourceVersion string, annotations, labels map[string]string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
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
}
