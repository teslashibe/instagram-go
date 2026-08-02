package mcp

import (
	"context"
	"fmt"
	"time"

	"github.com/teslashibe/instagram-go/meta"
	"github.com/teslashibe/mcptool"
)

type AccountInsightsInput struct {
	AccountID  string                 `json:"account_id,omitempty" jsonschema:"description=Instagram professional account ID; defaults to selected account"`
	Metrics    []meta.AccountMetric   `json:"metrics" jsonschema:"description=account insight metrics,required,minItems=1"`
	MetricType meta.InsightMetricType `json:"metric_type" jsonschema:"description=Graph insight response type; metrics cannot mix types,enum=time_series,enum=total_value,required"`
	Period     meta.Period            `json:"period" jsonschema:"description=aggregation period,enum=day,enum=week,enum=days_28,required"`
	Since      string                 `json:"since" jsonschema:"description=start date in YYYY-MM-DD format,required"`
	Until      string                 `json:"until" jsonschema:"description=end date in YYYY-MM-DD format,required"`
	Limit      int                    `json:"limit,omitempty" jsonschema:"minimum=1,maximum=100,default=25"`
	Cursor     string                 `json:"cursor,omitempty" jsonschema:"description=opaque next_cursor from a previous response"`
}

func accountInsights(ctx context.Context, c *meta.Client, in AccountInsightsInput) (any, error) {
	frame, err := parseTimeframe(in.Since, in.Until)
	if err != nil {
		return nil, toolError(err)
	}
	page, err := c.GetAccountInsights(ctx, meta.AccountInsightsRequest{
		AccountID: in.AccountID, Metrics: in.Metrics, MetricType: in.MetricType, Period: in.Period, Timeframe: frame,
		ListOptions: meta.ListOptions{Limit: in.Limit, Cursor: in.Cursor},
	})
	return page, toolError(err)
}

type MediaInsightsInput struct {
	MediaID   string             `json:"media_id" jsonschema:"description=Instagram Graph media ID,required"`
	MediaType meta.MediaType     `json:"media_type" jsonschema:"description=Instagram media surface,enum=image,enum=carousel_album,enum=video,enum=reel,enum=story,required"`
	Metrics   []meta.MediaMetric `json:"metrics" jsonschema:"description=media insight metrics,required,minItems=1"`
	Period    meta.Period        `json:"period" jsonschema:"description=media aggregation period,enum=lifetime,required"`
	Limit     int                `json:"limit,omitempty" jsonschema:"minimum=1,maximum=100,default=25"`
	Cursor    string             `json:"cursor,omitempty" jsonschema:"description=opaque next_cursor from a previous response"`
}

func mediaInsights(ctx context.Context, c *meta.Client, in MediaInsightsInput) (any, error) {
	page, err := c.GetMediaInsights(ctx, meta.MediaInsightsRequest{
		MediaID: in.MediaID, MediaType: in.MediaType, Metrics: in.Metrics, Period: in.Period,
		ListOptions: meta.ListOptions{Limit: in.Limit, Cursor: in.Cursor},
	})
	return page, toolError(err)
}

func parseTimeframe(since, until string) (meta.Timeframe, error) {
	start, err := time.Parse("2006-01-02", since)
	if err != nil {
		return meta.Timeframe{}, fmt.Errorf("%w: since must use YYYY-MM-DD", meta.ErrInvalidInput)
	}
	end, err := time.Parse("2006-01-02", until)
	if err != nil {
		return meta.Timeframe{}, fmt.Errorf("%w: until must use YYYY-MM-DD", meta.ErrInvalidInput)
	}
	return meta.Timeframe{Since: start, Until: end}, nil
}

var insightTools = []mcptool.Tool{
	mcptool.Define[*meta.Client, AccountInsightsInput]("instagram_meta_get_account_insights", "Read validated insights for an Instagram professional account", "GetAccountInsights", accountInsights),
	mcptool.Define[*meta.Client, MediaInsightsInput]("instagram_meta_get_media_insights", "Read validated lifetime insights for Instagram media", "GetMediaInsights", mediaInsights),
}
