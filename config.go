package main

import (
	"os"

	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/runtime/schema"
	klog "k8s.io/klog/v2"
)

// config outlines which namespace to watch objects in and which objects to watch
type config struct {
	Namespace      string                        `yaml:"namespace, omitempty"`
	Objects        []schema.GroupVersionResource `yaml:"objects, omitempty"`
	IgnoreChildren bool                          `yaml:"ignore_children, omitempty"`
}

// getConfig returns an empty config object
func getConfig() *config {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.SetConfigName(*configPath)
	if err := viper.ReadInConfig(); err != nil {
		klog.ErrorS(err, "unable to read config")
		os.Exit(1)
	}

	conf := &config{}
	// This isn't properly unmarshalled, so we explicitly set it here
	conf.IgnoreChildren = viper.GetBool("ignore_children")
	if err := viper.Unmarshal(conf); err != nil {
		klog.ErrorS(err, "invalid config")
		os.Exit(1)
	}

	klog.Info(conf)

	return conf
}
