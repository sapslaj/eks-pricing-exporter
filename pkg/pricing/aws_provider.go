package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/pricing"
	pricingtypes "github.com/aws/aws-sdk-go-v2/service/pricing/types"
	"github.com/samber/lo"
)

type AWSProvider struct {
	Region        string
	EC2Client     ec2.DescribeSpotPriceHistoryAPIClient
	PricingClient pricing.GetProductsAPIClient
}

// NewAWSPricingClient returns a pricing API client configured based on a particular region.
func NewAWSPricingClient(cfg aws.Config, region string) *pricing.Client {
	// pricing API doesn't have an endpoint in all regions
	pricingAPIRegion := "us-east-1"
	if strings.HasPrefix(region, "ap-") {
		pricingAPIRegion = "ap-south-1"
	}
	return pricing.NewFromConfig(cfg, func(o *pricing.Options) {
		o.Region = pricingAPIRegion
	})
}

func NewAWSProvider(cfg aws.Config) *AWSProvider {
	return &AWSProvider{
		Region:        cfg.Region,
		EC2Client:     ec2.NewFromConfig(cfg),
		PricingClient: NewAWSPricingClient(cfg, cfg.Region),
	}
}

func (p *AWSProvider) GetOnDemandPricing(ctx context.Context) (OnDemandPriceList, error) {
	onDemandPrices, err := p.fetchOnDemandPricing(
		ctx,
		pricingtypes.Filter{
			Field: aws.String("tenancy"),
			Type:  pricingtypes.FilterTypeTermMatch,
			Value: aws.String("Shared"),
		},
		pricingtypes.Filter{
			Field: aws.String("productFamily"),
			Type:  pricingtypes.FilterTypeTermMatch,
			Value: aws.String("Compute Instance"),
		},
	)
	if err != nil {
		return nil, err
	}
	onDemandMetalPrices, err := p.fetchOnDemandPricing(
		ctx,
		pricingtypes.Filter{
			Field: aws.String("tenancy"),
			Type:  pricingtypes.FilterTypeTermMatch,
			Value: aws.String("Dedicated"),
		},
		pricingtypes.Filter{
			Field: aws.String("productFamily"),
			Type:  pricingtypes.FilterTypeTermMatch,
			Value: aws.String("Compute Instance (bare metal)"),
		},
	)
	if err != nil {
		return nil, err
	}
	if len(onDemandPrices) == 0 || len(onDemandMetalPrices) == 0 {
		return nil, errors.New("no on-demand pricing found")
	}
	return lo.Assign(onDemandPrices, onDemandMetalPrices), nil
}

func (p *AWSProvider) GetSpotPricing(ctx context.Context) (SpotPriceList, error) {
	prices := make(SpotPriceList)

	spotPriceHistoryPaginator := ec2.NewDescribeSpotPriceHistoryPaginator(
		p.EC2Client,
		&ec2.DescribeSpotPriceHistoryInput{
			ProductDescriptions: []string{"Linux/UNIX", "Linux/UNIX (Amazon VPC)"},
			StartTime:           aws.Time(time.Now()),
		},
	)
	for spotPriceHistoryPaginator.HasMorePages() {
		output, err := spotPriceHistoryPaginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, sph := range output.SpotPriceHistory {
			spotPriceStr := aws.ToString(sph.SpotPrice)
			spotPrice, err := strconv.ParseFloat(spotPriceStr, 64)
			// these errors shouldn't occur, but if pricing API does have an error, we ignore the record
			if err != nil {
				log.Printf("unable to parse price record %#v", sph)
				continue
			}
			if sph.Timestamp == nil {
				continue
			}
			instanceType := string(sph.InstanceType)
			az := aws.ToString(sph.AvailabilityZone)
			_, ok := prices[instanceType]
			if !ok {
				prices[instanceType] = map[string]float64{}
			}
			prices[instanceType][az] = spotPrice
		}
	}
	if len(prices) == 0 {
		return nil, errors.New("no spot pricing found")
	}
	return prices, nil
}

func (p *AWSProvider) GetFargatePricing(ctx context.Context) (FargatePrice, error) {
	price := &FargatePrice{}
	filters := []pricingtypes.Filter{
		{
			Field: aws.String("regionCode"),
			Type:  pricingtypes.FilterTypeTermMatch,
			Value: aws.String(p.Region),
		},
	}
	productsPaginator := pricing.NewGetProductsPaginator(p.PricingClient, &pricing.GetProductsInput{
		Filters:     filters,
		ServiceCode: aws.String("AmazonEKS"),
	})
	for productsPaginator.HasMorePages() {
		output, err := productsPaginator.NextPage(ctx)
		if err != nil {
			return *price, err
		}
		price, err = p.parseFargatePage(price, output)
		if err != nil {
			return *price, err
		}
	}
	return *price, nil
}

func (p *AWSProvider) fetchOnDemandPricing(
	ctx context.Context,
	additionalFilters ...pricingtypes.Filter,
) (map[string]float64, error) {
	prices := map[string]float64{}
	filters := append(
		[]pricingtypes.Filter{
			{
				Field: aws.String("regionCode"),
				Type:  pricingtypes.FilterTypeTermMatch,
				Value: aws.String(p.Region),
			},
			{
				Field: aws.String("serviceCode"),
				Type:  pricingtypes.FilterTypeTermMatch,
				Value: aws.String("AmazonEC2"),
			},
			{
				Field: aws.String("preInstalledSw"),
				Type:  pricingtypes.FilterTypeTermMatch,
				Value: aws.String("NA"),
			},
			{
				Field: aws.String("operatingSystem"),
				Type:  pricingtypes.FilterTypeTermMatch,
				Value: aws.String("Linux"),
			},
			{
				Field: aws.String("capacitystatus"),
				Type:  pricingtypes.FilterTypeTermMatch,
				Value: aws.String("Used"),
			},
			{
				Field: aws.String("marketoption"),
				Type:  pricingtypes.FilterTypeTermMatch,
				Value: aws.String("OnDemand"),
			},
		},
		additionalFilters...,
	)
	productsPaginator := pricing.NewGetProductsPaginator(p.PricingClient, &pricing.GetProductsInput{
		Filters:     filters,
		ServiceCode: aws.String("AmazonEC2"),
	})
	for productsPaginator.HasMorePages() {
		output, err := productsPaginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		prices, err = p.parseOnDemandPage(prices, output)
		if err != nil {
			return nil, err
		}
	}

	return prices, nil
}

func (p *AWSProvider) parseOnDemandPage(
	prices map[string]float64,
	output *pricing.GetProductsOutput,
) (map[string]float64, error) {
	// this isn't the full pricing struct, just the portions we care about
	type priceItem struct {
		Product struct {
			Attributes struct {
				InstanceType string
			}
		}
		Terms struct {
			OnDemand map[string]struct {
				PriceDimensions map[string]struct {
					PricePerUnit struct {
						USD string
					}
				}
			}
		}
	}

	for _, outer := range output.PriceList {
		var pItem priceItem
		err := json.Unmarshal([]byte(outer), &pItem)
		if err != nil {
			return prices, fmt.Errorf("decoding: %w", err)
		}
		if pItem.Product.Attributes.InstanceType == "" {
			continue
		}
		for _, term := range pItem.Terms.OnDemand {
			for _, v := range term.PriceDimensions {
				price, err := strconv.ParseFloat(v.PricePerUnit.USD, 64)
				if err != nil || price == 0 {
					continue
				}
				prices[pItem.Product.Attributes.InstanceType] = price
			}
		}
	}
	return prices, nil
}

func (p *AWSProvider) parseFargatePage(
	fargatePrice *FargatePrice,
	output *pricing.GetProductsOutput,
) (*FargatePrice, error) {
	// this isn't the full pricing struct, just the portions we care about
	type priceItem struct {
		Product struct {
			ProductFamily string
			Attributes    struct {
				UsageType  string
				MemoryType string
			}
		}
		Terms struct {
			OnDemand map[string]struct {
				PriceDimensions map[string]struct {
					PricePerUnit struct {
						USD string
					}
				}
			}
		}
	}

	for _, outer := range output.PriceList {
		var pItem priceItem
		err := json.Unmarshal([]byte(outer), &pItem)
		if err != nil {
			return nil, fmt.Errorf("decoding: %w", err)
		}
		if !strings.Contains(pItem.Product.Attributes.UsageType, "Fargate") {
			continue
		}
		name := pItem.Product.Attributes.UsageType
		for _, term := range pItem.Terms.OnDemand {
			for _, v := range term.PriceDimensions {
				price, err := strconv.ParseFloat(v.PricePerUnit.USD, 64)
				if err != nil || price == 0 {
					continue
				}
				if strings.Contains(name, "vCPU-Hours") {
					fargatePrice.VCPUPerHour = price
				} else if strings.Contains(name, "GB-Hours") {
					fargatePrice.GBPerHour = price
				} else {
					return nil, fmt.Errorf("unsupported fargate price information found: %s", name)
				}
			}
		}
	}
	return fargatePrice, nil
}
