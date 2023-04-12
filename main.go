package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/sapslaj/eks-pricing-exporter/pkg/collector"
	"github.com/sapslaj/eks-pricing-exporter/pkg/pricing"
)

func main() {
	port := flag.Int("port", 9523, "port to run exporter on")

	flag.Parse()

	ctx, cancel := context.WithCancel(context.Background())
	go handleSigterm(cancel)

	cs := kubernetes.NewForConfigOrDie(ctrl.GetConfigOrDie())
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("loading aws config: %s", err)
	}

	pricingProvider := pricing.NewAWSProvider(cfg)
	// sanity check
	_, err = pricingProvider.GetFargatePricing(ctx)
	if err != nil {
		log.Fatalf("could not load AWS pricing data: %s", err)
	}
	pricingRepository := pricing.NewRepository(pricingProvider)
	log.Printf("updating pricing...")
	err = pricingRepository.UpdatePricing(ctx)
	if err != nil {
		log.Fatalf("could not update pricing repository: %s", err)
	}

	prometheus.MustRegister(collector.NewCollector(ctx, cs, pricingRepository))

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/admin/pricing/update", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintln(w, "Only POST method is allowed on this endpoint.")
			return
		}
		log.Println("updating pricing via /admin/pricing/update")
		err := pricingRepository.UpdatePricing(r.Context())
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "error updating pricing: %s", err)
			return
		}
		fmt.Fprintln(w, "success")
	})

	addr := fmt.Sprintf(":%d", *port)

	server := &http.Server{
		Addr:        addr,
		Handler:     mux,
		BaseContext: func(_ net.Listener) context.Context { return ctx },
		ReadTimeout: time.Minute,
	}

	log.Printf("Starting eks-pricing-exporter/%s on %s", VERSION, addr)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.Tick(1 * time.Hour):
				log.Println("updating pricing on schedule")
				err := pricingRepository.UpdatePricing(ctx)
				if err != nil {
					log.Fatalf("could not update pricing repository: %s", err)
				}
			}
		}
	}()

	err = server.ListenAndServe()
	if !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("error running server: %s", err)
	}
}

func handleSigterm(cancel func()) {
	signals := make(chan os.Signal, 1)
	signal.Notify(
		signals,
		syscall.SIGHUP,
		syscall.SIGQUIT,
		syscall.SIGTERM,
	)
	<-signals
	log.Println("Received SIGTERM. Terminating.")
	cancel()
}
