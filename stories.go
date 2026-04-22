package instagram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// GetStoryTray fetches the viewer's story tray (the row of profile circles
// at the top of the home feed).
//
// Endpoint: GET /api/v1/feed/reels_tray/
func (c *Client) GetStoryTray(ctx context.Context) ([]*StoryReel, error) {
	var resp struct {
		Tray []json.RawMessage `json:"tray"`
		Status string          `json:"status"`
	}
	if err := c.doJSON(ctx, "GET", "/api/v1/feed/reels_tray/", nil, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]*StoryReel, 0, len(resp.Tray))
	for _, raw := range resp.Tray {
		r, err := parseStoryReel(raw)
		if err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// GetUserStories fetches a single user's current story.
//
// Endpoint: GET /api/v1/feed/user/<user_id>/story/
//
// Returns nil if the user has no story.
func (c *Client) GetUserStories(ctx context.Context, userID string) (*StoryReel, error) {
	var resp struct {
		Reel   json.RawMessage `json:"reel"`
		Status string          `json:"status"`
	}
	if err := c.doJSON(ctx, "GET", "/api/v1/feed/user/"+userID+"/story/", nil, nil, &resp); err != nil {
		return nil, err
	}
	if len(resp.Reel) == 0 || string(resp.Reel) == "null" {
		return nil, nil
	}
	return parseStoryReel(resp.Reel)
}

// GetHighlights fetches a user's saved story highlight reels.
//
// Endpoint: GET /api/v1/highlights/<user_id>/highlights_tray/
func (c *Client) GetHighlights(ctx context.Context, userID string) ([]*StoryReel, error) {
	var resp struct {
		Tray   []json.RawMessage `json:"tray"`
		Status string            `json:"status"`
	}
	if err := c.doJSON(ctx, "GET", "/api/v1/highlights/"+userID+"/highlights_tray/", nil, nil, &resp); err != nil {
		return nil, err
	}
	out := make([]*StoryReel, 0, len(resp.Tray))
	for _, raw := range resp.Tray {
		r, err := parseStoryReel(raw)
		if err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, nil
}

// GetReelsMedia fetches the media items for one or more story reels.
//
// reelIDs are user IDs for live reels, or "highlight:<id>" for highlights.
//
// Endpoint: POST /api/v1/feed/reels_media/
func (c *Client) GetReelsMedia(ctx context.Context, reelIDs []string) (map[string]*StoryReel, error) {
	if len(reelIDs) == 0 {
		return map[string]*StoryReel{}, nil
	}
	form := url.Values{}
	for _, id := range reelIDs {
		form.Add("reel_ids", id)
	}
	form.Set("source", "reel_feed_timeline")
	var resp struct {
		Reels  map[string]json.RawMessage `json:"reels"`
		Status string                     `json:"status"`
	}
	if err := c.doJSON(ctx, "POST", "/api/v1/feed/reels_media/", nil, &requestOptions{
		FormBody: form,
	}, &resp); err != nil {
		return nil, err
	}
	out := make(map[string]*StoryReel, len(resp.Reels))
	for k, raw := range resp.Reels {
		r, err := parseStoryReel(raw)
		if err != nil {
			continue
		}
		out[k] = r
	}
	return out, nil
}

// MarkStorySeen marks a story media item as seen by the viewer.
//
// Endpoint: POST /api/v2/media/seen/  with form: reels[<reel_id>][]=<media_pk>_<reel_id>_<taken_at>
//
// Note: subject to write rate-limiting; see ErrWriteSoftBlock.
func (c *Client) MarkStorySeen(ctx context.Context, reelID, mediaPK string, takenAt int64) error {
	if reelID == "" || mediaPK == "" {
		return fmt.Errorf("instagram: MarkStorySeen: reelID and mediaPK required")
	}
	form := url.Values{}
	form.Add("reels["+reelID+"][]", mediaPK+"_"+reelID+"_"+intToString(takenAt))
	form.Set("container_module", "feed_timeline")
	return c.doJSON(ctx, "POST", "/api/v2/media/seen/", nil, &requestOptions{
		IsWrite:  true,
		FormBody: form,
	}, nil)
}

// StoryReel is a story tray item — one user's set of stories or a highlight.
type StoryReel struct {
	ID          string   `json:"id,omitempty"`
	User        *User    `json:"user,omitempty"`
	Title       string   `json:"title,omitempty"`
	Items       []*Story `json:"items,omitempty"`
	HasMore     bool     `json:"-"`
	LatestReelMedia int64 `json:"-"`

	Raw json.RawMessage `json:"-"`
}

func parseStoryReel(raw json.RawMessage) (*StoryReel, error) {
	var aux struct {
		ID    any    `json:"id"`
		Title string `json:"title"`
		User  json.RawMessage `json:"user"`
		Items []json.RawMessage `json:"items"`
		LatestReelMedia int64 `json:"latest_reel_media"`
		HasBesties bool `json:"has_besties_media"`
	}
	if err := json.Unmarshal(raw, &aux); err != nil {
		return nil, fmt.Errorf("%w: parse story reel: %v", ErrUnexpectedResponse, err)
	}
	r := &StoryReel{
		ID:              stringifyID(aux.ID),
		Title:           aux.Title,
		LatestReelMedia: aux.LatestReelMedia,
		Raw:             raw,
	}
	if len(aux.User) > 0 {
		if u, err := parseUser(aux.User); err == nil {
			r.User = u
		}
	}
	for _, item := range aux.Items {
		s, err := parseStory(item)
		if err != nil {
			continue
		}
		r.Items = append(r.Items, s)
	}
	return r, nil
}

func parseStory(raw json.RawMessage) (*Story, error) {
	var aux struct {
		PK         any   `json:"pk"`
		MediaType  int   `json:"media_type"`
		TakenAt    int64 `json:"taken_at"`
		ExpiringAt int64 `json:"expiring_at"`
		User       json.RawMessage `json:"user"`
		ImageVersions2 *struct {
			Candidates []ImageVersion `json:"candidates"`
		} `json:"image_versions2"`
		VideoVersions []VideoVersion `json:"video_versions"`
		Audience string `json:"audience"`
	}
	if err := json.Unmarshal(raw, &aux); err != nil {
		return nil, err
	}
	s := &Story{
		ID:         stringifyID(aux.PK),
		MediaType:  MediaType(aux.MediaType),
		TakenAt:    aux.TakenAt,
		ExpiringAt: aux.ExpiringAt,
		Audience:   aux.Audience,
		Raw:        raw,
	}
	if aux.ImageVersions2 != nil {
		s.ImageVersions = aux.ImageVersions2.Candidates
	}
	s.VideoVersions = aux.VideoVersions
	if len(aux.User) > 0 {
		if u, err := parseUser(aux.User); err == nil {
			s.User = u
		}
	}
	return s, nil
}

func intToString(n int64) string {
	if n == 0 {
		return "0"
	}
	const digits = "0123456789"
	negative := n < 0
	if negative {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = digits[n%10]
		n /= 10
	}
	if negative {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
