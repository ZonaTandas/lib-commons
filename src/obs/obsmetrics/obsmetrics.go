// Package obsmetrics aporta las métricas HTTP RED estándar de todos los
// servicios (las métricas de dominio de cada servicio siguen donde estaban):
//
//	http_requests_total{route,method,status}
//	http_request_duration_seconds{route,method}
//
// route es SIEMPRE el template de gorilla/mux (/bookings/{id}), jamás la URL
// real: la cardinalidad queda finita.
//
// Uso: router.Use(obs.Middleware, obsmetrics.Middleware) y
// router.Handle("/metrics", internalapi.ServiceToken(obsmetrics.Handler())).
package obsmetrics

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ZonaTandas/lib-commons/src/obs"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	requestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "Peticiones HTTP servidas, por template de ruta, método y status.",
	}, []string{"route", "method", "status"})

	requestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "Duración de las peticiones HTTP por template de ruta y método.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route", "method"})

	internalAuthTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "internal_auth_requests_total",
		Help: "Resultado de la verificación X-Internal-Auth por template de ruta (evolutivo 2026-07-red-interna-auth). result: ok_secret|ok_token|missing|invalid|expired|unconfigured|exempt.",
	}, []string{"result", "route"})
)

// El counter se engancha a obs.RequireInternalAuth vía el observer: obsmetrics
// ya se importa en todos los servicios (router.Use), así que basta el init.
// En Grafana, la fase 1 del rollout se vigila con
// sum by (result) (rate(internal_auth_requests_total[5m])).
func init() {
	obs.SetInternalAuthObserver(func(result, route string) {
		internalAuthTotal.WithLabelValues(result, route).Inc()
	})
}

// Middleware instrumenta cada request. Salta /health y /metrics.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)

		route := obs.RouteTemplate(r)
		requestsTotal.WithLabelValues(route, r.Method, strconv.Itoa(rec.status)).Inc()
		requestDuration.WithLabelValues(route, r.Method).Observe(time.Since(start).Seconds())
	})
}

// Handler expone el registry por defecto (promhttp). Envolver con el
// internalapi.ServiceToken existente: el /metrics queda protegido con el
// Bearer AUTH_SERVICE_TOKEN igual que en booking/payment-core.
func Handler() http.Handler {
	return promhttp.Handler()
}

// TokenHandler es Handler() ya protegido con el Bearer AUTH_SERVICE_TOKEN
// (misma semántica que los ServiceToken del ecosistema: env vacía = cerrado).
// Para los servicios que no tienen middleware ServiceToken propio.
func TokenHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		expected := os.Getenv("AUTH_SERVICE_TOKEN")
		if expected == "" || token != expected {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"message":"Invalid service token"}`))
			return
		}
		promhttp.Handler().ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status = status
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if !r.wroteHeader {
		r.wroteHeader = true
	}
	return r.ResponseWriter.Write(p)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := r.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("obsmetrics: el ResponseWriter subyacente no soporta Hijack")
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
