package instagram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// GetLocation fetches the metadata for a location by its numeric ID.
//
// Endpoint: GET /api/v1/locations/<id>/info/
func (c *Client) GetLocation(ctx context.Context, id string) (*Location, error) {
	if id == "" {
		return nil, fmt.Errorf("instagram: GetLocation: id required")
	}
	var resp struct {
		Location json.RawMessage `json:"location"`
		Status   string          `json:"status"`
	}
	if err := c.doJSON(ctx, "GET", "/api/v1/locations/"+id+"/info/", nil, nil, &resp); err != nil {
		return nil, err
	}
	if len(resp.Location) == 0 {
		return nil, fmt.Errorf("%w: location %q", ErrNotFound, id)
	}
	var loc Location
	if err := json.Unmarshal(resp.Location, &loc); err != nil {
		return nil, fmt.Errorf("%w: parse location: %v", ErrUnexpectedResponse, err)
	}
	loc.Raw = resp.Location
	return &loc, nil
}

// SearchLocations searches Instagram's location index by free-text query.
//
// Endpoint: GET /api/v1/location_search/?search_query=<q>
func (c *Client) SearchLocations(ctx context.Context, query string) ([]*Location, error) {
	if query == "" {
		return nil, fmt.Errorf("instagram: SearchLocations: query required")
	}
	q := url.Values{}
	q.Set("search_query", query)
	var resp struct {
		Venues []json.RawMessage `json:"venues"`
		Status string            `json:"status"`
	}
	if err := c.doJSON(ctx, "GET", "/api/v1/location_search/", q, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]*Location, 0, len(resp.Venues))
	for _, raw := range resp.Venues {
		var loc Location
		if err := json.Unmarshal(raw, &loc); err == nil {
			loc.Raw = raw
			out = append(out, &loc)
		}
	}
	return out, nil
}

// GetLocationPosts iterates over the recent posts at a location.
//
// Endpoint: GET /api/v1/locations/<id>/sections/?tab=recent
func (c *Client) GetLocationPosts(id string) *Iterator[*Post] {
	return c.locationPosts(id, "recent")
}

// GetLocationTopPosts iterates over the top posts at a location.
//
// Endpoint: GET /api/v1/locations/<id>/sections/?tab=ranked
func (c *Client) GetLocationTopPosts(id string) *Iterator[*Post] {
	return c.locationPosts(id, "ranked")
}

func (c *Client) locationPosts(id, tab string) *Iterator[*Post] {
	return newIterator(func(ctx context.Context, cursor string) (Page[*Post], error) {
		form := url.Values{}
		form.Set("tab", tab)
		if cursor != "" {
			form.Set("max_id", cursor)
			form.Set("page", "1")
			form.Set("next_media_ids", "[]")
		}
		var resp struct {
			Sections []struct {
				LayoutContent struct {
					Medias []struct {
						Media json.RawMessage `json:"media"`
					} `json:"medias"`
				} `json:"layout_content"`
			} `json:"sections"`
			NextMaxID     string `json:"next_max_id"`
			MoreAvailable bool   `json:"more_available"`
			Status        string `json:"status"`
		}
		if err := c.doJSON(ctx, "POST", "/api/v1/locations/"+id+"/sections/", nil, &requestOptions{
			FormBody: form,
		}, &resp); err != nil {
			return Page[*Post]{}, err
		}
		var raws []json.RawMessage
		for _, sec := range resp.Sections {
			for _, m := range sec.LayoutContent.Medias {
				if len(m.Media) > 0 {
					raws = append(raws, m.Media)
				}
			}
		}
		return parsePostList(raws, resp.NextMaxID, resp.MoreAvailable)
	})
}
