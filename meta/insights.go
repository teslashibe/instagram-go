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

type AccountInsightsRequest struct {
	AccountID string          `json:"account_id,omitempty"`
	Metrics   []AccountMetric `json:"metrics"`
	Period    Period          `json:"period"`
	Timeframe Timeframe       `json:"timeframe"`
	ListOptions
}

type MediaInsightsRequest struct {
	MediaID   string        `json:"media_id"`
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
		"metric": {joinAccountMetrics(request.Metrics)},
		"period": {string(request.Period)},
		"since":  {strconv.FormatInt(request.Timeframe.Since.Unix(), 10)},
		"until":  {strconv.FormatInt(request.Timeframe.Until.Unix(), 10)},
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
	if request.Period != PeriodDay && request.Period != PeriodWeek && request.Period != PeriodDays28 {
		return fmt.Errorf("%w: account insight period must be day, week, or days_28", ErrInvalidInput)
	}
	if err := validateTimeframe(request.Timeframe, 93*24*time.Hour, true); err != nil {
		return err
	}
	allowed := map[AccountMetric]bool{
		AccountMetricReach: true, AccountMetricProfileViews: true, AccountMetricWebsiteClicks: true,
		AccountMetricAccountsEngaged: true, AccountMetricTotalInteractions: true, AccountMetricFollowsAndUnfollows: true,
	}
	if err := validateMetrics(request.Metrics, allowed); err != nil {
		return err
	}
	return validateListOptions(request.ListOptions)
}

func validateMediaInsights(request MediaInsightsRequest) error {
	if strings.TrimSpace(request.MediaID) == "" {
		return fmt.Errorf("%w: media_id is required", ErrInvalidInput)
	}
	if request.Period != PeriodLifetime {
		return fmt.Errorf("%w: media insight period must be lifetime", ErrInvalidInput)
	}
	if !request.Timeframe.Since.IsZero() || !request.Timeframe.Until.IsZero() {
		return fmt.Errorf("%w: media lifetime insights do not accept a timeframe", ErrInvalidInput)
	}
	allowed := map[MediaMetric]bool{
		MediaMetricReach: true, MediaMetricLikes: true, MediaMetricComments: true, MediaMetricSaved: true,
		MediaMetricShares: true, MediaMetricPlays: true, MediaMetricTotalInteractions: true,
	}
	if err := validateMetrics(request.Metrics, allowed); err != nil {
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
