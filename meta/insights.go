package meta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Period string

const (
	PeriodDay      Period = "day"
	PeriodWeek     Period = "week"
	PeriodDays28   Period = "days_28"
	PeriodLifetime Period = "lifetime"
)

type Timeframe struct {
	Since time.Time `json:"since"`
	Until time.Time `json:"until"`
}

type AccountMetric string

const (
	AccountMetricReach               AccountMetric = "reach"
	AccountMetricProfileViews        AccountMetric = "profile_views"
	AccountMetricWebsiteClicks       AccountMetric = "website_clicks"
	AccountMetricAccountsEngaged     AccountMetric = "accounts_engaged"
	AccountMetricTotalInteractions   AccountMetric = "total_interactions"
	AccountMetricFollowsAndUnfollows AccountMetric = "follows_and_unfollows"
)

// InsightMetricType selects the Graph response shape for account insights.
// A single request cannot mix time-series and total-value metrics.
type InsightMetricType string

const (
	MetricTypeTimeSeries InsightMetricType = "time_series"
	MetricTypeTotalValue InsightMetricType = "total_value"
)

type MediaMetric string

const (
	MediaMetricReach             MediaMetric = "reach"
	MediaMetricLikes             MediaMetric = "likes"
	MediaMetricComments          MediaMetric = "comments"
	MediaMetricSaved             MediaMetric = "saved"
	MediaMetricShares            MediaMetric = "shares"
	MediaMetricPlays             MediaMetric = "plays"
	MediaMetricTotalInteractions MediaMetric = "total_interactions"
)

// MediaType identifies the Instagram media surface whose insight metrics are
// being requested. Graph exposes different metrics for feed media, reels, and
// stories, so callers must provide this context for pre-request validation.
type MediaType string

const (
	MediaTypeImage         MediaType = "image"
	MediaTypeCarouselAlbum MediaType = "carousel_album"
	MediaTypeVideo         MediaType = "video"
	MediaTypeReel          MediaType = "reel"
	MediaTypeStory         MediaType = "story"
)

type AccountInsightsRequest struct {
	AccountID  string            `json:"account_id,omitempty"`
	Metrics    []AccountMetric   `json:"metrics"`
	MetricType InsightMetricType `json:"metric_type"`
	Period     Period            `json:"period"`
	Timeframe  Timeframe         `json:"timeframe"`
	ListOptions
}

type MediaInsightsRequest struct {
	MediaID   string        `json:"media_id"`
	MediaType MediaType     `json:"media_type"`
	Metrics   []MediaMetric `json:"metrics"`
	Period    Period        `json:"period"`
	Timeframe Timeframe     `json:"timeframe,omitempty"`
	ListOptions
}

type Insight struct {
	ID          string          `json:"id,omitempty"`
	Name        string          `json:"name"`
	Period      string          `json:"period,omitempty"`
	Title       string          `json:"title,omitempty"`
	Description string          `json:"description,omitempty"`
	Values      []InsightValue  `json:"values,omitempty"`
	TotalValue  json.RawMessage `json:"total_value,omitempty"`
}

type InsightValue struct {
	Value   json.RawMessage `json:"value"`
	EndTime string          `json:"end_time,omitempty"`
}

func (c *Client) GetAccountInsights(ctx context.Context, request AccountInsightsRequest) (Page[Insight], error) {
	accountID, err := c.selectedInstagramAccount(request.AccountID)
	if err != nil {
		return Page[Insight]{}, err
	}
	if err := validateAccountInsights(request); err != nil {
		return Page[Insight]{}, err
	}
	path := accountID + "/insights"
	q := url.Values{
		"metric":      {joinAccountMetrics(request.Metrics)},
		"metric_type": {string(request.MetricType)},
		"period":      {string(request.Period)},
		"since":       {strconv.FormatInt(request.Timeframe.Since.Unix(), 10)},
		"until":       {strconv.FormatInt(request.Timeframe.Until.Unix(), 10)},
	}
	q, binding, err := preparePageQuery(path, q, request.ListOptions)
	if err != nil {
		return Page[Insight]{}, err
	}
	var raw graphPage[Insight]
	if err := c.doJSON(ctx, http.MethodGet, path, q, nil, []Scope{ScopeInstagramManageInsights}, &raw); err != nil {
		return Page[Insight]{}, err
	}
	return finishPage(raw, binding)
}

func (c *Client) GetMediaInsights(ctx context.Context, request MediaInsightsRequest) (Page[Insight], error) {
	if err := validateMediaInsights(request); err != nil {
		return Page[Insight]{}, err
	}
	path := strings.TrimSpace(request.MediaID) + "/insights"
	q := url.Values{
		"metric": {joinMediaMetrics(request.Metrics)},
		"period": {string(request.Period)},
	}
	q, binding, err := preparePageQuery(path, q, request.ListOptions)
	if err != nil {
		return Page[Insight]{}, err
	}
	var raw graphPage[Insight]
	if err := c.doJSON(ctx, http.MethodGet, path, q, nil, []Scope{ScopeInstagramManageInsights}, &raw); err != nil {
		return Page[Insight]{}, err
	}
	return finishPage(raw, binding)
}

func validateAccountInsights(request AccountInsightsRequest) error {
	if err := validateTimeframe(request.Timeframe, 93*24*time.Hour, true); err != nil {
		return err
	}
	compatibility := map[AccountMetric]map[Period]bool{
		AccountMetricReach: {
			PeriodDay: true, PeriodWeek: true, PeriodDays28: true,
		},
		AccountMetricProfileViews:        {PeriodDay: true},
		AccountMetricWebsiteClicks:       {PeriodDay: true},
		AccountMetricAccountsEngaged:     {PeriodDay: true},
		AccountMetricTotalInteractions:   {PeriodDay: true},
		AccountMetricFollowsAndUnfollows: {PeriodDay: true},
	}
	if err := validateMetricPeriodCombinations(request.Metrics, request.Period, compatibility); err != nil {
		return err
	}
	metricTypes := map[AccountMetric]InsightMetricType{
		AccountMetricReach:               MetricTypeTimeSeries,
		AccountMetricProfileViews:        MetricTypeTimeSeries,
		AccountMetricWebsiteClicks:       MetricTypeTimeSeries,
		AccountMetricAccountsEngaged:     MetricTypeTotalValue,
		AccountMetricTotalInteractions:   MetricTypeTotalValue,
		AccountMetricFollowsAndUnfollows: MetricTypeTotalValue,
	}
	switch request.MetricType {
	case MetricTypeTimeSeries, MetricTypeTotalValue:
	default:
		return fmt.Errorf("%w: metric_type must be time_series or total_value", ErrInvalidInput)
	}
	for _, metric := range request.Metrics {
		if metricTypes[metric] != request.MetricType {
			return fmt.Errorf("%w: metric %q requires metric_type %q and cannot be mixed with %q metrics",
				ErrInvalidInput, metric, metricTypes[metric], request.MetricType)
		}
	}
	return validateListOptions(request.ListOptions)
}

func validateMediaInsights(request MediaInsightsRequest) error {
	if strings.TrimSpace(request.MediaID) == "" {
		return fmt.Errorf("%w: media_id is required", ErrInvalidInput)
	}
	switch request.MediaType {
	case MediaTypeImage, MediaTypeCarouselAlbum, MediaTypeVideo, MediaTypeReel, MediaTypeStory:
	default:
		return fmt.Errorf("%w: media_type must be image, carousel_album, video, reel, or story", ErrInvalidInput)
	}
	if request.Period != PeriodLifetime {
		return fmt.Errorf("%w: media insight period must be lifetime", ErrInvalidInput)
	}
	if !request.Timeframe.Since.IsZero() || !request.Timeframe.Until.IsZero() {
		return fmt.Errorf("%w: media lifetime insights do not accept a timeframe", ErrInvalidInput)
	}
	compatibility := map[MediaMetric]map[MediaType]bool{
		MediaMetricReach: {
			MediaTypeImage: true, MediaTypeCarouselAlbum: true, MediaTypeVideo: true,
			MediaTypeReel: true, MediaTypeStory: true,
		},
		MediaMetricLikes: {
			MediaTypeImage: true, MediaTypeCarouselAlbum: true, MediaTypeVideo: true, MediaTypeReel: true,
		},
		MediaMetricComments: {
			MediaTypeImage: true, MediaTypeCarouselAlbum: true, MediaTypeVideo: true, MediaTypeReel: true,
		},
		MediaMetricSaved: {
			MediaTypeImage: true, MediaTypeCarouselAlbum: true, MediaTypeVideo: true, MediaTypeReel: true,
		},
		MediaMetricShares: {
			MediaTypeImage: true, MediaTypeCarouselAlbum: true, MediaTypeVideo: true, MediaTypeReel: true,
		},
		MediaMetricPlays: {MediaTypeVideo: true, MediaTypeReel: true},
		MediaMetricTotalInteractions: {
			MediaTypeImage: true, MediaTypeCarouselAlbum: true, MediaTypeVideo: true, MediaTypeReel: true,
		},
	}
	if err := validateMetricResourceCombinations(request.Metrics, request.MediaType, compatibility); err != nil {
		return err
	}
	return validateListOptions(request.ListOptions)
}

func validateTimeframe(frame Timeframe, maximum time.Duration, required bool) error {
	if frame.Since.IsZero() || frame.Until.IsZero() {
		if required {
			return fmt.Errorf("%w: since and until are required", ErrInvalidInput)
		}
		return nil
	}
	if !frame.Since.Before(frame.Until) {
		return fmt.Errorf("%w: since must be before until", ErrInvalidInput)
	}
	if frame.Until.Sub(frame.Since) > maximum {
		return fmt.Errorf("%w: timeframe exceeds %s", ErrInvalidInput, maximum)
	}
	return nil
}

func validateMetrics[T ~string](metrics []T, allowed map[T]bool) error {
	if len(metrics) == 0 {
		return fmt.Errorf("%w: at least one metric is required", ErrInvalidInput)
	}
	seen := make(map[T]bool, len(metrics))
	for _, metric := range metrics {
		if !allowed[metric] {
			return fmt.Errorf("%w: unsupported metric %q", ErrInvalidInput, metric)
		}
		if seen[metric] {
			return fmt.Errorf("%w: duplicate metric %q", ErrInvalidInput, metric)
		}
		seen[metric] = true
	}
	return nil
}

func validateMetricPeriodCombinations[T ~string](metrics []T, period Period, compatibility map[T]map[Period]bool) error {
	allowed := make(map[T]bool, len(compatibility))
	for metric := range compatibility {
		allowed[metric] = true
	}
	if err := validateMetrics(metrics, allowed); err != nil {
		return err
	}
	for _, metric := range metrics {
		if !compatibility[metric][period] {
			return fmt.Errorf("%w: metric %q does not support period %q", ErrInvalidInput, metric, period)
		}
	}
	return nil
}

func validateMetricResourceCombinations[T ~string, R ~string](metrics []T, resource R, compatibility map[T]map[R]bool) error {
	allowed := make(map[T]bool, len(compatibility))
	for metric := range compatibility {
		allowed[metric] = true
	}
	if err := validateMetrics(metrics, allowed); err != nil {
		return err
	}
	for _, metric := range metrics {
		if !compatibility[metric][resource] {
			return fmt.Errorf("%w: metric %q is not supported for %q", ErrInvalidInput, metric, resource)
		}
	}
	return nil
}

func joinAccountMetrics(metrics []AccountMetric) string {
	parts := make([]string, len(metrics))
	for i, metric := range metrics {
		parts[i] = string(metric)
	}
	return strings.Join(parts, ",")
}

func joinMediaMetrics(metrics []MediaMetric) string {
	parts := make([]string, len(metrics))
	for i, metric := range metrics {
		parts[i] = string(metric)
	}
	return strings.Join(parts, ",")
}
