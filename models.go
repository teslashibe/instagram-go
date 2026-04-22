package instagram

import "encoding/json"

// User represents an Instagram user/profile. Fields are populated based on
// which endpoint returned the data; not all fields are present everywhere.
//
// IDs are strings because Instagram returns 64-bit numeric IDs that exceed
// JS Number safe range and are sometimes serialised as strings, sometimes as
// numbers. The client normalises to strings.
type User struct {
	ID                  string   `json:"pk_id,omitempty"`
	Username            string   `json:"username,omitempty"`
	FullName            string   `json:"full_name,omitempty"`
	Biography           string   `json:"biography,omitempty"`
	ExternalURL         string   `json:"external_url,omitempty"`
	ProfilePicURL       string   `json:"profile_pic_url,omitempty"`
	ProfilePicURLHD     string   `json:"profile_pic_url_hd,omitempty"`
	IsPrivate           bool     `json:"is_private,omitempty"`
	IsVerified          bool     `json:"is_verified,omitempty"`
	IsBusiness          bool     `json:"is_business,omitempty"`
	IsProfessional      bool     `json:"is_professional_account,omitempty"`
	BusinessCategory    string   `json:"business_category_name,omitempty"`
	Category            string   `json:"category_name,omitempty"`
	FollowerCount       int      `json:"follower_count,omitempty"`
	FollowingCount      int      `json:"following_count,omitempty"`
	MediaCount          int      `json:"media_count,omitempty"`
	TotalIGTVCount      int      `json:"total_igtv_videos,omitempty"`
	HasReels            bool     `json:"has_clips,omitempty"`
	HasGuides           bool     `json:"has_guides,omitempty"`
	HasChaining         bool     `json:"has_chaining,omitempty"`
	HasHighlightReels   bool     `json:"has_highlight_reels,omitempty"`
	HideLikeAndViewCnts bool     `json:"hide_like_and_view_counts,omitempty"`
	IsBusinessOwned     bool     `json:"is_business_owned_by_viewer,omitempty"`
	PublicEmail         string   `json:"public_email,omitempty"`
	PublicPhone         string   `json:"public_phone_number,omitempty"`
	ContactPhone        string   `json:"contact_phone_number,omitempty"`
	AddressStreet       string   `json:"address_street,omitempty"`
	City                string   `json:"city_name,omitempty"`
	Zip                 string   `json:"zip,omitempty"`
	AccountType         int      `json:"account_type,omitempty"`
	Pronouns            []string `json:"pronouns,omitempty"`

	FriendshipStatus *FriendshipStatus `json:"friendship_status,omitempty"`

	// Raw is the complete user payload from the source endpoint.
	// It lets callers access fields that aren't yet typed.
	Raw json.RawMessage `json:"-"`
}

// FriendshipStatus describes the relationship between the viewer and a user.
type FriendshipStatus struct {
	Following       bool `json:"following"`
	FollowedBy      bool `json:"followed_by"`
	Blocking        bool `json:"blocking"`
	Muting          bool `json:"muting"`
	IsPrivate       bool `json:"is_private"`
	IncomingRequest bool `json:"incoming_request"`
	OutgoingRequest bool `json:"outgoing_request"`
	IsBestie        bool `json:"is_bestie"`
	IsRestricted    bool `json:"is_restricted"`
	IsFeedFavorite  bool `json:"is_feed_favorite"`
}

// MediaType is the Instagram media_type enum.
type MediaType int

const (
	MediaTypeUnknown  MediaType = 0
	MediaTypePhoto    MediaType = 1
	MediaTypeVideo    MediaType = 2
	MediaTypeCarousel MediaType = 8
)

// Post is a media item — photo, video, reel, carousel, or IGTV.
type Post struct {
	ID            string    `json:"id,omitempty"`
	PK            string    `json:"pk,omitempty"`
	Code          string    `json:"code,omitempty"`
	MediaType     MediaType `json:"media_type"`
	ProductType   string    `json:"product_type,omitempty"`
	TakenAt       int64     `json:"taken_at,omitempty"`
	Caption       string    `json:"caption_text,omitempty"`
	CaptionUserID string    `json:"caption_user_id,omitempty"`
	Owner         *User     `json:"user,omitempty"`

	LikeCount      int     `json:"like_count"`
	CommentCount   int     `json:"comment_count"`
	ViewCount      int     `json:"view_count,omitempty"`
	PlayCount      int     `json:"play_count,omitempty"`
	IGTVViewCount  int     `json:"igtv_view_count,omitempty"`
	ReshareCount   int     `json:"reshare_count,omitempty"`
	SaveCount      int     `json:"save_count,omitempty"`
	OriginalWidth  int     `json:"original_width,omitempty"`
	OriginalHeight int     `json:"original_height,omitempty"`
	VideoDurationS float64 `json:"video_duration,omitempty"`

	HasLiked      bool `json:"has_liked,omitempty"`
	IsPinned      bool `json:"is_pinned,omitempty"`
	IsPaidPartner bool `json:"is_paid_partnership,omitempty"`

	ImageVersions []ImageVersion `json:"image_versions,omitempty"`
	VideoVersions []VideoVersion `json:"video_versions,omitempty"`
	CarouselMedia []*Post        `json:"carousel_media,omitempty"`

	Hashtags []string `json:"hashtags,omitempty"`
	Mentions []string `json:"mentions,omitempty"`

	Location *Location `json:"location,omitempty"`

	ClipsMetadata *ClipsMetadata `json:"clips_metadata,omitempty"`

	// PermalinkURL is constructed from the shortcode as
	// https://www.instagram.com/p/<code>/ for posts and /reel/<code>/ for
	// product_type==clips.
	PermalinkURL string `json:"-"`

	// Raw is the complete media payload from the source endpoint.
	Raw json.RawMessage `json:"-"`
}

// ImageVersion describes one resolution of a post's image.
type ImageVersion struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
}

// VideoVersion describes one resolution of a post's video.
type VideoVersion struct {
	URL    string `json:"url"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Type   int    `json:"type,omitempty"`
}

// ClipsMetadata is the reels-specific metadata attached to a Post when
// product_type == "clips".
type ClipsMetadata struct {
	OriginalSoundInfo  *AudioInfo `json:"original_sound_info,omitempty"`
	MusicInfo          *AudioInfo `json:"music_info,omitempty"`
	AudioRankingInfo   *AudioInfo `json:"audio_ranking_info,omitempty"`
	OriginalAudioTitle string     `json:"original_audio_title,omitempty"`
}

// AudioInfo describes the audio track used in a reel.
type AudioInfo struct {
	AudioAssetID   string `json:"audio_asset_id,omitempty"`
	OriginalAudio  bool   `json:"original_audio,omitempty"`
	ArtistName     string `json:"display_artist,omitempty"`
	Title          string `json:"title,omitempty"`
	DurationMs     int64  `json:"duration_in_ms,omitempty"`
	OwnerID        string `json:"original_media_id,omitempty"`
	ProgressiveURL string `json:"progressive_download_url,omitempty"`
	IPADURL        string `json:"dash_manifest,omitempty"`
}

// Comment is a top-level or threaded comment on a post.
type Comment struct {
	ID                string `json:"pk,omitempty"`
	UserID            string `json:"user_id,omitempty"`
	User              *User  `json:"user,omitempty"`
	Text              string `json:"text,omitempty"`
	CreatedAt         int64  `json:"created_at,omitempty"`
	LikeCount         int    `json:"comment_like_count"`
	HasLikedComment   bool   `json:"has_liked_comment"`
	ChildCommentCount int    `json:"child_comment_count"`
	ParentCommentID   string `json:"parent_comment_id,omitempty"`

	Replies []*Comment `json:"child_comments,omitempty"`

	Raw json.RawMessage `json:"-"`
}

// Hashtag describes a hashtag with profile metadata.
type Hashtag struct {
	ID             string `json:"id,omitempty"`
	Name           string `json:"name"`
	MediaCount     int    `json:"media_count"`
	ProfilePicURL  string `json:"profile_pic_url,omitempty"`
	Following      bool   `json:"following"`
	FollowingCount int    `json:"following_count,omitempty"`

	Raw json.RawMessage `json:"-"`
}

// Location is a geo-tag attached to a post.
type Location struct {
	ID               string  `json:"pk,omitempty"`
	ShortName        string  `json:"short_name,omitempty"`
	Name             string  `json:"name,omitempty"`
	Address          string  `json:"address,omitempty"`
	City             string  `json:"city,omitempty"`
	Lng              float64 `json:"lng,omitempty"`
	Lat              float64 `json:"lat,omitempty"`
	ExternalSource   string  `json:"external_source,omitempty"`
	FacebookPlacesID string  `json:"facebook_places_id,omitempty"`
	MediaCount       int     `json:"media_count,omitempty"`

	Raw json.RawMessage `json:"-"`
}

// Story is one item from a user's story tray.
type Story struct {
	ID            string         `json:"pk,omitempty"`
	MediaType     MediaType      `json:"media_type"`
	TakenAt       int64          `json:"taken_at,omitempty"`
	ExpiringAt    int64          `json:"expiring_at,omitempty"`
	User          *User          `json:"user,omitempty"`
	ImageVersions []ImageVersion `json:"image_versions,omitempty"`
	VideoVersions []VideoVersion `json:"video_versions,omitempty"`
	Audience      string         `json:"audience,omitempty"`

	Raw json.RawMessage `json:"-"`
}

// SearchResult bundles users, hashtags, and places returned by /web/search/topsearch/.
type SearchResult struct {
	Users    []*User    `json:"users,omitempty"`
	Hashtags []*Hashtag `json:"hashtags,omitempty"`
	Places   []*Place   `json:"places,omitempty"`
}

// Place is a search result wrapping a Location with extra subtitle text.
type Place struct {
	Title    string    `json:"title,omitempty"`
	Subtitle string    `json:"subtitle,omitempty"`
	Location *Location `json:"location,omitempty"`
}

// Page is one page of a paginated response. NextCursor is empty when there
// are no more results. Pass it back to the next call's WithCursor option to
// fetch the following page.
type Page[T any] struct {
	Items      []T
	NextCursor string
	HasMore    bool
}

// PageOptions configures a paginated request.
type PageOptions struct {
	// Cursor is the next_max_id (or equivalent) returned from a previous page.
	// Leave empty to fetch the first page.
	Cursor string
	// Limit caps the number of items per request. Instagram clamps this server-side
	// (typically 12-50 depending on the endpoint); 0 uses the endpoint default.
	Limit int
}
