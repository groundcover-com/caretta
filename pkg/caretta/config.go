package caretta

import (
	"fmt"
	"os"
	"strconv"
)

const (
	defaultPrometheusEndpoint     = "/metrics"
	defaultPrometheusPort         = ":7117"
	defaultPollingIntervalSeconds = 5
	defaultShouldResolveDns       = false
)

type carettaConfig struct {
	shouldResolveDns       bool
	prometheusPort         string
	prometheusEndpoint     string
	pollingIntervalSeconds int
}

// environment variables based, encapsulated to enable future changes
func readConfig() carettaConfig {
	port := defaultPrometheusPort
	if val := os.Getenv("PROMETHEUS_PORT"); val != "" {
		valInt, err := strconv.Atoi(val)
		if err == nil {
			port = fmt.Sprintf(":%d", valInt)
		}
	}

	endpoint := defaultPrometheusEndpoint
	if val := os.Getenv("PROMETHEUS_ENDPOINT"); val != "" {
		endpoint = val
	}

	interval := defaultPollingIntervalSeconds
	if val := os.Getenv("POLL_INTERVAL"); val != "" {
		valInt, err := strconv.Atoi(val)
		if err == nil {
			interval = valInt
		}
	}

	shouldResolveDns := defaultShouldResolveDns
	if val := os.Getenv("RESOLVE_DNS"); val != "" {
		valBool, err := strconv.ParseBool(val)
		if err == nil {
			shouldResolveDns = valBool
		}
	}

	return carettaConfig{
		shouldResolveDns:       shouldResolveDns,
		prometheusPort:         port,
		prometheusEndpoint:     endpoint,
		pollingIntervalSeconds: interval,
	}
}
