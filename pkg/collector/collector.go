package collector

import (
	"context"
	"log"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/kubernetes"

	"github.com/sapslaj/eks-pricing-exporter/pkg/model"
	"github.com/sapslaj/eks-pricing-exporter/pkg/pricing"
)

type collectorMetricDesc struct {
	nodeInfo    *prometheus.Desc
	hourlyPrice *prometheus.Desc
}

type Collector struct {
	metricDesc        collectorMetricDesc
	parentCtx         context.Context
	cs                *kubernetes.Clientset
	pricingRepository *pricing.Repository
}

func NewCollector(ctx context.Context, cs *kubernetes.Clientset, pricingRepository *pricing.Repository) *Collector {
	namespace := "eksnode"
	return &Collector{
		parentCtx:         ctx,
		cs:                cs,
		pricingRepository: pricingRepository,
		metricDesc: collectorMetricDesc{
			nodeInfo: prometheus.NewDesc(
				prometheus.BuildFQName(namespace, "node", "info"),
				"info labels about the node",
				[]string{"node", "capacity_type", "instance_type", "zone", "region"},
				nil,
			),
			hourlyPrice: prometheus.NewDesc(
				prometheus.BuildFQName(namespace, "node", "hourly_price"),
				"hourly price of node",
				[]string{"node", "capacity_type", "instance_type", "zone", "region"},
				nil,
			),
		},
	}
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.metricDesc.hourlyPrice
	ch <- c.metricDesc.nodeInfo
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(c.parentCtx, 5*time.Minute)
	defer cancel()

	cluster := model.NewCluster()
	err := cluster.Populate(ctx, c.cs)
	if err != nil {
		log.Fatalf("getting cluster information failed: %s", err)
	}

	cluster.ForEachNode(func(node *model.Node) {
		node.UpdatePrice(c.pricingRepository)

		ch <- prometheus.MustNewConstMetric(
			c.metricDesc.nodeInfo,
			prometheus.GaugeValue,
			1.0,
			node.Name(),                  // "node"
			node.CapacityType().String(), // "capacity_type"
			node.InstanceType(),          // "instance_type"
			node.Zone(),                  // "zone"
			node.Region(),                // "region"
		)
		ch <- prometheus.MustNewConstMetric(
			c.metricDesc.hourlyPrice,
			prometheus.GaugeValue,
			node.Price,
			node.Name(),                  // "node"
			node.CapacityType().String(), // "capacity_type"
			node.InstanceType(),          // "instance_type"
			node.Zone(),                  // "zone"
			node.Region(),                // "region"
		)
	})
}
