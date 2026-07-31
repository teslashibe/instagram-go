package instagram

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	mobileBaseURL     = "https://i.instagram.com"
	mobileSearchAppID = "567067343352427"
	mobileSearchUA    = "Instagram 321.0.0.0.70 Android (33/13; 420dpi; 1080x2400; Google/google; Pixel 7; panther; panther; en_US; 502001959)"
	keywordPageSize   = 24
)

// AccountSearchResult is one typed page from the mobile account SERP.
// PageToken and RankToken are returned for observability; continuation request
// parameters were not proven by the inventory and are intentionally not sent.
type AccountSearchResult struct {
	Users      []*User `json:"users"`
	NumResults int     `json:"num_results"`
	HasMore    bool    `json:"has_more"`
	PageToken  string  `json:"page_token,omitempty"`
	RankToken  string  `json:"rank_token,omitempty"`
}

// TypeaheadSearchResult is the typed account context returned by the mobile
// keyword typeahead stream.
type TypeaheadSearchResult struct {
	Users     []*User `json:"users"`
	RankToken string  `json:"rank_token,omitempty"`
}

// SearchKeywordPosts iterates over posts from the authenticated web keyword
// search GraphQL connection. It uses the inventory-proven initial persisted
// operation for the first request and the distinct pagination operation for
// subsequent pages.
//
// Persisted document IDs are private, dated contracts and can rotate. A stale
// or malformed GraphQL response returns ErrUnexpectedResponse.
func (c *Client) SearchKeywordPosts(query string) *Iterator[*Post] {
	query = strings.TrimSpace(query)
	var searchSessionID, serpSessionID string
	var sessionErr error

	return newIterator(func(ctx context.Context, cursor string) (Page[*Post], error) {
		if query == "" {
			return Page[*Post]{}, fmt.Errorf("instagram: SearchKeywordPosts: query required")
		}
		if searchSessionID == "" && sessionErr == nil {
			searchSessionID, sessionErr = newSearchSessionID()
			if sessionErr == nil {
				serpSessionID, sessionErr = newSearchSessionID()
			}
		}
		if sessionErr != nil {
			return Page[*Post]{}, fmt.Errorf("instagram: SearchKeywordPosts: create session IDs: %w", sessionErr)
		}

		op := keywordSearchInitialOperation
		var after any
		if cursor != "" {
			op = keywordSearchPaginationOperation
			after = cursor
		}
		variables, err := json.Marshal(struct {
			After           any    `json:"after"`
			First           int    `json:"first"`
			Query           string `json:"query"`
			SearchSessionID string `json:"search_session_id"`
			SERPSessionID   string `json:"serp_session_id"`
		}{
			After:           after,
			First:           keywordPageSize,
			Query:           query,
			SearchSessionID: searchSessionID,
			SERPSessionID:   serpSessionID,
		})
		if err != nil {
			return Page[*Post]{}, fmt.Errorf("instagram: SearchKeywordPosts: encode variables: %w", err)
		}

		form := url.Values{}
		form.Set("__a", "1")
		form.Set("__d", "www")
		form.Set("doc_id", op.DocID)
		form.Set("fb_api_caller_class", "RelayModern")
		form.Set("fb_api_req_friendly_name", op.FriendlyName)
		form.Set("server_timestamps", "true")
		form.Set("variables", string(variables))

		var resp keywordSearchGraphQLResponse
		if err := c.doJSON(ctx, http.MethodPost, "/graphql/query", nil, &requestOptions{
			FormBody: form,
			Referer:  baseURL + "/explore/search/keyword/?q=" + url.QueryEscape(query),
			ExtraHeaders: map[string]string{
				"X-FB-Friendly-Name": op.FriendlyName,
			},
		}, &resp); err != nil {
			return Page[*Post]{}, err
		}
		return resp.page(op)
	})
}

// SearchAccounts searches the inventory-proven mobile account SERP and
// returns richer account card context than SearchUsers.
func (c *Client) SearchAccounts(ctx context.Context, query string) (*AccountSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("instagram: SearchAccounts: query required")
	}
	q := mobileSearchQuery(query, "account_serp")
	var resp struct {
		Users      []json.RawMessage `json:"users"`
		NumResults int               `json:"num_results"`
		HasMore    bool              `json:"has_more"`
		PageToken  string            `json:"page_token"`
		RankToken  string            `json:"rank_token"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/fbsearch/account_serp/", q, mobileSearchRequestOptions(), &resp); err != nil {
		return nil, err
	}
	if resp.Users == nil {
		return nil, fmt.Errorf("%w: account SERP missing users", ErrUnexpectedResponse)
	}
	users, err := parseSearchUsers(resp.Users)
	if err != nil {
		return nil, err
	}
	return &AccountSearchResult{
		Users: users, NumResults: resp.NumResults, HasMore: resp.HasMore,
		PageToken: resp.PageToken, RankToken: resp.RankToken,
	}, nil
}

// SearchTypeaheadUsers fetches account suggestions from the inventory-proven
// mobile keyword typeahead stream. Pass count <= 0 to use the captured value
// of 30.
func (c *Client) SearchTypeaheadUsers(ctx context.Context, query string, count int) (*TypeaheadSearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("instagram: SearchTypeaheadUsers: query required")
	}
	if count <= 0 {
		count = 30
	}
	q := mobileSearchQuery(query, "typeahead_search_page")
	q.Set("context", "blended")
	q.Set("count", strconv.Itoa(count))
	var resp struct {
		Users     []json.RawMessage `json:"users"`
		RankToken string            `json:"rank_token"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/api/v1/fbsearch/typeahead_stream/", q, mobileSearchRequestOptions(), &resp); err != nil {
		return nil, err
	}
	if resp.Users == nil {
		return nil, fmt.Errorf("%w: typeahead stream missing users", ErrUnexpectedResponse)
	}
	users, err := parseSearchUsers(resp.Users)
	if err != nil {
		return nil, err
	}
	return &TypeaheadSearchResult{Users: users, RankToken: resp.RankToken}, nil
}

type keywordSearchGraphQLResponse struct {
	Data *struct {
		Connection *struct {
			Edges []struct {
				Node struct {
					TypeName string            `json:"__typename"`
					Items    []json.RawMessage `json:"items"`
				} `json:"node"`
			} `json:"edges"`
			PageInfo struct {
				HasNextPage bool   `json:"has_next_page"`
				EndCursor   string `json:"end_cursor"`
			} `json:"page_info"`
		} `json:"xdt_fbsearch__top_serp_graphql"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func (r keywordSearchGraphQLResponse) page(op graphqlOperation) (Page[*Post], error) {
	if r.Data == nil || r.Data.Connection == nil {
		detail := "missing keyword connection"
		if len(r.Errors) > 0 && r.Errors[0].Message != "" {
			detail = r.Errors[0].Message
		}
		return Page[*Post]{}, fmt.Errorf("%w: %s (%s)", ErrUnexpectedResponse, detail, op.FriendlyName)
	}
	raws := make([]json.RawMessage, 0)
	for _, edge := range r.Data.Connection.Edges {
		if edge.Node.TypeName != "XDTTopSerpMediaGridUnit" {
			continue
		}
		raws = append(raws, edge.Node.Items...)
	}
	pageInfo := r.Data.Connection.PageInfo
	return parsePostList(raws, pageInfo.EndCursor, pageInfo.HasNextPage && pageInfo.EndCursor != "")
}

func parseSearchUsers(raws []json.RawMessage) ([]*User, error) {
	users := make([]*User, 0, len(raws))
	for _, raw := range raws {
		u, err := parseUser(raw)
		if err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, nil
}

func mobileSearchQuery(query, surface string) url.Values {
	_, timezoneOffset := time.Now().Zone()
	q := url.Values{}
	q.Set("query", query)
	q.Set("search_surface", surface)
	q.Set("timezone_offset", strconv.Itoa(timezoneOffset))
	return q
}

func mobileSearchRequestOptions() *requestOptions {
	return &requestOptions{
		BaseURL: mobileBaseURL,
		ExtraHeaders: map[string]string{
			"User-Agent":           mobileSearchUA,
			"X-IG-App-ID":          mobileSearchAppID,
			"X-IG-Capabilities":    "3brTv10=",
			"X-IG-Connection-Type": "WIFI",
		},
	}
}

func newSearchSessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	// UUID v4 keeps the familiar client-generated session shape without
	// persisting or exposing any captured session value.
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return hex.EncodeToString(raw[0:4]) + "-" +
		hex.EncodeToString(raw[4:6]) + "-" +
		hex.EncodeToString(raw[6:8]) + "-" +
		hex.EncodeToString(raw[8:10]) + "-" +
		hex.EncodeToString(raw[10:16]), nil
}
