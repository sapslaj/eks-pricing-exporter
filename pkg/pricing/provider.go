package pricing

import (
	"context"
)

// OnDemandPriceList is a map of instance type to on-demand price.
type OnDemandPriceList map[string]float64

// SpotPriceList is a map of instance type and zone to spot price.
type SpotPriceList map[string]map[string]float64

// FargatePrice is the price for Fargate.
type FargatePrice struct {
	VCPUPerHour float64
	GBPerHour   float64
}

// Provider is the interface used for provider implementation.
type Provider interface {
	GetOnDemandPricing(context.Context) (OnDemandPriceList, error)
	GetSpotPricing(context.Context) (SpotPriceList, error)
	GetFargatePricing(context.Context) (FargatePrice, error)
}
