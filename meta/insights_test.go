package meta

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestAccountInsightsValidationAndRequest(t *testing.T) {
	requests := 0
	c := testClient(t, allReadScopes(), false, func(req *http.Request) (*http.Response, error) {
		requests++
		if req.URL.Path != "/v23.0/ig-1/insights" || req.URL.Query().Get("metric") != "reach,profile_views" || req.URL.Query().Get("period") != "day" {
			t.Errorf("request=%s", req.URL)
		}
		return jsonResponse(req, http.StatusOK, `{"data":[{"id":"ig-1/insights/reach/day","name":"reach","period":"day","values":[{"value":12,"end_time":"2026-07-02T00:00:00+0000"}]}]}`), nil
	})
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	request := AccountInsightsRequest{AccountID: "ig-1", Metrics: []AccountMetric{AccountMetricReach, AccountMetricProfileViews}, Period: PeriodDay,
		Timeframe: Timeframe{Since: start, Until: start.Add(24 * time.Hour)}}
	page, err := c.GetAccountInsights(context.Background(), request)
	if err != nil || len(page.Items) != 1 || page.Items[0].Name != "reach" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	request.Metrics = []AccountMetric{"made_up"}
	if _, err := c.GetAccountInsights(context.Background(), request); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("metric error=%v", err)
	}
	request.Metrics = []AccountMetric{AccountMetricReach}
	request.Timeframe.Until = request.Timeframe.Since
	if _, err := c.GetAccountInsights(context.Background(), request); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("timeframe error=%v", err)
	}
	if requests != 1 {
		t.Fatalf("invalid requests reached HTTP: %d", requests)
	}
}

func TestMediaInsightsPeriodAndTimeframeValidation(t *testing.T) {
	requests := 0
	c := testClient(t, allReadScopes(), false, func(req *http.Request) (*http.Response, error) {
		requests++
		return jsonResponse(req, http.StatusOK, `{"data":[]}`), nil
	})
	valid := MediaInsightsRequest{MediaID: "media-1", Metrics: []MediaMetric{MediaMetricReach}, Period: PeriodLifetime}
	if _, err := c.GetMediaInsights(context.Background(), valid); err != nil {
		t.Fatal(err)
	}
	valid.Period = PeriodDay
	if _, err := c.GetMediaInsights(context.Background(), valid); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("period error=%v", err)
	}
	valid.Period = PeriodLifetime
	valid.Timeframe.Since = time.Now()
	if _, err := c.GetMediaInsights(context.Background(), valid); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("timeframe error=%v", err)
	}
	if requests != 1 {
		t.Fatalf("requests=%d", requests)
	}
}
