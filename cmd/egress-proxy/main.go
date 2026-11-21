// Command egress-proxy runs the tier egress proxy: see internal/egressproxy
// and docs/guides/egress-proxy.md.
package main

import (
	"context"
	"errors"
	"flag"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/crewlet/crewlet-operator/internal/egressproxy"
)

func main() {
	opts := zap.Options{}
	opts.BindFlags(flag.CommandLine)
	if err := run(&opts); err != nil {
		ctrl.Log.Error(err, "Egress proxy stopped")
		os.Exit(1)
	}
}

func run(opts *zap.Options) error {
	var configPath, keyPath, listen, healthListen, kubeconfig string
	var dialTimeout, shutdownTimeout time.Duration
	flag.StringVar(&configPath, "config", "/etc/egress-proxy/config.yaml", "The tier policy file.")
	flag.StringVar(&keyPath, "signing-key-file", "/var/run/egress-proxy/signing-key",
		"The file holding the key the tenants' credentials are signed with.")
	flag.StringVar(&listen, "listen", ":3128", "The address the proxy listens on.")
	flag.StringVar(&healthListen, "health-listen", ":8081", "The address /healthz is served on.")
	flag.StringVar(&kubeconfig, "kubeconfig", "", "A kubeconfig, outside a cluster; the pod's own identity inside one.")
	flag.DurationVar(&dialTimeout, "dial-timeout", 10*time.Second,
		"The bound on resolving and connecting to a destination.")
	flag.DurationVar(&shutdownTimeout, "shutdown-timeout", 30*time.Second,
		"How long open requests are given to finish when the proxy is stopped.")
	flag.Parse()
	// Operational messages go to stderr; stdout carries the access log alone.
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(opts), zap.WriteTo(os.Stderr)))

	policy, err := egressproxy.LoadConfig(configPath)
	if err != nil {
		return err
	}
	keys, err := egressproxy.NewKeyFile(keyPath, 30*time.Second)
	if err != nil {
		return err
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}
	dir, err := egressproxy.NewKubeDirectory(cs)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go dir.Run(ctx)

	dialer := &net.Dialer{KeepAlive: 30 * time.Second}
	proxy := &http.Server{
		Addr: listen,
		Handler: &egressproxy.Proxy{
			Policy:      policy,
			Directory:   dir,
			Keys:        keys,
			Resolver:    net.DefaultResolver,
			Dial:        dialer.DialContext,
			Log:         egressproxy.NewAccessLog(os.Stdout),
			DialTimeout: dialTimeout,
		},
		ReadHeaderTimeout: 10 * time.Second,
	}
	mux := http.NewServeMux()
	mux.Handle("/healthz", egressproxy.HealthHandler(dir.Synced))
	health := &http.Server{Addr: healthListen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	errs := make(chan error, 2)
	for _, srv := range []*http.Server{proxy, health} {
		go func() {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errs <- err
			}
		}()
	}
	ctrl.Log.Info("Egress proxy listening", "listen", listen, "health", healthListen)
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	// Hijacked tunnels are not tracked by Shutdown; they end with the
	// process, and a client reconnects through another replica.
	shutdown, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	_ = health.Shutdown(shutdown)
	return proxy.Shutdown(shutdown)
}
