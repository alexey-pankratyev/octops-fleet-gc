package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	v1 "agones.dev/agones/pkg/apis/agones/v1"
	autoscaling "agones.dev/agones/pkg/apis/autoscaling/v1"
	"github.com/alecthomas/kingpin"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"github.com/Octops/agones-event-broadcaster/pkg/broadcaster"
	"github.com/Octops/octops-fleet-gc/internal/version"
	"github.com/Octops/octops-fleet-gc/pkg/collector"
)

var (
	debug         = kingpin.Flag("debug", "Debug mode.").Bool()
	kubeconfig    = kingpin.Flag("kubeconfig", "Path for the kukeconfig file. Only required for development.").Envar("KUBECONFIG").String()
	syncPeriod    = kingpin.Flag("sync-period", "Sync period interval. I.e: 15s, 1m, 2h.").Default("15s").Duration()
	maxConcurrent = kingpin.Flag("max-concurrent", "Maximum number of concurrent Reconciles which can be run.").Default("5").Int()
)


func main() {
	kingpin.Parse()
	logger := log.NewJSONLogger(os.Stdout)
	logger = log.With(logger, "ts", log.DefaultTimestampUTC, "caller", log.DefaultCaller)

	if *debug {
		logger = level.NewFilter(logger, level.AllowDebug())
	}

	level.Info(logger).Log("msg", version.Info())

	ctx, done := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer done()

	cfg, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	exitIfErr(logger, err)

	fleetGC, err := collector.NewFleetCollector(ctx, logger, cfg, *syncPeriod)
	exitIfErr(logger, err)

	opts := &broadcaster.Config{
		SyncPeriod:             *syncPeriod,
		ServerPort:             8090,
		MetricsBindAddress:     "0.0.0.0:8095",
		MaxConcurrentReconcile: *maxConcurrent,
		// HealthProbeBindAddress: "0.0.0.0:8099",
	}

	bc := broadcaster.New(cfg, fleetGC, opts)

    if err := bc.Manager.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		level.Error(logger).Log("unable to set up health check", err)
        os.Exit(1)
    }

    if err := bc.Manager.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		level.Error(logger).Log("unable to set up ready check", err)
        os.Exit(1)
    }
	
	bc.WithWatcherFor(&v1.Fleet{})
	bc.WithWatcherFor(&autoscaling.FleetAutoscaler{})

	err = bc.Build()
	exitIfErr(logger, err)

	level.Info(logger).Log("msg", "starting fleet garbage collector")
	if err := bc.Start(ctx); err != nil {
		exitIfErr(logger, err)
	}
}

func exitIfErr(logger log.Logger, err error) {
	if err != nil {
		level.Error(logger).Log("err", err)
		os.Exit(1)
	}
}
