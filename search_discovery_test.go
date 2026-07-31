package instagram_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	instagram "github.com/teslashibe/instagram-go"
)

type discoveryRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn discoveryRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func newDiscoveryClient(t *testing.T, fn discoveryRoundTripFunc, opts ...instagram.Option) *instagram.Client {
	t.Helper()
	options := []instagram.Option{
		instagram.WithHTTPClient(&http.Client{Transport: fn}),
		instagram.WithSkipSessionValidation(),
		instagram.WithMinRequestGap(0),
		instagram.WithRetry(1, 0),
	}
	options = append(options, opts...)
	c, err := instagram.New(instagram.Cookies{
		SessionID: "session", CSRFToken: "csrf", DSUserID: "viewer",
	}, options...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestDiscoveryMethodsHonorConfiguredHostsAndRequestProfiles(t *testing.T) {
	const (
		wwwHost = "https://web-proxy.instagram.test"
		apiHost = "https://mobile-proxy.instagram.test"
	)
	type requestRecord struct {
		host    string
		path    string
		header  http.Header
		referer string
	}
	var records []requestRecord
	c := newDiscoveryClient(t, func(req *http.Request) (*http.Response, error) {
		records = append(records, requestRecord{
			host: req.URL.Host, path: req.URL.Path, header: req.Header.Clone(), referer: req.Referer(),
		})
		switch req.URL.Path {
		case "/graphql/query":
			return jsonResponse(req, http.StatusOK, []byte(`{"data":{"xdt_fbsearch__top_serp_graphql":{"edges":[],"page_info":{"has_next_page":false,"end_cursor":""}}}}`)), nil
		case "/api/v1/fbsearch/account_serp/", "/api/v1/fbsearch/typeahead_stream/":
			return jsonResponse(req, http.StatusOK, []byte(`{"users":[],"status":"ok"}`)), nil
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL)
			return nil, nil
		}
	},
		instagram.WithWWWHost(wwwHost),
		instagram.WithAPIHost(apiHost),
		instagram.WithAPIUserAgent("configured-mobile-ua"),
		instagram.WithAPIAppID("configured-mobile-app"),
	)

	if _, err := c.SearchKeywordPosts("coffee").Collect(context.Background()); err != nil {
		t.Fatalf("SearchKeywordPosts: %v", err)
	}
	if _, err := c.SearchAccounts(context.Background(), "coffee"); err != nil {
		t.Fatalf("SearchAccounts: %v", err)
	}
	if _, err := c.SearchTypeaheadUsers(context.Background(), "coffee", 5); err != nil {
		t.Fatalf("SearchTypeaheadUsers: %v", err)
	}
	if len(records) != 3 {
		t.Fatalf("recorded %d requests, want 3", len(records))
	}

	graphql := records[0]
	if graphql.host != "web-proxy.instagram.test" || graphql.path != "/graphql/query" {
		t.Errorf("GraphQL route = %s%s", graphql.host, graphql.path)
	}
	if want := wwwHost + "/explore/search/keyword/?q=coffee"; graphql.referer != want {
		t.Errorf("GraphQL Referer = %q, want %q", graphql.referer, want)
	}
	if got := graphql.header.Get("Origin"); got != wwwHost {
		t.Errorf("GraphQL Origin = %q, want %q", got, wwwHost)
	}

	for _, mobile := range records[1:] {
		if mobile.host != "mobile-proxy.instagram.test" {
			t.Errorf("mobile route = %s%s", mobile.host, mobile.path)
		}
		if got := mobile.header.Get("User-Agent"); got != "configured-mobile-ua" {
			t.Errorf("%s User-Agent = %q", mobile.path, got)
		}
		if got := mobile.header.Get("X-IG-App-ID"); got != "configured-mobile-app" {
			t.Errorf("%s X-IG-App-ID = %q", mobile.path, got)
		}
		if got := mobile.header.Get("X-IG-Capabilities"); got != "3brTv10=" {
			t.Errorf("%s X-IG-Capabilities = %q", mobile.path, got)
		}
		for _, name := range []string{"Origin", "Referer", "X-IG-WWW-Claim", "X-Requested-With"} {
			if got := mobile.header.Get(name); got != "" {
				t.Errorf("%s %s = %q, want empty", mobile.path, name, got)
			}
		}
	}
}

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return body
}

func jsonResponse(req *http.Request, status int, body []byte) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewReader(body)),
		Request:    req,
	}
}

func TestSearchKeywordPostsMapsInitialAndPaginationFixtures(t *testing.T) {
	initial := fixture(t, "keyword_search_graphql_response.json")
	continuation := fixture(t, "keyword_search_graphql_pagination_response.json")

	var mu sync.Mutex
	var forms []url.Values
	c := newDiscoveryClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost || req.URL.Host != "www.instagram.com" || req.URL.Path != "/graphql/query" {
			t.Errorf("unexpected request: %s %s", req.Method, req.URL.String())
		}
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read form: %v", err)
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("parse form: %v", err)
		}
		mu.Lock()
		forms = append(forms, form)
		mu.Unlock()
		switch form.Get("doc_id") {
		case "26586987494245638":
			return jsonResponse(req, http.StatusOK, initial), nil
		case "26577336451926911":
			return jsonResponse(req, http.StatusOK, continuation), nil
		default:
			t.Fatalf("unexpected doc_id %q", form.Get("doc_id"))
			return nil, nil
		}
	})

	posts, err := c.SearchKeywordPosts("coffee").Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(posts) != 2 {
		t.Fatalf("got %d posts, want 2", len(posts))
	}
	if posts[0].Code != "SHORTCODE_REDACTED" || posts[0].Owner == nil || posts[0].Owner.Username != "username_redacted" {
		t.Fatalf("unexpected initial post mapping: %#v", posts[0])
	}
	if posts[0].OriginalWidth != 1080 || len(posts[0].VideoVersions) != 1 || posts[0].Caption != "CAPTION_REDACTED" {
		t.Fatalf("missing rich initial fields: %#v", posts[0])
	}
	if posts[1].Code != "SECOND_SHORTCODE_REDACTED" || posts[1].MediaType != instagram.MediaTypePhoto {
		t.Fatalf("unexpected continuation post mapping: %#v", posts[1])
	}

	if len(forms) != 2 {
		t.Fatalf("got %d GraphQL requests, want 2", len(forms))
	}
	wantFriendly := []string{
		"PolarisKeywordSearchExplorePageRelayQuery",
		"PolarisKeywordSearchExplorePageRelayPaginationQuery",
	}
	var sessionIDs [][2]string
	for i, form := range forms {
		if form.Get("fb_api_req_friendly_name") != wantFriendly[i] || form.Get("__a") != "1" || form.Get("__d") != "www" {
			t.Errorf("request %d transport fields: %#v", i, form)
		}
		var variables map[string]any
		if err := json.Unmarshal([]byte(form.Get("variables")), &variables); err != nil {
			t.Fatalf("request %d variables: %v", i, err)
		}
		keys := make([]string, 0, len(variables))
		for key := range variables {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		wantKeys := []string{"after", "first", "query", "search_session_id", "serp_session_id"}
		if !reflect.DeepEqual(keys, wantKeys) {
			t.Errorf("request %d variable names = %v, want %v", i, keys, wantKeys)
		}
		sessionIDs = append(sessionIDs, [2]string{variables["search_session_id"].(string), variables["serp_session_id"].(string)})
		if variables["query"] != "coffee" || variables["first"] != float64(24) {
			t.Errorf("request %d variables: %#v", i, variables)
		}
		if i == 0 && variables["after"] != nil {
			t.Errorf("initial after = %#v, want nil", variables["after"])
		}
		if i == 1 && variables["after"] != "END_CURSOR_REDACTED" {
			t.Errorf("pagination after = %#v", variables["after"])
		}
	}
	if sessionIDs[0] != sessionIDs[1] || sessionIDs[0][0] == "" || sessionIDs[0][1] == "" {
		t.Errorf("session IDs were not stable across pages: %#v", sessionIDs)
	}
}

func TestSearchKeywordPostsSkipsEmptyIntermediateFixture(t *testing.T) {
	emptyIntermediate := fixture(t, "keyword_search_graphql_empty_intermediate_response.json")
	continuation := fixture(t, "keyword_search_graphql_pagination_response.json")

	var requests int
	c := newDiscoveryClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		body, err := io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read form: %v", err)
		}
		form, err := url.ParseQuery(string(body))
		if err != nil {
			t.Fatalf("parse form: %v", err)
		}
		var variables map[string]any
		if err := json.Unmarshal([]byte(form.Get("variables")), &variables); err != nil {
			t.Fatalf("variables: %v", err)
		}
		switch requests {
		case 1:
			if form.Get("doc_id") != "26586987494245638" || variables["after"] != nil {
				t.Fatalf("initial request = %#v, variables %#v", form, variables)
			}
			return jsonResponse(req, http.StatusOK, emptyIntermediate), nil
		case 2:
			if form.Get("doc_id") != "26577336451926911" || variables["after"] != "EMPTY_PAGE_END_CURSOR_REDACTED" {
				t.Fatalf("continuation request = %#v, variables %#v", form, variables)
			}
			return jsonResponse(req, http.StatusOK, continuation), nil
		default:
			t.Fatalf("unexpected request %d", requests)
			return nil, nil
		}
	})

	posts, err := c.SearchKeywordPosts("coffee").Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if requests != 2 {
		t.Fatalf("got %d requests, want 2", requests)
	}
	if len(posts) != 1 || posts[0].Code != "SECOND_SHORTCODE_REDACTED" {
		t.Fatalf("posts = %#v, want continuation media", posts)
	}
}

func TestSearchKeywordPostsRejectsStalledEmptyPageCursor(t *testing.T) {
	var requests int
	c := newDiscoveryClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		body := []byte(`{
			"data": {
				"xdt_fbsearch__top_serp_graphql": {
					"edges": [],
					"page_info": {"has_next_page": true, "end_cursor": "STALLED_CURSOR"}
				}
			}
		}`)
		return jsonResponse(req, http.StatusOK, body), nil
	})

	it := c.SearchKeywordPosts("coffee")
	if it.Next(context.Background()) {
		t.Fatal("unexpected post")
	}
	if !errors.Is(it.Err(), instagram.ErrUnexpectedResponse) || !strings.Contains(it.Err().Error(), "cursor did not advance") {
		t.Fatalf("got %v, want stalled-cursor ErrUnexpectedResponse", it.Err())
	}
	if requests != 2 {
		t.Fatalf("got %d requests, want 2", requests)
	}
}

func TestSearchPostsMapsTopSERPLayoutsAndResumesCursor(t *testing.T) {
	var requests int
	var firstRankToken string
	c := newDiscoveryClient(t, func(req *http.Request) (*http.Response, error) {
		requests++
		q := req.URL.Query()
		if req.Method != http.MethodGet || req.URL.Host != "i.instagram.com" || req.URL.Path != "/api/v1/fbsearch/top_serp/" {
			t.Errorf("unexpected request: %s %s", req.Method, req.URL.String())
		}
		if q.Get("query") != "billing automation" || q.Get("search_surface") != "top_serp" || q.Get("timezone_offset") == "" {
			t.Errorf("unexpected query: %s", req.URL.RawQuery)
		}
		if req.Header.Get("X-IG-App-ID") != "567067343352427" || req.Header.Get("X-IG-Capabilities") != "3brTv10=" {
			t.Errorf("missing mobile headers: %#v", req.Header)
		}
		switch requests {
		case 1:
			firstRankToken = q.Get("rank_token")
			if firstRankToken == "" || q.Get("next_max_id") != "" || q.Get("reels_max_id") != "" {
				t.Errorf("initial cursors: %s", req.URL.RawQuery)
			}
			body := []byte(`{
				"media_grid": {
					"sections": [
						{"layout_content":{"medias":[{"media":{"pk":"101","id":"101_9","code":"PHOTO101","media_type":1,"product_type":"feed","caption":{"text":"first #billing"},"user":{"pk":"9","username":"creator"}}}]}},
						{"layout_content":{"fill_items":[{"media":{"pk":"102","id":"102_9","code":"REEL102","media_type":2,"product_type":"clips","caption":{"text":"second"},"user":{"pk":"9","username":"creator"}}}]}},
						{"layout_content":{"one_by_two_item":{"media":{"pk":"103","id":"103_9","code":"PHOTO103","media_type":1,"user":{"pk":"9","username":"creator"}},"clips":{"items":[{"media":{"pk":"104","id":"104_9","code":"REEL104","media_type":2,"product_type":"clips","user":{"pk":"9","username":"creator"}}},{"media":{"pk":"102","id":"102_9","code":"REEL102","media_type":2,"product_type":"clips"}}]}}}}
					],
					"has_more": true,
					"next_max_id": "NEXT_1",
					"reels_max_id": "REELS_1",
					"rank_token": "RANK_1"
				},
				"rank_token": "TOP_LEVEL_RANK",
				"status": "ok"
			}`)
			return jsonResponse(req, http.StatusOK, body), nil
		case 2:
			if q.Get("next_max_id") != "NEXT_1" || q.Get("reels_max_id") != "REELS_1" || q.Get("rank_token") != "RANK_1" {
				t.Errorf("continuation cursors not preserved: %s", req.URL.RawQuery)
			}
			body := []byte(`{"media_grid":{"sections":[{"layout_content":{"fill_items":[{"media":{"pk":"105","id":"105_9","code":"PHOTO105","media_type":1,"caption":{"text":"next page"},"user":{"pk":"9","username":"creator"}}}]}}],"has_more":false,"next_max_id":"STALE"},"status":"ok"}`)
			return jsonResponse(req, http.StatusOK, body), nil
		default:
			t.Fatalf("unexpected request %d", requests)
			return nil, nil
		}
	})

	first := c.SearchPosts(" billing automation ").WithMaxPages(1)
	firstPage, err := first.Collect(context.Background())
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(firstPage) != 4 {
		t.Fatalf("got %d unique first-page posts, want 4", len(firstPage))
	}
	for _, post := range firstPage {
		if post.PK == "" || post.Code == "" || post.Owner == nil || post.PermalinkURL == "" {
			t.Fatalf("incomplete post mapping: %#v", post)
		}
	}
	if firstPage[0].Caption != "first #billing" || !reflect.DeepEqual(firstPage[0].Hashtags, []string{"billing"}) {
		t.Fatalf("caption mapping = %#v", firstPage[0])
	}
	if firstPage[1].PermalinkURL != "https://www.instagram.com/reel/REEL102/" {
		t.Fatalf("reel permalink = %q", firstPage[1].PermalinkURL)
	}
	if firstRankToken == "RANK_1" {
		t.Fatal("test did not prove the response rank token replaced the generated token")
	}

	cursor := first.Cursor()
	if cursor == "" || strings.Contains(cursor, "NEXT_1") || strings.Contains(cursor, "RANK_1") {
		t.Fatalf("cursor is not opaque: %q", cursor)
	}
	second := c.SearchPosts("billing automation").WithCursor(cursor).WithMaxPages(1)
	secondPage, err := second.Collect(context.Background())
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(secondPage) != 1 || secondPage[0].Code != "PHOTO105" {
		t.Fatalf("second page = %#v", secondPage)
	}
	if second.Cursor() != "" {
		t.Fatalf("terminal cursor = %q, want empty", second.Cursor())
	}
}

func TestSearchPostsRejectsInvalidPaginationWithoutExtraRequest(t *testing.T) {
	tests := []struct {
		name   string
		cursor string
		body   string
	}{
		{name: "invalid cursor", cursor: "not-a-search-posts-cursor"},
		{name: "missing pagination ID", body: `{"media_grid":{"sections":[],"has_more":true,"rank_token":"rank"},"status":"ok"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requests := 0
			c := newDiscoveryClient(t, func(req *http.Request) (*http.Response, error) {
				requests++
				return jsonResponse(req, http.StatusOK, []byte(tt.body)), nil
			})
			it := c.SearchPosts("coffee").WithCursor(tt.cursor)
			if it.Next(context.Background()) {
				t.Fatal("unexpected post")
			}
			if it.Err() == nil {
				t.Fatal("expected pagination error")
			}
			if tt.cursor != "" && requests != 0 {
				t.Fatalf("invalid cursor performed %d requests", requests)
			}
			if tt.cursor == "" && !errors.Is(it.Err(), instagram.ErrUnexpectedResponse) {
				t.Fatalf("got %v, want ErrUnexpectedResponse", it.Err())
			}
		})
	}
}

func TestSearchAccountsMapsFixtureAndMobileContract(t *testing.T) {
	body := fixture(t, "account_serp_response.json")
	c := newDiscoveryClient(t, func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet || req.URL.Host != "i.instagram.com" || req.URL.Path != "/api/v1/fbsearch/account_serp/" {
			t.Errorf("unexpected request: %s %s", req.Method, req.URL.String())
		}
		if req.URL.Query().Get("query") != "coffee" || req.URL.Query().Get("search_surface") != "account_serp" || req.URL.Query().Get("timezone_offset") == "" {
			t.Errorf("unexpected query: %s", req.URL.RawQuery)
		}
		if req.Header.Get("X-IG-App-ID") != "567067343352427" || !strings.HasPrefix(req.Header.Get("User-Agent"), "Instagram 321.") {
			t.Errorf("missing captured mobile headers: %#v", req.Header)
		}
		return jsonResponse(req, http.StatusOK, body), nil
	})

	result, err := c.SearchAccounts(context.Background(), " coffee ")
	if err != nil {
		t.Fatalf("SearchAccounts: %v", err)
	}
	if result.NumResults != 1 || !result.HasMore || result.PageToken != "PAGE_TOKEN_REDACTED" || result.RankToken != "RANK_TOKEN_REDACTED" {
		t.Fatalf("unexpected page mapping: %#v", result)
	}
	if len(result.Users) != 1 {
		t.Fatalf("got %d users", len(result.Users))
	}
	u := result.Users[0]
	if u.ID != "ACCOUNT_PK_ID_REDACTED" || u.Username != "coffee_account_redacted" || !u.IsVerified || !u.IsSearchBoosted {
		t.Fatalf("unexpected user mapping: %#v", u)
	}
	if u.SearchSERPType != "user" || u.SearchSocialContext == "" || u.FriendshipStatus == nil || !u.FriendshipStatus.FollowedBy {
		t.Fatalf("missing account context: %#v", u)
	}
}

func TestSearchTypeaheadUsersMapsFixtureAndDefaultsCount(t *testing.T) {
	body := fixture(t, "typeahead_stream_response.json")
	c := newDiscoveryClient(t, func(req *http.Request) (*http.Response, error) {
		q := req.URL.Query()
		if req.Method != http.MethodGet || req.URL.Host != "i.instagram.com" || req.URL.Path != "/api/v1/fbsearch/typeahead_stream/" {
			t.Errorf("unexpected request: %s %s", req.Method, req.URL.String())
		}
		if q.Get("query") != "coffee" || q.Get("search_surface") != "typeahead_search_page" || q.Get("context") != "blended" || q.Get("count") != "30" {
			t.Errorf("unexpected query: %s", req.URL.RawQuery)
		}
		return jsonResponse(req, http.StatusOK, body), nil
	})

	result, err := c.SearchTypeaheadUsers(context.Background(), "coffee", 0)
	if err != nil {
		t.Fatalf("SearchTypeaheadUsers: %v", err)
	}
	if result.RankToken != "TYPEAHEAD_RANK_TOKEN_REDACTED" || len(result.Users) != 1 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if result.Users[0].ID != "TYPEAHEAD_PK_REDACTED" || result.Users[0].Username != "coffee_typeahead_redacted" {
		t.Fatalf("unexpected user: %#v", result.Users[0])
	}
}

func TestDiscoveryMethodsReturnExistingAuthSentinels(t *testing.T) {
	methods := []struct {
		name string
		call func(*instagram.Client) error
	}{
		{name: "keyword posts", call: func(c *instagram.Client) error {
			it := c.SearchKeywordPosts("coffee")
			it.Next(context.Background())
			return it.Err()
		}},
		{name: "top SERP posts", call: func(c *instagram.Client) error {
			it := c.SearchPosts("coffee")
			it.Next(context.Background())
			return it.Err()
		}},
		{name: "accounts", call: func(c *instagram.Client) error {
			_, err := c.SearchAccounts(context.Background(), "coffee")
			return err
		}},
		{name: "typeahead", call: func(c *instagram.Client) error {
			_, err := c.SearchTypeaheadUsers(context.Background(), "coffee", 10)
			return err
		}},
	}
	cases := []struct {
		name     string
		status   int
		location string
		body     string
		want     error
	}{
		{name: "unauthenticated", status: http.StatusUnauthorized, body: `{"message":"login_required","status":"fail"}`, want: instagram.ErrInvalidAuth},
		{name: "expired", status: http.StatusFound, location: "https://www.instagram.com/accounts/login/", body: `{"message":"login_required","status":"fail"}`, want: instagram.ErrSessionExpired},
		{name: "challenge", status: http.StatusOK, body: `{"message":"challenge_required","status":"fail"}`, want: instagram.ErrChallengeRequired},
	}
	for _, method := range methods {
		for _, tc := range cases {
			t.Run(method.name+"/"+tc.name, func(t *testing.T) {
				c := newDiscoveryClient(t, func(req *http.Request) (*http.Response, error) {
					resp := jsonResponse(req, tc.status, []byte(tc.body))
					if tc.location != "" {
						resp.Header.Set("Location", tc.location)
					}
					return resp, nil
				})
				err := method.call(c)
				if !errors.Is(err, tc.want) {
					t.Fatalf("got %v, want errors.Is(%v)", err, tc.want)
				}
			})
		}
	}
}

func TestDiscoveryMethodsParticipateInReadRateLimit(t *testing.T) {
	methods := []struct {
		name string
		call func(*instagram.Client) error
	}{
		{name: "keyword posts", call: func(c *instagram.Client) error {
			it := c.SearchKeywordPosts("coffee")
			it.Next(context.Background())
			return it.Err()
		}},
		{name: "top SERP posts", call: func(c *instagram.Client) error {
			it := c.SearchPosts("coffee")
			it.Next(context.Background())
			return it.Err()
		}},
		{name: "accounts", call: func(c *instagram.Client) error {
			_, err := c.SearchAccounts(context.Background(), "coffee")
			return err
		}},
		{name: "typeahead", call: func(c *instagram.Client) error {
			_, err := c.SearchTypeaheadUsers(context.Background(), "coffee", 10)
			return err
		}},
	}
	for _, method := range methods {
		t.Run(method.name, func(t *testing.T) {
			c := newDiscoveryClient(t, func(req *http.Request) (*http.Response, error) {
				return jsonResponse(req, http.StatusTooManyRequests, []byte(`{"message":"throttled"}`)), nil
			})
			err := method.call(c)
			if !errors.Is(err, instagram.ErrRateLimited) {
				t.Fatalf("got %v, want ErrRateLimited", err)
			}
			if c.RateLimit().CooldownReadUntil.IsZero() {
				t.Fatal("read cooldown was not recorded")
			}
		})
	}
}

func TestSearchKeywordPostsMapsGraphQLErrors(t *testing.T) {
	c := newDiscoveryClient(t, func(req *http.Request) (*http.Response, error) {
		return jsonResponse(req, http.StatusOK, []byte(`{"data":null,"errors":[{"message":"PersistedQueryNotFound"}]}`)), nil
	})
	it := c.SearchKeywordPosts("coffee")
	if it.Next(context.Background()) {
		t.Fatal("unexpected post")
	}
	if !errors.Is(it.Err(), instagram.ErrUnexpectedResponse) || !strings.Contains(it.Err().Error(), "PersistedQueryNotFound") {
		t.Fatalf("got %v, want ErrUnexpectedResponse with GraphQL detail", it.Err())
	}
}

func TestDiscoveryMethodsValidateQueriesWithoutHTTP(t *testing.T) {
	c := newDiscoveryClient(t, func(req *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected HTTP request: %s", req.URL)
		return nil, nil
	})
	it := c.SearchKeywordPosts("  ")
	if it.Next(context.Background()) || it.Err() == nil {
		t.Fatalf("SearchKeywordPosts empty query error = %v", it.Err())
	}
	posts := c.SearchPosts("  ")
	if posts.Next(context.Background()) || posts.Err() == nil || !strings.Contains(posts.Err().Error(), "query required") {
		t.Fatalf("SearchPosts empty query error = %v", posts.Err())
	}
	if _, err := c.SearchAccounts(context.Background(), ""); err == nil {
		t.Fatal("SearchAccounts accepted empty query")
	}
	if _, err := c.SearchTypeaheadUsers(context.Background(), "", 1); err == nil {
		t.Fatal("SearchTypeaheadUsers accepted empty query")
	}
}
