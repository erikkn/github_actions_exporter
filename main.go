package main

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/common/promlog/flag"
	"github.com/prometheus/common/version"
	"gopkg.in/alecthomas/kingpin.v2"
)

var (
	listenAddress = kingpin.Flag("web.listen-address", "Address to listen on for web interface and telemetry.").Default(":9101").String()
	metricsPath   = kingpin.Flag("web.telemetry-path", "Path under which to expose metrics.").Default("/metrics").String()
	ghWebHookPath = kingpin.Flag("web.gh-webhook-path", "Path under which to expose metrics.").Default("/gh_event").String()
	gitHubToken   = kingpin.Flag("gh.github-webhook-token", "GitHub Webhook Token.").Default("").String()
)

// GHActionExporter struct to hold some information
type GHActionExporter struct {
	Logger log.Logger
}

func init() {
	prometheus.MustRegister(version.NewCollector("ghactions_exporter"))
}

func main() {
	promlogConfig := &promlog.Config{}
	flag.AddFlags(kingpin.CommandLine, promlogConfig)
	kingpin.Version(version.Print("ghactions_exporter"))
	kingpin.HelpFlag.Short('h')
	kingpin.Parse()
	logger := promlog.New(promlogConfig)

	level.Info(logger).Log("msg", "Starting ghactions_exporter", "version", version.Info())
	level.Info(logger).Log("build_context", version.BuildContext())

	gh := NewGHActionExporter(logger)

	srv := http.Server{}

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		level.Info(logger).Log("msg", fmt.Sprintf("Signal received: %v. Exiting...", <-signalChan))
		err := srv.Close()
		if err != nil {
			level.Error(logger).Log("msg", "Error occurred while closing the server", "err", err)
		}
		os.Exit(0)
	}()

	http.Handle(*metricsPath, promhttp.Handler())
	http.HandleFunc(*ghWebHookPath, gh.handleGHWebHook)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html>
<head><title>GitHub Actions Exporter</title></head>
<body>
<h1>GitHub Actions Exporter</h1>
<p> ` + version.Print("ghactions_exporter") + `  </p>
<p><a href='` + *metricsPath + `'>Metrics</a></p>
</body>
</html>
`))
	})

	listener, err := getListener(*listenAddress, logger)
	if err != nil {
		level.Error(logger).Log("msg", "Could not create listener", "err", err)
		os.Exit(1)
	}

	level.Info(logger).Log("msg", "GitHub Actions Prometheus Exporter has successfully started")
	if err := srv.Serve(listener); err != nil {
		level.Error(logger).Log("msg", "Error starting HTTP server", "err", err)
		os.Exit(1)
	}
}

func getListener(listenAddress string, logger log.Logger) (net.Listener, error) {
	var listener net.Listener
	var err error

	if strings.HasPrefix(listenAddress, "unix:") {
		path, _, pathError := parseUnixSocketAddress(listenAddress)
		if pathError != nil {
			return listener, fmt.Errorf("parsing unix domain socket listen address %s failed: %v", listenAddress, pathError)
		}
		listener, err = net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	} else {
		listener, err = net.Listen("tcp", listenAddress)
	}

	if err != nil {
		return listener, err
	}

	level.Info(logger).Log("msg", fmt.Sprintf("Listening on %s", listenAddress))
	return listener, nil
}

func parseUnixSocketAddress(address string) (string, string, error) {
	addressParts := strings.Split(address, ":")
	addressPartsLength := len(addressParts)

	if addressPartsLength > 3 || addressPartsLength < 1 {
		return "", "", fmt.Errorf("address for unix domain socket has wrong format")
	}

	unixSocketPath := addressParts[1]
	requestPath := ""
	if addressPartsLength == 3 {
		requestPath = addressParts[2]
	}

	return unixSocketPath, requestPath, nil
}

func validateFlags(token, org, repo string) error {
	if token == "" {
		return errors.New("Please configure the GitHub Token")
	}

	if org == "" {
		return errors.New("Please configure the GitHub Organization")
	}

	if repo == "" {
		return errors.New("Please configure the GitHub Repository")
	}

	return nil
}

func NewGHActionExporter(logger log.Logger) *GHActionExporter {
	return &GHActionExporter{
		Logger: logger,
	}
}
