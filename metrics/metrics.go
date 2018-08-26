package metrics

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"
)

const namespace = "inverter"

type Metrics struct {
	http.Server
	log *logrus.Entry

	registry      *prometheus.Registry
	Yield         *prometheus.CounterVec
	TimeOnStatus  *prometheus.CounterVec
	GridVoltage   *prometheus.SummaryVec
	GridPower     *prometheus.SummaryVec
	GridFrequency *prometheus.SummaryVec
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func newLoggingResponseWriter(w http.ResponseWriter) *loggingResponseWriter {
	return &loggingResponseWriter{w, http.StatusOK}
}

func (m *Metrics) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		entry := m.log
		start := time.Now()

		if reqID := r.Header.Get("X-Request-Id"); reqID != "" {
			entry = entry.WithField("requestId", reqID)
		}

		entry = entry.WithFields(logrus.Fields{
			"remoteAddr": r.RemoteAddr,
			"method":     r.Method,
			"request":    r.RequestURI,
			"userAgent":  r.UserAgent(),
		})

		lw := newLoggingResponseWriter(w)
		next.ServeHTTP(lw, r)

		latency := time.Since(start)

		entry.WithFields(logrus.Fields{
			"status": lw.statusCode,
			"took":   latency,
		}).Debug("completed handling request")
	})
}

func New(log *logrus.Entry) *Metrics {

	router := mux.NewRouter()

	// Create server and register prometheus metrics handler
	m := &Metrics{
		log: log.WithField("module", "metrics"),
		Server: http.Server{
			Handler: router,
		},
		Yield: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "yield_total",
				Help:      "Total yield of the inverter in Wh.",
			},
			[]string{"serial"},
		),
		TimeOnStatus: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Namespace: namespace,
				Name:      "time_on_status_total",
				Help:      "Time spend on status in seconds.",
			},
			[]string{"serial", "status"},
		),
		GridVoltage: prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Namespace: namespace,
				Name:      "grid_voltage",
				Help:      "Voltage of the grid in Volts.",
			},
			[]string{"serial"},
		),
		GridPower: prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Namespace: namespace,
				Name:      "grid_power",
				Help:      "Power delivered into the grid in Watts.",
			},
			[]string{"serial"},
		),
		GridFrequency: prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Namespace: namespace,
				Name:      "grid_frequency",
				Help:      "Frequency of the grid in Hertz.",
			},
			[]string{"serial"},
		),
		registry: prometheus.NewRegistry(),
	}

	m.registry.MustRegister(m.Yield)
	m.registry.MustRegister(m.TimeOnStatus)
	m.registry.MustRegister(m.GridVoltage)
	m.registry.MustRegister(m.GridPower)
	m.registry.MustRegister(m.GridFrequency)
	m.registry.MustRegister(prometheus.NewProcessCollector(os.Getpid(), ""))
	m.registry.MustRegister(prometheus.NewGoCollector())

	router.Handle("/metrics", promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{}))

	// setup logging
	router.Use(m.loggingMiddleware)

	return m
}

func (m *Metrics) Start(stopCh chan struct{}) {
	go func() {
		m.log.Infof("metrics server listening on http://%s", m.Addr)
		if err := m.ListenAndServe(); err != nil {
			if err.Error() != "http: Server closed" {
				m.log.Fatalf("error binding metrics server: %s", err.Error())
			}
		}
		m.log.Info("metric server stopped")
	}()

	<-stopCh
	m.log.Info("stopping metrics server")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := m.Shutdown(ctx); err != nil {
		m.log.Errorf("error during metrics server shutdown: %s", err)
	}
}
