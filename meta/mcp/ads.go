package mcp

import (
	"context"

	"github.com/teslashibe/instagram-go/meta"
	"github.com/teslashibe/mcptool"
)

type ListAdsInput struct {
	AdAccountID string   `json:"ad_account_id,omitempty" jsonschema:"description=ad account ID; defaults to selected account"`
	Statuses    []string `json:"statuses,omitempty" jsonschema:"description=effective status filters"`
	Limit       int      `json:"limit,omitempty" jsonschema:"minimum=1,maximum=100,default=25"`
	Cursor      string   `json:"cursor,omitempty" jsonschema:"description=opaque next_cursor from a previous response"`
}

func adListRequest(in ListAdsInput) meta.AdListRequest {
	return meta.AdListRequest{AdAccountID: in.AdAccountID, Statuses: in.Statuses,
		ListOptions: meta.ListOptions{Limit: in.Limit, Cursor: in.Cursor}}
}

func listCampaigns(ctx context.Context, c *meta.Client, in ListAdsInput) (any, error) {
	page, err := c.ListCampaigns(ctx, adListRequest(in))
	return page, toolError(err)
}

func listAdSets(ctx context.Context, c *meta.Client, in ListAdsInput) (any, error) {
	page, err := c.ListAdSets(ctx, adListRequest(in))
	return page, toolError(err)
}

func listAds(ctx context.Context, c *meta.Client, in ListAdsInput) (any, error) {
	page, err := c.ListAds(ctx, adListRequest(in))
	return page, toolError(err)
}

func listCreatives(ctx context.Context, c *meta.Client, in ListAdsInput) (any, error) {
	page, err := c.ListAdCreatives(ctx, adListRequest(in))
	return page, toolError(err)
}

type DeliveryInput struct {
	ObjectType meta.DeliveryObjectType `json:"object_type" jsonschema:"description=delivery object type,enum=campaign,enum=ad_set,enum=ad,required"`
	ObjectID   string                  `json:"object_id" jsonschema:"description=campaign ad set or ad ID,required"`
}

func getDelivery(ctx context.Context, c *meta.Client, in DeliveryInput) (any, error) {
	delivery, err := c.GetDelivery(ctx, meta.DeliveryRequest{ObjectType: in.ObjectType, ObjectID: in.ObjectID})
	return delivery, toolError(err)
}

type AdInsightsInput struct {
	AdAccountID   string                 `json:"ad_account_id,omitempty" jsonschema:"description=ad account ID; defaults to selected account"`
	Metrics       []meta.AdInsightMetric `json:"metrics" jsonschema:"description=delivery and performance metrics,required,minItems=1"`
	Level         meta.InsightLevel      `json:"level" jsonschema:"description=reporting level,enum=account,enum=campaign,enum=adset,enum=ad,required"`
	Since         string                 `json:"since" jsonschema:"description=start date in YYYY-MM-DD format,required"`
	Until         string                 `json:"until" jsonschema:"description=end date in YYYY-MM-DD format,required"`
	TimeIncrement int                    `json:"time_increment,omitempty" jsonschema:"description=number of days per row,minimum=1,maximum=90"`
	Limit         int                    `json:"limit,omitempty" jsonschema:"minimum=1,maximum=100,default=25"`
	Cursor        string                 `json:"cursor,omitempty" jsonschema:"description=opaque next_cursor from a previous response"`
}

func adInsights(ctx context.Context, c *meta.Client, in AdInsightsInput) (any, error) {
	frame, err := parseTimeframe(in.Since, in.Until)
	if err != nil {
		return nil, toolError(err)
	}
	page, err := c.GetAdInsights(ctx, meta.AdInsightsRequest{
		AdAccountID: in.AdAccountID, Metrics: in.Metrics, Level: in.Level, Timeframe: frame,
		TimeIncrement: in.TimeIncrement, ListOptions: meta.ListOptions{Limit: in.Limit, Cursor: in.Cursor},
	})
	return page, toolError(err)
}

var adTools = []mcptool.Tool{
	mcptool.Define[*meta.Client, ListAdsInput]("instagram_meta_list_campaigns", "List read-only campaigns and effective delivery status", "ListCampaigns", listCampaigns),
	mcptool.Define[*meta.Client, ListAdsInput]("instagram_meta_list_ad_sets", "List read-only ad sets, budgets, schedules, and delivery status", "ListAdSets", listAdSets),
	mcptool.Define[*meta.Client, ListAdsInput]("instagram_meta_list_ads", "List read-only ads, creatives, and effective delivery status", "ListAds", listAds),
	mcptool.Define[*meta.Client, ListAdsInput]("instagram_meta_list_creatives", "List read-only ad creative metadata for an ad account", "ListAdCreatives", listCreatives),
	mcptool.Define[*meta.Client, DeliveryInput]("instagram_meta_get_delivery", "Read configured and effective delivery status for an ad object", "GetDelivery", getDelivery),
	mcptool.Define[*meta.Client, AdInsightsInput]("instagram_meta_get_ad_insights", "Read validated delivery and performance insights for an ad account", "GetAdInsights", adInsights),
}
