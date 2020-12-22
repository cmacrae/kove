package main

import (
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog"
)

type Config struct {
	Objects []schema.GroupVersionResource `yaml:"objects"`
}

func getConfig() *Config {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.SetConfigName(*configPath)
	if err := viper.ReadInConfig(); err != nil {
		klog.Fatalf("unable to read config: %s", err.Error())
	}

	conf := &Config{}
	if err := viper.Unmarshal(conf); err != nil {
		klog.Fatalf("invalid config: %s", err.Error())
	}

	return conf
}
