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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/crewlet/crewlet-operator/internal/egressproxy"
)

func main() {
	o := bindFlags(flag.CommandLine)
	flag.Parse()
	if err := run(o); err != nil {
		ctrl.Log.Error(err, "Egress proxy stopped")
		os.Exit(1)
	}
}

// options are the command's flags. --kubeconfig is not among them:
// controller-runtime registers it on the default FlagSet, and ctrl.GetConfig
// reads it (outside a cluster) or the pod's own identity (inside one).
type options struct {
	zap                          zap.Options
	configPath, keyPath          string
	listen, healthListen         string
	dialTimeout, shutdownTimeout time.Duration
}

// bindFlags registers the command's flags on fs.
func bindFlags(fs *flag.FlagSet) *options {
	o := &options{}
	o.zap.BindFlags(fs)
	fs.StringVar(&o.configPath, "config", "/etc/egress-proxy/config.yaml", "The tier policy file.")
	fs.StringVar(&o.keyPath, "signing-key-file", "/var/run/egress-proxy/signing-key",
		"The file holding the key the tenants' credentials are signed with.")
	fs.StringVar(&o.listen, "listen", ":3128", "The address the proxy listens on.")
	fs.StringVar(&o.healthListen, "health-listen", ":8081", "The address /healthz is served on.")
	fs.DurationVar(&o.dialTimeout, "dial-timeout", 10*time.Second,
		"The bound on resolving and connecting to a destination.")
	fs.DurationVar(&o.shutdownTimeout, "shutdown-timeout", 30*time.Second,
		"How long open requests are given to finish when the proxy is stopped.")
	return o
}

func run(o *options) error {
	// Operational messages go to stderr; stdout carries the access log alone.
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&o.zap), zap.WriteTo(os.Stderr)))

	policy, err := egressproxy.LoadConfig(o.configPath)
	if err != nil {
		return err
	}
	keys, err := egressproxy.NewKeyFile(o.keyPath, 30*time.Second)
	if err != nil {
		return err
	}
	cfg, err := ctrl.GetConfig()
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
	proxy := egressproxy.NewServer(o.listen, &egressproxy.Proxy{
		Policy:      policy,
		Directory:   dir,
		Keys:        keys,
		Resolver:    net.DefaultResolver,
		Dial:        dialer.DialContext,
		Log:         egressproxy.NewAccessLog(os.Stdout),
		DialTimeout: o.dialTimeout,
	})
	mux := http.NewServeMux()
	mux.Handle("/healthz", egressproxy.HealthHandler(dir.Synced))
	health := &http.Server{Addr: o.healthListen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}

	errs := make(chan error, 2)
	for _, srv := range []*http.Server{proxy, health} {
		go func() {
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				errs <- err
			}
		}()
	}
	ctrl.Log.Info("Egress proxy listening", "listen", o.listen, "health", o.healthListen)
	select {
	case err := <-errs:
		return err
	case <-ctx.Done():
	}
	// Hijacked tunnels are not tracked by Shutdown; they end with the
	// process, and a client reconnects through another replica.
	shutdown, cancel := context.WithTimeout(context.Background(), o.shutdownTimeout)
	defer cancel()
	_ = health.Shutdown(shutdown)
	return proxy.Shutdown(shutdown)
}
