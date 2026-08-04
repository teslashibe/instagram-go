package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type AdListRequest struct {
	AdAccountID string   `json:"ad_account_id,omitempty"`
	Statuses    []string `json:"statuses,omitempty"`
	ListOptions
}

type DeliveryObjectType string

const (
	DeliveryCampaign DeliveryObjectType = "campaign"
	DeliveryAdSet    DeliveryObjectType = "ad_set"
	DeliveryAd       DeliveryObjectType = "ad"
)

type DeliveryRequest struct {
	ObjectType DeliveryObjectType `json:"object_type"`
	ObjectID   string             `json:"object_id"`
}

type AdInsightMetric string

const (
	AdMetricImpressions AdInsightMetric = "impressions"
	AdMetricReach       AdInsightMetric = "reach"
	AdMetricClicks      AdInsightMetric = "clicks"
	AdMetricSpend       AdInsightMetric = "spend"
	AdMetricCPC         AdInsightMetric = "cpc"
	AdMetricCPM         AdInsightMetric = "cpm"
	AdMetricCTR         AdInsightMetric = "ctr"
	AdMetricActions     AdInsightMetric = "actions"
)

type InsightLevel string

const (
	InsightLevelAccount  InsightLevel = "account"
	InsightLevelCampaign InsightLevel = "campaign"
	InsightLevelAdSet    InsightLevel = "adset"
	InsightLevelAd       InsightLevel = "ad"
)

type AdInsightsRequest struct {
	AdAccountID   string            `json:"ad_account_id,omitempty"`
	Metrics       []AdInsightMetric `json:"metrics"`
	Level         InsightLevel      `json:"level"`
	Timeframe     Timeframe         `json:"timeframe"`
	TimeIncrement int               `json:"time_increment,omitempty"`
	ListOptions
}

type AdInsight struct {
	AccountID    string          `json:"account_id,omitempty"`
	AccountName  string          `json:"account_name,omitempty"`
	CampaignID   string          `json:"campaign_id,omitempty"`
	CampaignName string          `json:"campaign_name,omitempty"`
	AdSetID      string          `json:"adset_id,omitempty"`
	AdSetName    string          `json:"adset_name,omitempty"`
	AdID         string          `json:"ad_id,omitempty"`
	AdName       string          `json:"ad_name,omitempty"`
	DateStart    string          `json:"date_start,omitempty"`
	DateStop     string          `json:"date_stop,omitempty"`
	Impressions  string          `json:"impressions,omitempty"`
	Reach        string          `json:"reach,omitempty"`
	Clicks       string          `json:"clicks,omitempty"`
	Spend        string          `json:"spend,omitempty"`
	CPC          string          `json:"cpc,omitempty"`
	CPM          string          `json:"cpm,omitempty"`
	CTR          string          `json:"ctr,omitempty"`
	Actions      json.RawMessage `json:"actions,omitempty"`
}

func (c *Client) ListCampaigns(ctx context.Context, request AdListRequest) (Page[Campaign], error) {
	return listAdObjects[Campaign](ctx, c, request, "campaigns",
		"id,name,objective,status,effective_status,buying_type,start_time,stop_time,updated_time")
}

func (c *Client) ListAdSets(ctx context.Context, request AdListRequest) (Page[AdSet], error) {
	return listAdObjects[AdSet](ctx, c, request, "adsets",
		"id,name,campaign_id,status,effective_status,daily_budget,lifetime_budget,start_time,end_time,optimization_goal,billing_event,targeting")
}

func (c *Client) ListAds(ctx context.Context, request AdListRequest) (Page[Ad], error) {
	return listAdObjects[Ad](ctx, c, request, "ads",
		"id,name,adset_id,campaign_id,status,effective_status,creative{id},updated_time")
}

func (c *Client) ListAdCreatives(ctx context.Context, request AdListRequest) (Page[AdCreative], error) {
	if len(request.Statuses) > 0 {
		return Page[AdCreative]{}, fmt.Errorf("%w: creative lists do not accept delivery statuses", ErrInvalidInput)
	}
	return listAdObjects[AdCreative](ctx, c, request, "adcreatives",
		"id,name,title,body,object_story_id,instagram_actor_id,thumbnail_url,url_tags,object_story_spec")
}

func listAdObjects[T any](ctx context.Context, c *Client, request AdListRequest, edge, fields string) (Page[T], error) {
	accountID, err := c.selectedAdAccount(request.AdAccountID)
	if err != nil {
		return Page[T]{}, err
	}
	if err := validateStatuses(request.Statuses); err != nil {
		return Page[T]{}, err
	}
	path := accountID + "/" + edge
	q := url.Values{"fields": {fields}}
	if len(request.Statuses) > 0 {
		raw, _ := json.Marshal(request.Statuses)
		q.Set("effective_status", string(raw))
	}
	q, binding, err := preparePageQuery(path, q, request.ListOptions)
	if err != nil {
		return Page[T]{}, err
	}
	var raw graphPage[T]
	if err := c.doJSON(ctx, http.MethodGet, path, q, nil, []Scope{ScopeAdsRead}, &raw); err != nil {
		return Page[T]{}, err
	}
	return finishPage(raw, binding)
}

func (c *Client) GetDelivery(ctx context.Context, request DeliveryRequest) (*DeliveryStatus, error) {
	if strings.TrimSpace(request.ObjectID) == "" {
		return nil, fmt.Errorf("%w: object_id is required", ErrInvalidInput)
	}
	switch request.ObjectType {
	case DeliveryCampaign, DeliveryAdSet, DeliveryAd:
	default:
		return nil, fmt.Errorf("%w: object_type must be campaign, ad_set, or ad", ErrInvalidInput)
	}
	var delivery DeliveryStatus
	q := url.Values{"fields": {"id,name,status,configured_status,effective_status"}}
	if err := c.doJSON(ctx, http.MethodGet, request.ObjectID, q, nil, []Scope{ScopeAdsRead}, &delivery); err != nil {
		return nil, err
	}
	return &delivery, nil
}

func (c *Client) GetAdInsights(ctx context.Context, request AdInsightsRequest) (Page[AdInsight], error) {
	accountID, err := c.selectedAdAccount(request.AdAccountID)
	if err != nil {
		return Page[AdInsight]{}, err
	}
	if err := validateAdInsights(request); err != nil {
		return Page[AdInsight]{}, err
	}
	path := accountID + "/insights"
	fields := append([]string{"account_id", "account_name", "campaign_id", "campaign_name", "adset_id", "adset_name", "ad_id", "ad_name"}, adMetricStrings(request.Metrics)...)
	timeRange, _ := json.Marshal(map[string]string{
		"since": request.Timeframe.Since.Format("2006-01-02"),
		"until": request.Timeframe.Until.Format("2006-01-02"),
	})
	q := url.Values{
		"fields":     {strings.Join(fields, ",")},
		"level":      {string(request.Level)},
		"time_range": {string(timeRange)},
	}
	if request.TimeIncrement > 0 {
		q.Set("time_increment", fmt.Sprint(request.TimeIncrement))
	}
	q, binding, err := preparePageQuery(path, q, request.ListOptions)
	if err != nil {
		return Page[AdInsight]{}, err
	}
	var raw graphPage[AdInsight]
	if err := c.doJSON(ctx, http.MethodGet, path, q, nil, []Scope{ScopeAdsRead}, &raw); err != nil {
		return Page[AdInsight]{}, err
	}
	return finishPage(raw, binding)
}

func validateStatuses(statuses []string) error {
	allowed := map[string]bool{"ACTIVE": true, "PAUSED": true, "DELETED": true, "ARCHIVED": true, "IN_PROCESS": true,
		"WITH_ISSUES": true, "CAMPAIGN_PAUSED": true, "ADSET_PAUSED": true, "DISAPPROVED": true, "PENDING_REVIEW": true}
	for _, status := range statuses {
		if !allowed[status] {
			return fmt.Errorf("%w: unsupported effective status %q", ErrInvalidInput, status)
		}
	}
	return nil
}

func validateAdInsights(request AdInsightsRequest) error {
	allowed := map[AdInsightMetric]bool{
		AdMetricImpressions: true, AdMetricReach: true, AdMetricClicks: true, AdMetricSpend: true,
		AdMetricCPC: true, AdMetricCPM: true, AdMetricCTR: true, AdMetricActions: true,
	}
	if err := validateMetrics(request.Metrics, allowed); err != nil {
		return err
	}
	switch request.Level {
	case InsightLevelAccount, InsightLevelCampaign, InsightLevelAdSet, InsightLevelAd:
	default:
		return fmt.Errorf("%w: invalid insight level", ErrInvalidInput)
	}
	if err := validateTimeframe(request.Timeframe, 37*30*24*time.Hour, true); err != nil {
		return err
	}
	if request.TimeIncrement < 0 || request.TimeIncrement > 90 {
		return fmt.Errorf("%w: time_increment must be between 1 and 90", ErrInvalidInput)
	}
	return validateListOptions(request.ListOptions)
}

func adMetricStrings(metrics []AdInsightMetric) []string {
	values := make([]string, len(metrics))
	for i, metric := range metrics {
		values[i] = string(metric)
	}
	return values
}
