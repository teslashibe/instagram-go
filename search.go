package instagram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// SearchUsers searches Instagram for users matching a query.
//
// Endpoint: GET /api/v1/users/search/?q=<query>&count=<n>
//
// count clamps to 50 server-side; pass 0 to use the default (~12).
func (c *Client) SearchUsers(ctx context.Context, query string, count int) ([]*User, error) {
	if query == "" {
		return nil, fmt.Errorf("instagram: SearchUsers: query required")
	}
	q := url.Values{}
	q.Set("q", query)
	if count > 0 {
		q.Set("count", strconv.Itoa(count))
	}
	var resp struct {
		Users      []json.RawMessage `json:"users"`
		NumResults int               `json:"num_results"`
		Status     string            `json:"status"`
	}
	if err := c.doJSON(ctx, "GET", "/api/v1/users/search/", q, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]*User, 0, len(resp.Users))
	for _, raw := range resp.Users {
		u, err := parseUser(raw)
		if err != nil {
			continue
		}
		out = append(out, u)
	}
	return out, nil
}

// Search runs a topsearch across users, hashtags, and places.
//
// Endpoint: GET /api/v1/web/search/topsearch/?context=blended&query=<q>
func (c *Client) Search(ctx context.Context, query string) (*SearchResult, error) {
	if query == "" {
		return nil, fmt.Errorf("instagram: Search: query required")
	}
	q := url.Values{}
	q.Set("context", "blended")
	q.Set("query", query)
	q.Set("rank_token", "")
	q.Set("include_reel", "true")
	var resp struct {
		Users []struct {
			User json.RawMessage `json:"user"`
		} `json:"users"`
		Hashtags []struct {
			Hashtag json.RawMessage `json:"hashtag"`
		} `json:"hashtags"`
		Places []json.RawMessage `json:"places"`
		Status string            `json:"status"`
	}
	if err := c.doJSON(ctx, "GET", "/api/v1/web/search/topsearch/", q, nil, &resp); err != nil {
		return nil, err
	}
	result := &SearchResult{}
	for _, e := range resp.Users {
		if u, err := parseUser(e.User); err == nil {
			result.Users = append(result.Users, u)
		}
	}
	for _, e := range resp.Hashtags {
		var h Hashtag
		if err := json.Unmarshal(e.Hashtag, &h); err == nil {
			h.Raw = e.Hashtag
			result.Hashtags = append(result.Hashtags, &h)
		}
	}
	for _, raw := range resp.Places {
		var p struct {
			Title    string          `json:"title"`
			Subtitle string          `json:"subtitle"`
			Location json.RawMessage `json:"location"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			continue
		}
		place := &Place{Title: p.Title, Subtitle: p.Subtitle}
		if len(p.Location) > 0 {
			var loc Location
			if err := json.Unmarshal(p.Location, &loc); err == nil {
				loc.Raw = p.Location
				place.Location = &loc
			}
		}
		result.Places = append(result.Places, place)
	}
	return result, nil
}

// GetSuggestedUsers returns up to ~80 accounts Instagram suggests based on
// the given seed user (typically the logged-in user's ID for "Suggested for
// you", but any user ID works to get accounts related to that user).
//
// Endpoint: GET /api/v1/discover/chaining/?target_id=<user_id>
func (c *Client) GetSuggestedUsers(ctx context.Context, targetID string) ([]*User, error) {
	if targetID == "" {
		return nil, fmt.Errorf("instagram: GetSuggestedUsers: targetID required")
	}
	q := url.Values{}
	q.Set("target_id", targetID)
	var resp struct {
		Users  []json.RawMessage `json:"users"`
		Status string            `json:"status"`
	}
	if err := c.doJSON(ctx, "GET", "/api/v1/discover/chaining/", q, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]*User, 0, len(resp.Users))
	for _, raw := range resp.Users {
		if u, err := parseUser(raw); err == nil {
			out = append(out, u)
		}
	}
	return out, nil
}
