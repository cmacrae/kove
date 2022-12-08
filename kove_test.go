package main

import (
	"fmt"
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
		obj                *unstructured.Unstructured
		previousViolations int
		resetCount         bool // Should the violation counter metric be reset after the test run.
		want               int
	}{
		"success": {
			obj:                newUnstructured("extensions/v1beta1", "deployment", "testEvaluate", "testEvaluate", "1", annotationsTeam, getChartLabels("4.0.0"), false),
			previousViolations: 0,
			resetCount:         true,
			want:               0,
		},
		"failure": {
			obj:                newUnstructured("extensions/v1beta1", "deployment", "testEvaluate", "testEvaluate", "1", annotationsTeam, getChartLabels("3.0.0"), false),
			previousViolations: 0,
			resetCount:         true,
			want:               1,
		},
	}

	initConfig()

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			evaluate(tc.obj, tc.previousViolations)

			got := getNumberOfViolations()

			if tc.resetCount {
				// Reset counter for next test
				violation.Reset()
			}

			require.Equal(t, tc.want, got)
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
			obj:        newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, getChartLabels("4.0.0"), false),
			resetCount: true,
			want:       0,
		},
		"add bad": {
			obj:        newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, getChartLabels("3.0.0"), false),
			resetCount: true,
			want:       1,
		},
		"add bad child object": {
			obj:        newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, getChartLabels("3.0.0"), true),
			resetCount: true,
			want:       0,
		},
	}

	initConfig()

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			onAdd(tc.obj)
			wg.Wait()
			got := getNumberOfViolations()

			if tc.resetCount {
				// Reset counter for next test
				violation.Reset()
			}

			require.Equal(t, tc.want, got)
		})
	}
}

func TestOnUpdate(t *testing.T) {
	tests := map[string]struct {
		oldObj     *unstructured.Unstructured
		newObj     *unstructured.Unstructured
		resetCount bool // Should the violation counter metric be reset after the test run.
		want       int
	}{
		"both good": {
			oldObj:     newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, getChartLabels("4.0.0"), false),
			newObj:     newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, getChartLabels("4.0.1"), false),
			resetCount: true,
			want:       0,
		},
		"good then bad": {
			oldObj:     newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, getChartLabels("4.0.0"), false),
			newObj:     newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, getChartLabels("3.0.0"), false),
			resetCount: true,
			want:       1,
		},
		"both bad": {
			oldObj:     newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, getChartLabels("3.0.0"), false),
			newObj:     newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, getChartLabels("3.0.1"), false),
			resetCount: true,
			want:       1,
		},
		"bad then good": {
			oldObj:     newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, getChartLabels("3.0.0"), false),
			newObj:     newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, getChartLabels("4.0.0"), false),
			resetCount: true,
			want:       0,
		},
	}

	initConfig()

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			onAdd(tc.oldObj)
			wg.Wait()

			onUpdate(tc.oldObj, tc.newObj)
			wg.Wait()
			got := getNumberOfViolations()

			if tc.resetCount {
				// Reset counter for next test
				violation.Reset()
			}

			require.Equal(t, tc.want, got)
		})
	}
}

func TestOnDelete(t *testing.T) {
	tests := map[string]struct {
		obj        *unstructured.Unstructured
		resetCount bool // Should the violation counter metric be reset after the test run.
		want       int
	}{
		"delete bad": {
			obj:        newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, getChartLabels("3.0.0"), false),
			resetCount: true,
			want:       0,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			onAdd(tc.obj)
			wg.Wait()

			onDelete(tc.obj)
			wg.Wait()

			got := getNumberOfViolations()

			if tc.resetCount {
				// Reset counter for next test
				violation.Reset()
			}

			require.Equal(t, tc.want, got)
		})
	}
}

func TestDeleteAllMetricsForObject(t *testing.T) {
	tests := map[string]struct {
		obj         *unstructured.Unstructured
		metricCount int  // Number of metrics to create
		resetCount  bool // Should the violation counter metric be reset after the test run.
		want        int
	}{
		"delete with single": {
			obj:         newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, getChartLabels("3.0.0"), false),
			metricCount: 1,
			resetCount:  true,
			want:        0,
		},
		"delete with multiple for same object": {
			obj:         newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, getChartLabels("3.0.0"), false),
			metricCount: 4,
			resetCount:  true,
			want:        0,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			metricsToCreate := tc.metricCount
			for metricsToCreate != 0 {
				registerViolation(
					tc.obj.GetName(),
					tc.obj.GetNamespace(),
					tc.obj.GetKind(),
					tc.obj.GetAPIVersion(),
					fmt.Sprintf("ruleset-%d", metricsToCreate),
					fmt.Sprintf("data-%d", metricsToCreate),
				)
				metricsToCreate -= 1
			}

			deleteAllMetricsForObject(tc.obj)
			got := getNumberOfViolations()

			if tc.resetCount {
				// Reset counter for next test
				violation.Reset()
			}

			require.Equal(t, tc.want, got)
		})
	}

	t.Run("delete with multiple for different objects", func(t *testing.T) {
		objToDelete := newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1", annotationsTeam, getChartLabels("3.0.0"), false)
		otherObj := newUnstructured("extensions/v1beta1", "deployment", "other", "other", "1", annotationsTeam, getChartLabels("3.0.0"), false)

		i := 0
		metricsToCreate := 3
		for i < metricsToCreate {
			registerViolation(
				objToDelete.GetName(),
				objToDelete.GetNamespace(),
				objToDelete.GetKind(),
				objToDelete.GetAPIVersion(),
				fmt.Sprintf("ruleset-%d", i),
				fmt.Sprintf("data-%d", i),
			)
			registerViolation(
				otherObj.GetName(),
				otherObj.GetNamespace(),
				otherObj.GetKind(),
				otherObj.GetAPIVersion(),
				fmt.Sprintf("ruleset-%d", i),
				fmt.Sprintf("data-%d", i),
			)
			i += 1
		}

		deleteAllMetricsForObject(objToDelete)
		got := getNumberOfViolations()
		violation.Reset()
		require.Equal(t, metricsToCreate, got)
	})
}

func getNumberOfViolations() int {
	return testutil.CollectAndCount(violation)
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

func getChartLabels(vers string) map[string]string {
	return map[string]string{"helm.sh/chart": fmt.Sprintf("specific-chart-name-%s", vers)}
}
