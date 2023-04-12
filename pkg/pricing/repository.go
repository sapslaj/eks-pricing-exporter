package pricing

import (
	"context"
	"sync"
	"time"

	"github.com/samber/lo"
	"go.uber.org/multierr"
)

type Repository struct {
	mu                 sync.RWMutex
	pricingProvider    Provider
	onDemandUpdateTime time.Time
	onDemandPrices     OnDemandPriceList
	spotUpdateTime     time.Time
	spotPrices         SpotPriceList
	fargateUpdateTime  time.Time
	fargatePrice       FargatePrice
}

func NewRepository(provider Provider) *Repository {
	return &Repository{
		pricingProvider: provider,
	}
}

func (pr *Repository) UpdateOnDemandPricing(ctx context.Context) error {
	pricing, err := pr.pricingProvider.GetOnDemandPricing(ctx)
	if err != nil {
		return err
	}
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.onDemandPrices = pricing
	pr.onDemandUpdateTime = time.Now()
	return nil
}

func (pr *Repository) UpdateSpotPricing(ctx context.Context) error {
	pricing, err := pr.pricingProvider.GetSpotPricing(ctx)
	if err != nil {
		return err
	}
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.spotPrices = pricing
	pr.spotUpdateTime = time.Now()
	return nil
}

func (pr *Repository) UpdateFargatePricing(ctx context.Context) error {
	pricing, err := pr.pricingProvider.GetFargatePricing(ctx)
	if err != nil {
		return err
	}
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.fargatePrice = pricing
	pr.fargateUpdateTime = time.Now()
	return nil
}

func (pr *Repository) UpdatePricing(ctx context.Context) error {
	var errs []error
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := pr.UpdateOnDemandPricing(ctx)
		if err != nil {
			errs = append(errs, err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := pr.UpdateSpotPricing(ctx)
		if err != nil {
			errs = append(errs, err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := pr.UpdateFargatePricing(ctx)
		if err != nil {
			errs = append(errs, err)
		}
	}()

	if len(errs) != 0 {
		return multierr.Combine(errs...)
	}
	return nil
}

// InstanceTypes returns the list of all instance types for which either a spot or on-demand price is known.
func (pr *Repository) InstanceTypes() []string {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return lo.Union(lo.Keys(pr.onDemandPrices), lo.Keys(pr.spotPrices))
}

// OnDemandLastUpdated returns the time that the on-demand pricing was last updated.
func (pr *Repository) OnDemandLastUpdated() time.Time {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return pr.onDemandUpdateTime
}

// SpotLastUpdated returns the time that the spot pricing was last updated.
func (pr *Repository) SpotLastUpdated() time.Time {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return pr.spotUpdateTime
}

// FargateLastUpdated returns the time that the Fargate pricing was last updated.
func (pr *Repository) FargateLastUpdated() time.Time {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	return pr.fargateUpdateTime
}

// OnDemandPrice returns the last known on-demand price for a given instance type, returning an error if there is no
// known on-demand pricing for the instance type.
func (pr *Repository) OnDemandPrice(instanceType string) (float64, bool) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	price, ok := pr.onDemandPrices[instanceType]
	if !ok {
		return 0.0, false
	}
	return price, true
}

func (pr *Repository) FargatePrice(cpu, memory float64) (float64, bool) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	if pr.fargatePrice.GBPerHour == 0 || pr.fargatePrice.VCPUPerHour == 0 {
		return 0, false
	}
	return cpu*pr.fargatePrice.VCPUPerHour + memory*pr.fargatePrice.GBPerHour, true
}

// SpotPrice returns the last known spot price for a given instance type and zone, returning an error
// if there is no known spot pricing for that instance type or zone.
func (pr *Repository) SpotPrice(instanceType string, zone string) (float64, bool) {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	if _, ok := pr.spotPrices[instanceType]; ok {
		if price, ok := pr.spotPrices[instanceType][zone]; ok {
			return price, true
		}
		return 0.0, false
	}
	return 0.0, false
}
