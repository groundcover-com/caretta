package caretta

import (
	"hash/fnv"
	"log"
	"strconv"
	"time"

	"github.com/groundcover-com/caretta/pkg/k8s"
	"github.com/groundcover-com/caretta/pkg/metrics"
	"github.com/groundcover-com/caretta/pkg/tracing"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	PROMETHEUS_ENDPOINT   = "/metrics"
	PROMETHEUS_PORT       = ":7117"
	POLLING_INTERVAL_SECS = 5
)

var (
	linksMetrics = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "caretta_links_observed",
		Help: "total bytes_sent value of links observed by caretta since its launch",
	}, []string{
		"LinkId", "ClientId", "ClientName", "ClientNamespace", "ServerId", "ServerName", "ServerNamespace", "ServerPort", "Role",
	})
)

// simple hash function from string to uint32
func hash(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

type Caretta struct {
	StopSignal    chan bool
	tracerObjects tracing.TracerEbpfObjects
}

func NewCaretta() *Caretta {
	return &Caretta{
		StopSignal: make(chan bool),
	}
}

func (caretta *Caretta) Start() {
	metrics.StartMetricsServer(PROMETHEUS_ENDPOINT, PROMETHEUS_PORT)
	caretta.tracerObjects = tracing.LoadProbes()

	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatal(err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal(err)
	}

	resolver := k8s.NewIPResolver(clientset)
	pollingTicker := time.NewTicker(POLLING_INTERVAL_SECS * time.Second)

	pastLinks := make(map[tracing.NetworkLink]uint64)

	go func() {
		for {
			select {
			case <-caretta.StopSignal:
				return
			case <-pollingTicker.C:
				var links map[tracing.NetworkLink]uint64

				resolver.UpdateIPResolver()

				pastLinks, links = tracing.TracesPollingIteration(caretta.tracerObjects.BpfObjs, pastLinks, *resolver)
				for link, throughput := range links {
					clientName, clientNamespace := k8s.SplitNamespace(link.ClientHost)
					serverName, serverNamespace := k8s.SplitNamespace(link.ServerHost)
					linksMetrics.With(prometheus.Labels{
						"LinkId":          strconv.Itoa(int(hash(link.ClientHost+link.ServerHost) + link.Role)),
						"ClientId":        strconv.Itoa(int(hash(link.ClientHost))),
						"ClientName":      clientName,
						"ClientNamespace": clientNamespace,
						"ServerId":        strconv.Itoa(int(hash(link.ServerHost))),
						"ServerName":      serverName,
						"ServerNamespace": serverNamespace,
						"ServerPort":      strconv.Itoa(int(link.ServerPort)),
						"Role":            strconv.Itoa(int(link.Role)),
					}).Set(float64(throughput))
				}
			}
		}
	}()
}

func (caretta *Caretta) Stop() {
	log.Print("Stopping Caretta...")
	caretta.StopSignal <- true
}
