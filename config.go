package main

import (
	"os"

	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/runtime/schema"
	klog "k8s.io/klog/v2"
)

type Config struct {
	Namespace string                        `yaml:"namespace, omitempty"`
	Objects   []schema.GroupVersionResource `yaml:"objects, omitempty"`
}

func getConfig() *Config {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.SetConfigName(*configPath)
	if err := viper.ReadInConfig(); err != nil {
		klog.ErrorS(err, "unable to read config")
		os.Exit(1)
	}

	conf := &Config{}
	if err := viper.Unmarshal(conf); err != nil {
		klog.ErrorS(err, "invalid config")
		os.Exit(1)
	}

	return conf
}
