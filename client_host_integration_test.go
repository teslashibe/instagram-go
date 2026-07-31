package instagram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"
)

// TestIntegration_MobileKeywordSERP is the transport smoke for issue #7.
// It intentionally uses the same Client that first validates against the WWW
// host, proving browser cookies and the HTTP transport are shared when the
// request switches to the mobile fbsearch host.
func TestIntegration_MobileKeywordSERP(t *testing.T) {
	cookies := Cookies{
		SessionID: os.Getenv("IG_SESSIONID"),
		CSRFToken: os.Getenv("IG_CSRFTOKEN"),
		DSUserID:  os.Getenv("IG_DS_USER_ID"),
		Datr:      os.Getenv("IG_DATR"),
		Mid:       os.Getenv("IG_MID"),
		IgDid:     os.Getenv("IG_DID"),
		Rur:       os.Getenv("IG_RUR"),
		IgNrcb:    os.Getenv("IG_NRCB"),
		PsL:       os.Getenv("IG_PS_L"),
		PsN:       os.Getenv("IG_PS_N"),
		Wd:        os.Getenv("IG_WD"),
	}
	if cookies.SessionID == "" || cookies.CSRFToken == "" || cookies.DSUserID == "" {
		t.Skip("set IG_SESSIONID, IG_CSRFTOKEN, IG_DS_USER_ID to run integration tests")
	}

	c, err := New(cookies)
	if err != nil {
		t.Fatalf("New (WWW session validation): %v", err)
	}
	_, timezoneOffset := time.Now().Zone()
	query := url.Values{
		"query":           {"coffee"},
		"rank_token":      {"00000000-0000-4000-8000-000000000007"},
		"search_surface":  {"top_serp"},
		"timezone_offset": {strconv.Itoa(timezoneOffset)},
	}
	var response struct {
		MediaGrid json.RawMessage `json:"media_grid"`
		Status    string          `json:"status"`
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/fbsearch/top_serp/", query,
		&requestOptions{Host: requestHostAPI}, &response); err != nil {
		t.Fatalf("mobile top_serp: %v", err)
	}
	if response.Status != "ok" {
		t.Fatalf("mobile top_serp status = %q, want ok", response.Status)
	}
	var grid any
	if len(response.MediaGrid) == 0 || json.Unmarshal(response.MediaGrid, &grid) != nil {
		t.Fatal("mobile top_serp returned no valid media_grid")
	}
	mediaCount := countSERPMedia(grid)
	if mediaCount == 0 {
		t.Fatal("mobile top_serp returned an empty media grid/post list")
	}
	t.Logf("PASS: mobile top_serp status=ok with %d media nodes", mediaCount)
}

func countSERPMedia(value any) int {
	switch value := value.(type) {
	case []any:
		count := 0
		for _, item := range value {
			count += countSERPMedia(item)
		}
		return count
	case map[string]any:
		_, hasType := value["media_type"]
		_, hasCode := value["code"]
		_, hasID := value["id"]
		if hasType && hasCode && hasID {
			return 1
		}
		count := 0
		for _, item := range value {
			count += countSERPMedia(item)
		}
		return count
	default:
		return 0
	}
}
