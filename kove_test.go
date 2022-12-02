package main

import (
	"testing"

	diff "github.com/r3labs/diff/v2"
	"github.com/stretchr/testify/require"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/prometheus/client_golang/prometheus/testutil"
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
			oldResource: newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1"),
			newResource: newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1"),
			want:        false,
		},
		"rename": {
			oldResource: newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1"),
			newResource: newUnstructured("extensions/v1beta1", "deployment", "test", "test2", "2"),
			want:        true,
		},
		"only resource version": {
			oldResource: newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1"),
			newResource: newUnstructured("extensions/v1beta1", "deployment", "test", "test", "2"),
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
	tests := map[string]struct {
		obj      *unstructured.Unstructured
		existing bool
		want     int
	}{
		"success": {
			obj:      newUnstructured("extensions/v1beta1", "deployment", "test", "test", "1"),
			existing: false,
			want:     1,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// TODO - Get evaluate working
			evaluate(tc.obj, tc.existing)
			got := testutil.ToFloat64(violation)
			require.Equal(t, tc.want, got)
		})
	}
}

func newUnstructured(apiVersion, kind, namespace, name, resourceVersion string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": apiVersion,
			"kind":       kind,
			"metadata": map[string]interface{}{
				"namespace":       namespace,
				"name":            name,
				"resourceVersion": resourceVersion,
			},
			"spec": "test",
		},
	}
}
