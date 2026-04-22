package instagram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
)

// GetProfile fetches a user's full profile by username.
//
// Endpoint: GET /api/v1/users/web_profile_info/?username=<username>
func (c *Client) GetProfile(ctx context.Context, username string) (*User, error) {
	if username == "" {
		return nil, fmt.Errorf("instagram: GetProfile: username required")
	}
	q := url.Values{}
	q.Set("username", username)

	var resp struct {
		Data struct {
			User json.RawMessage `json:"user"`
		} `json:"data"`
		Status string `json:"status"`
	}
	if err := c.doJSON(ctx, "GET", "/api/v1/users/web_profile_info/", q, &requestOptions{
		Referer: baseURL + "/" + username + "/",
	}, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data.User) == 0 || string(resp.Data.User) == "null" {
		return nil, fmt.Errorf("%w: user %q", ErrNotFound, username)
	}
	return parseUser(resp.Data.User)
}

// GetProfileByID fetches a user's full profile by their numeric ID.
//
// Endpoint: GET /api/v1/users/<user_id>/info/
func (c *Client) GetProfileByID(ctx context.Context, userID string) (*User, error) {
	if userID == "" {
		return nil, fmt.Errorf("instagram: GetProfileByID: userID required")
	}
	var resp struct {
		User   json.RawMessage `json:"user"`
		Status string          `json:"status"`
	}
	if err := c.doJSON(ctx, "GET", "/api/v1/users/"+userID+"/info/", nil, nil, &resp); err != nil {
		return nil, err
	}
	if len(resp.User) == 0 || string(resp.User) == "null" {
		return nil, fmt.Errorf("%w: user_id %q", ErrNotFound, userID)
	}
	return parseUser(resp.User)
}

// currentUser fetches the authenticated user via the by-id info endpoint
// using DSUserID from the cookie set. Used for session validation in New.
//
// Note: /api/v1/accounts/current_user/ is more restricted on web sessions
// and frequently returns 302-to-login even with a healthy session, so we
// use the regular profile endpoint here.
func (c *Client) currentUser(ctx context.Context) (*User, error) {
	if c.cookies.DSUserID == "" {
		return nil, fmt.Errorf("%w: DSUserID required for session validation", ErrInvalidAuth)
	}
	return c.GetProfileByID(ctx, c.cookies.DSUserID)
}

// parseUser unmarshals a user payload (possibly with numeric or string pk)
// into a User struct, preserving the raw JSON for callers that need extra fields.
func parseUser(raw json.RawMessage) (*User, error) {
	// Local view — also captures the alternate ID fields Instagram uses.
	var aux struct {
		PK     any    `json:"pk"`
		PKID   string `json:"pk_id"`
		ID     any    `json:"id"`
		UserID any    `json:"user_id"`

		Username       string `json:"username"`
		FullName       string `json:"full_name"`
		Biography      string `json:"biography"`
		ExternalURL    string `json:"external_url"`
		ProfilePicURL  string `json:"profile_pic_url"`
		ProfilePicHD   string `json:"profile_pic_url_hd"`
		IsPrivate      bool   `json:"is_private"`
		IsVerified     bool   `json:"is_verified"`
		IsBusiness     bool   `json:"is_business"`
		IsProfessional bool   `json:"is_professional_account"`
		BusinessCat    string `json:"business_category_name"`
		Category       string `json:"category_name"`

		FollowerCount  any `json:"follower_count"`
		FollowingCount any `json:"following_count"`
		MediaCount     any `json:"media_count"`
		TotalIGTV      any `json:"total_igtv_videos"`

		HasReels   bool `json:"has_clips"`
		HasGuides  bool `json:"has_guides"`
		HasChain   bool `json:"has_chaining"`
		HasReelsHL bool `json:"has_highlight_reels"`

		HideLikeViewCnt bool `json:"hide_like_and_view_counts"`

		PublicEmail   string `json:"public_email"`
		PublicPhone   string `json:"public_phone_number"`
		ContactPhone  string `json:"contact_phone_number"`
		AddressStreet string `json:"address_street"`
		City          string `json:"city_name"`
		Zip           string `json:"zip"`

		AccountType any `json:"account_type"`

		Pronouns []string `json:"pronouns"`

		Friendship *FriendshipStatus `json:"friendship_status"`

		// /web_profile_info/ uses edge_followed_by/edge_follow with .count.
		EdgeFollowedBy *struct {
			Count int `json:"count"`
		} `json:"edge_followed_by"`
		EdgeFollow *struct {
			Count int `json:"count"`
		} `json:"edge_follow"`
		EdgeOwnerToTimelineMedia *struct {
			Count int `json:"count"`
		} `json:"edge_owner_to_timeline_media"`
	}
	if err := json.Unmarshal(raw, &aux); err != nil {
		return nil, fmt.Errorf("%w: parse user: %v", ErrUnexpectedResponse, err)
	}

	id := stringifyID(aux.PKID, aux.PK, aux.ID, aux.UserID)
	if id == "" {
		return nil, fmt.Errorf("%w: user payload missing id", ErrUnexpectedResponse)
	}

	follower := anyToInt(aux.FollowerCount)
	following := anyToInt(aux.FollowingCount)
	media := anyToInt(aux.MediaCount)

	if follower == 0 && aux.EdgeFollowedBy != nil {
		follower = aux.EdgeFollowedBy.Count
	}
	if following == 0 && aux.EdgeFollow != nil {
		following = aux.EdgeFollow.Count
	}
	if media == 0 && aux.EdgeOwnerToTimelineMedia != nil {
		media = aux.EdgeOwnerToTimelineMedia.Count
	}

	return &User{
		ID:                  id,
		Username:            aux.Username,
		FullName:            aux.FullName,
		Biography:           aux.Biography,
		ExternalURL:         aux.ExternalURL,
		ProfilePicURL:       aux.ProfilePicURL,
		ProfilePicURLHD:     aux.ProfilePicHD,
		IsPrivate:           aux.IsPrivate,
		IsVerified:          aux.IsVerified,
		IsBusiness:          aux.IsBusiness,
		IsProfessional:      aux.IsProfessional,
		BusinessCategory:    aux.BusinessCat,
		Category:            aux.Category,
		FollowerCount:       follower,
		FollowingCount:      following,
		MediaCount:          media,
		TotalIGTVCount:      anyToInt(aux.TotalIGTV),
		HasReels:            aux.HasReels,
		HasGuides:           aux.HasGuides,
		HasChaining:         aux.HasChain,
		HasHighlightReels:   aux.HasReelsHL,
		HideLikeAndViewCnts: aux.HideLikeViewCnt,
		PublicEmail:         aux.PublicEmail,
		PublicPhone:         aux.PublicPhone,
		ContactPhone:        aux.ContactPhone,
		AddressStreet:       aux.AddressStreet,
		City:                aux.City,
		Zip:                 aux.Zip,
		AccountType:         anyToInt(aux.AccountType),
		Pronouns:            aux.Pronouns,
		FriendshipStatus:    aux.Friendship,
		Raw:                 raw,
	}, nil
}

// stringifyID returns the first non-empty ID, normalised to string.
func stringifyID(vals ...any) string {
	for _, v := range vals {
		switch t := v.(type) {
		case string:
			if t != "" {
				return t
			}
		case float64:
			if t != 0 {
				return strconv.FormatInt(int64(t), 10)
			}
		case json.Number:
			if s := t.String(); s != "" && s != "0" {
				return s
			}
		case int:
			if t != 0 {
				return strconv.Itoa(t)
			}
		case int64:
			if t != 0 {
				return strconv.FormatInt(t, 10)
			}
		}
	}
	return ""
}

func anyToInt(v any) int {
	switch t := v.(type) {
	case nil:
		return 0
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	}
	return 0
}
