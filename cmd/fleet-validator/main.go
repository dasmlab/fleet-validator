// fleet-validator runs on an ACM hub and continuously validates the production readiness of
// the hub (local-cluster) and of every joined ManagedCluster, exporting the checklist as
// Prometheus metrics, a JSON API and a small UI.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/logr/funcr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/dasmlab/fleet-validator/internal/checks"
	"github.com/dasmlab/fleet-validator/internal/config"
	"github.com/dasmlab/fleet-validator/internal/httpserver"
	"github.com/dasmlab/fleet-validator/internal/metrics"
	"github.com/dasmlab/fleet-validator/internal/promq"
	"github.com/dasmlab/fleet-validator/internal/runner"
)

var version = "dev"

const saTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"

func main() {
	var (
		cfgPath, httpAddr, uiAddr, kubeconfig, kubeContext, promURL, promCAs string
		once, noProm                                                         bool
		verbosity                                                            int
	)
	flag.StringVar(&cfgPath, "config", "", "Path to the YAML config (optional).")
	flag.StringVar(&httpAddr, "http-bind-address", ":8090", "Listener for /metrics, /healthz, /readyz "+
		"(and the UI/API unless --ui-bind-address is set).")
	flag.StringVar(&uiAddr, "ui-bind-address", "", "Separate UI/API listener, e.g. 127.0.0.1:8091 behind oauth-proxy.")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Kubeconfig for out-of-cluster runs (default: in-cluster, then $KUBECONFIG).")
	flag.StringVar(&kubeContext, "context", "", "Kubeconfig context for out-of-cluster runs.")
	flag.StringVar(&promURL, "prometheus-url", "", "Thanos querier URL (overrides the config).")
	flag.StringVar(&promCAs, "prometheus-ca", "/etc/pki/fleet-validator/service-ca/service-ca.crt",
		"Comma-separated extra CA bundles for the Prometheus TLS connection.")
	flag.BoolVar(&noProm, "no-prometheus", false, "Disable metric-based checks (etcd DB size, API latency).")
	flag.BoolVar(&once, "once", false, "Validate every cluster once, print the reports as JSON and exit.")
	flag.IntVar(&verbosity, "v", 0, "Log verbosity.")
	printMetrics := flag.Bool("print-metric-names", false, "Print the exported metric names (MCO allowlist) and exit.")
	flag.Parse()

	if *printMetrics {
		for _, n := range metrics.Names {
			_, _ = os.Stdout.WriteString(n + "\n")
		}
		return
	}

	log := funcr.NewJSON(func(obj string) { _, _ = os.Stderr.WriteString(obj + "\n") },
		funcr.Options{Verbosity: verbosity})

	cfg, err := config.Load(cfgPath)
	if err != nil {
		fatal(log, err, "loading config")
	}
	if promURL != "" {
		cfg.PrometheusURL = promURL
	}

	restCfg, inCluster, err := kubeConfig(kubeconfig, kubeContext)
	if err != nil {
		fatal(log, err, "loading kubeconfig")
	}
	restCfg.QPS, restCfg.Burst = 20, 40
	restCfg.UserAgent = "fleet-validator/" + version

	env := &checks.Env{Cfg: cfg}
	if env.Dyn, err = dynamic.NewForConfig(restCfg); err != nil {
		fatal(log, err, "dynamic client")
	}
	if env.Kube, err = kubernetes.NewForConfig(restCfg); err != nil {
		fatal(log, err, "kube client")
	}
	// Out of cluster there is no SA token to reach the in-cluster Thanos querier.
	if !noProm && inCluster {
		if env.Prom, err = promq.New(cfg.PrometheusURL, saTokenFile, strings.Split(promCAs, ","), 5*time.Minute); err != nil {
			fatal(log, err, "prometheus client")
		}
	}

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	exp := metrics.New(reg, version)
	run := runner.New(env, exp, log.WithName("runner"))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if once {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(run.Once(ctx))
		return
	}

	srv := &httpserver.Server{Runner: run, Reg: reg, Version: version}
	if uiAddr == "" {
		go serve(log, "all", httpAddr, srv.AllMux())
	} else {
		go serve(log, "metrics", httpAddr, srv.MetricsMux())
		go serve(log, "ui", uiAddr, srv.UIMux())
	}

	log.Info("starting", "version", version, "interval", cfg.Interval, "concurrency", cfg.Concurrency,
		"inCluster", inCluster, "prometheus", env.Prom != nil)
	run.Run(ctx)
}

func kubeConfig(path, kubeContext string) (*rest.Config, bool, error) {
	if path == "" && kubeContext == "" {
		if c, err := rest.InClusterConfig(); err == nil {
			return c, true, nil
		}
	}
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	if path != "" {
		rules.ExplicitPath = path
	}
	c, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules,
		&clientcmd.ConfigOverrides{CurrentContext: kubeContext}).ClientConfig()
	return c, false, err
}

func serve(log logr.Logger, name, addr string, h http.Handler) {
	log.Info("listening", "server", name, "addr", addr)
	s := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 10 * time.Second}
	if err := s.ListenAndServe(); err != nil {
		fatal(log, err, "http server stopped", "server", name)
	}
}

func fatal(log logr.Logger, err error, msg string, kv ...any) {
	log.Error(err, msg, kv...)
	os.Exit(1)
}
