package pricing

import (
	"context"
)

type StaticProvider struct{}

func NewStaticProvider() *StaticProvider {
	return &StaticProvider{}
}

func (p *StaticProvider) GetOnDemandPricing(_ context.Context) (OnDemandPriceList, error) {
	return initialOnDemandPrices, nil
}

func (p *StaticProvider) GetSpotPricing(_ context.Context) (SpotPriceList, error) {
	return make(SpotPriceList), nil
}

func (p *StaticProvider) GetFargatePricing(_ context.Context) (FargatePrice, error) {
	return FargatePrice{}, nil
}
