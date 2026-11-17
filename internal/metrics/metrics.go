// Package metrics exposes Prometheus metrics in the text exposition format,
// without a client library: request counts and latencies per route, plus
// gauges read at scrape time.
package metrics

import (
	"bufio"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var buckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

type reqKey struct {
	route, method, class string
}

type histogram struct {
	counts []uint64 // per bucket, cumulative at write time
	sum    float64
	n      uint64
}

var (
	mu       sync.Mutex
	requests = map[reqKey]uint64{}
	latency  = map[reqKey]*histogram{}
)

// statusWriter records the status code. It forwards Flush so server-sent
// events keep streaming through it.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Instrument counts requests to route (the registered pattern, never the raw
// path, so ids don't explode the label set) and times them.
func Instrument(route string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w}
		start := time.Now()
		next(sw, r)
		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		observe(reqKey{route: route, method: r.Method, class: strconv.Itoa(status/100) + "xx"}, time.Since(start).Seconds())
	}
}

func observe(k reqKey, seconds float64) {
	mu.Lock()
	defer mu.Unlock()
	requests[k]++
	h := latency[k]
	if h == nil {
		h = &histogram{counts: make([]uint64, len(buckets))}
		latency[k] = h
	}
	for i, b := range buckets {
		if seconds <= b {
			h.counts[i]++
		}
	}
	h.sum += seconds
	h.n++
}

// Gauge is one sample read at scrape time.
type Gauge struct {
	Name   string
	Help   string
	Labels map[string]string
	Value  float64
}

func labelString(l map[string]string) string {
	if len(l) == 0 {
		return ""
	}
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		v := strings.NewReplacer(`\`, `\`, `"`, `\"`, "\n", `\n`).Replace(l[k])
		parts = append(parts, fmt.Sprintf(`%s="%s"`, k, v))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

// Write renders the request metrics and gauges.
func Write(w http.ResponseWriter, gauges []Gauge) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	bw := bufio.NewWriter(w)
	defer bw.Flush()

	mu.Lock()
	keys := make([]reqKey, 0, len(requests))
	for k := range requests {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.route != b.route {
			return a.route < b.route
		}
		if a.method != b.method {
			return a.method < b.method
		}
		return a.class < b.class
	})
	fmt.Fprintln(bw, "# HELP ordryn_http_requests_total API requests by route, method, and status class.")
	fmt.Fprintln(bw, "# TYPE ordryn_http_requests_total counter")
	for _, k := range keys {
		fmt.Fprintf(bw, "ordryn_http_requests_total%s %d\n",
			labelString(map[string]string{"route": k.route, "method": k.method, "status": k.class}), requests[k])
	}
	fmt.Fprintln(bw, "# HELP ordryn_http_request_duration_seconds API request latency.")
	fmt.Fprintln(bw, "# TYPE ordryn_http_request_duration_seconds histogram")
	for _, k := range keys {
		h := latency[k]
		base := map[string]string{"route": k.route, "method": k.method, "status": k.class}
		for i, b := range buckets {
			l := map[string]string{"le": strconv.FormatFloat(b, 'f', -1, 64)}
			for kk, vv := range base {
				l[kk] = vv
			}
			fmt.Fprintf(bw, "ordryn_http_request_duration_seconds_bucket%s %d\n", labelString(l), h.counts[i])
		}
		inf := map[string]string{"le": "+Inf"}
		for kk, vv := range base {
			inf[kk] = vv
		}
		fmt.Fprintf(bw, "ordryn_http_request_duration_seconds_bucket%s %d\n", labelString(inf), h.n)
		fmt.Fprintf(bw, "ordryn_http_request_duration_seconds_sum%s %g\n", labelString(base), h.sum)
		fmt.Fprintf(bw, "ordryn_http_request_duration_seconds_count%s %d\n", labelString(base), h.n)
	}
	mu.Unlock()

	seen := map[string]bool{}
	for _, g := range gauges {
		if !seen[g.Name] {
			seen[g.Name] = true
			fmt.Fprintf(bw, "# HELP %s %s\n# TYPE %s gauge\n", g.Name, g.Help, g.Name)
		}
		fmt.Fprintf(bw, "%s%s %g\n", g.Name, labelString(g.Labels), g.Value)
	}
}

// reset clears recorded requests (tests).
func reset() {
	mu.Lock()
	requests = map[reqKey]uint64{}
	latency = map[reqKey]*histogram{}
	mu.Unlock()
}
