package meta

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestReadOnlyAdSurfaces(t *testing.T) {
	paths := make(map[string]int)
	c := testClient(t, allReadScopes(), false, func(req *http.Request) (*http.Response, error) {
		paths[req.URL.Path]++
		switch {
		case strings.HasSuffix(req.URL.Path, "/campaigns"):
			return jsonResponse(req, http.StatusOK, `{"data":[{"id":"campaign-1","name":"Launch","effective_status":"ACTIVE"}]}`), nil
		case strings.HasSuffix(req.URL.Path, "/adsets"):
			return jsonResponse(req, http.StatusOK, `{"data":[{"id":"adset-1","name":"Prospecting","effective_status":"ACTIVE"}]}`), nil
		case strings.HasSuffix(req.URL.Path, "/ads"):
			return jsonResponse(req, http.StatusOK, `{"data":[{"id":"ad-1","name":"Creative A","effective_status":"PAUSED","creative":{"id":"creative-1"}}]}`), nil
		case strings.HasSuffix(req.URL.Path, "/adcreatives"):
			return jsonResponse(req, http.StatusOK, `{"data":[{"id":"creative-1","name":"Image"}]}`), nil
		case req.URL.Path == "/v23.0/ad-1":
			return jsonResponse(req, http.StatusOK, `{"id":"ad-1","name":"Creative A","status":"PAUSED","effective_status":"PAUSED"}`), nil
		default:
			t.Fatalf("unexpected request %s", req.URL)
			return nil, nil
		}
	})
	request := AdListRequest{AdAccountID: "123", Statuses: []string{"ACTIVE"}}
	if page, err := c.ListCampaigns(context.Background(), request); err != nil || len(page.Items) != 1 {
		t.Fatalf("campaigns=%#v err=%v", page, err)
	}
	if _, err := c.ListAdSets(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListAds(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	request.Statuses = nil
	if _, err := c.ListAdCreatives(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if delivery, err := c.GetDelivery(context.Background(), DeliveryRequest{ObjectType: DeliveryAd, ObjectID: "ad-1"}); err != nil || delivery.EffectiveStatus != "PAUSED" {
		t.Fatalf("delivery=%#v err=%v", delivery, err)
	}
	if len(paths) != 5 {
		t.Fatalf("paths=%v", paths)
	}
}

func TestAdInsightsValidationAndPaging(t *testing.T) {
	requests := 0
	c := testClient(t, allReadScopes(), false, func(req *http.Request) (*http.Response, error) {
		requests++
		if req.URL.Query().Get("level") != "campaign" || !strings.Contains(req.URL.Query().Get("fields"), "spend") {
			t.Errorf("query=%s", req.URL.RawQuery)
		}
		return jsonResponse(req, http.StatusOK, `{"data":[{"campaign_id":"c1","spend":"12.34","impressions":"100"}],"paging":{"cursors":{"after":"next"},"next":"https://next"}}`), nil
	})
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	request := AdInsightsRequest{AdAccountID: "123", Metrics: []AdInsightMetric{AdMetricSpend, AdMetricImpressions}, Level: InsightLevelCampaign,
		Timeframe: Timeframe{Since: start, Until: start.AddDate(0, 0, 7)}, TimeIncrement: 1}
	page, err := c.GetAdInsights(context.Background(), request)
	if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
		t.Fatalf("page=%#v err=%v", page, err)
	}
	request.Level = "unknown"
	if _, err := c.GetAdInsights(context.Background(), request); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("level error=%v", err)
	}
	if requests != 1 {
		t.Fatalf("requests=%d", requests)
	}
}
