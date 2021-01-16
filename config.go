package main

import (
	"os"

	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/runtime/schema"
	klog "k8s.io/klog/v2"
)

// config outlines which namespace to watch objects in and which objects to watch
type config struct {
	Namespace            string                        `yaml:"namespace,omitempty"`
	Objects              []schema.GroupVersionResource `yaml:"objects,omitempty"`
	Policies             []string                      `yaml:"policies,omitempty"`
	IgnoreChildren       bool                          `yaml:"ignoreChildren,omitempty"`
	IgnoreKinds          []string                      `yaml:"ignoreKinds,omitempty"`
	IgnoreDifferingPaths []string                      `yaml:"ignoreDifferingPaths,omitempty"`
	RegoQuery            string                        `yaml:"regoQuery,omitempty"`
}

// getConfig returns a default config object
func getConfig() *config {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.SetConfigName("config.yaml")
	if *configPath != "" {
		viper.SetConfigFile(*configPath)
	}
	if err := viper.ReadInConfig(); err != nil {
		klog.ErrorS(err, "unable to read config")
		os.Exit(1)
	}

	conf := &config{}
	if err := viper.Unmarshal(conf); err != nil {
		klog.ErrorS(err, "invalid config")
		os.Exit(1)
	}

	// Set our defaults or warn for empty values
	if conf.RegoQuery == "" {
		conf.RegoQuery = "data[_].main"
	}
	if len(conf.Policies) == 0 {
		klog.Warning("no policies set, all evaluations will be futile")
	}
	if len(conf.Objects) == 0 {
		klog.Info("no objects set, watching all discoverable objects")
	}
	if len(conf.IgnoreKinds) == 0 {
		conf.IgnoreKinds = []string{
			"apiservice",
			"endpoint",
			"endpoints",
			"endpointslice",
			"event",
			"flowschema",
			"lease",
			"limitrange",
			"namespace",
			"prioritylevelconfiguration",
			"replicationcontroller",
			"runtimeclass",
		}
	}

	if len(conf.IgnoreDifferingPaths) == 0 {
		conf.IgnoreDifferingPaths = []string{
			"Object/metadata/resourceVersion",
			"Object/metadata/managedFields/0/time",
			"Object/status/observedGeneration",
		}
	} else {
		for i := range conf.IgnoreDifferingPaths {
			conf.IgnoreDifferingPaths[i] = "Object/" + conf.IgnoreDifferingPaths[i]
		}
	}
	return conf
}
