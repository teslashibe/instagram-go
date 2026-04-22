package instagram_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	instagram "github.com/teslashibe/instagram-go"
)

// envOrSkip skips the test if the required env vars are not present.
// This lets `go test ./...` succeed even without cookies.
func envOrSkip(t *testing.T) instagram.Cookies {
	t.Helper()
	c := instagram.Cookies{
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
	if c.SessionID == "" || c.CSRFToken == "" || c.DSUserID == "" {
		t.Skip("set IG_SESSIONID, IG_CSRFTOKEN, IG_DS_USER_ID to run integration tests")
	}
	return c
}

func newClient(t *testing.T) *instagram.Client {
	t.Helper()
	cookies := envOrSkip(t)
	c, err := instagram.New(cookies)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestIntegration_New_ValidatesSession(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	me, err := c.Me(ctx)
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if me.ID == "" || me.Username == "" {
		t.Fatalf("Me returned empty user: %#v", me)
	}
	t.Logf("PASS: authenticated as @%s (id=%s)", me.Username, me.ID)
}

func TestIntegration_New_RejectsMissingCookies(t *testing.T) {
	if _, err := instagram.New(instagram.Cookies{}); err == nil {
		t.Fatal("expected ErrInvalidAuth, got nil")
	} else if !errors.Is(err, instagram.ErrInvalidAuth) {
		t.Fatalf("expected ErrInvalidAuth, got %v", err)
	}
	t.Log("PASS: empty cookies rejected with ErrInvalidAuth")
}

func TestIntegration_GetProfile(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	user, err := c.GetProfile(ctx, "instagram")
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	t.Logf("PASS: @%s — %d followers, %d following, %d posts",
		user.Username, user.FollowerCount, user.FollowingCount, user.MediaCount)
	if user.ID == "" {
		t.Fatal("expected non-empty user ID")
	}
	if !user.IsVerified {
		t.Error("expected @instagram to be verified")
	}
}

func TestIntegration_GetProfileByID(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Look up by username first to get the canonical ID.
	user, err := c.GetProfile(ctx, "natgeo")
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	user2, err := c.GetProfileByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetProfileByID: %v", err)
	}
	if user2.Username != user.Username {
		t.Fatalf("ID lookup mismatch: %s vs %s", user.Username, user2.Username)
	}
	t.Logf("PASS: ID lookup matches username lookup for @%s", user2.Username)
}

func TestIntegration_GetProfile_NotFound(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := c.GetProfile(ctx, "thisuserdoesnotexist1234567890qwertyz")
	if err == nil {
		t.Fatal("expected ErrNotFound, got nil")
	}
	if !errors.Is(err, instagram.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	t.Log("PASS: missing username -> ErrNotFound")
}

func TestIntegration_GetPosts(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	user, err := c.GetProfile(ctx, "natgeo")
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	it := c.GetPosts(user.ID).WithMaxPages(1)
	count := 0
	for it.Next(ctx) {
		p := it.Item()
		count++
		if count <= 1 {
			t.Logf("  Post pk=%s code=%s media_type=%d likes=%d comments=%d permalink=%s",
				p.PK, p.Code, p.MediaType, p.LikeCount, p.CommentCount, p.PermalinkURL)
		}
	}
	if err := it.Err(); err != nil {
		t.Fatalf("iterator: %v", err)
	}
	if count == 0 {
		t.Fatal("expected at least one post")
	}
	t.Logf("PASS: GetPosts returned %d items (1 page)", count)
}

func TestIntegration_GetPost(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Pull the latest post for @natgeo and round-trip it through GetPost(shortcode).
	user, err := c.GetProfile(ctx, "natgeo")
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	it := c.GetPosts(user.ID).WithMaxPages(1)
	if !it.Next(ctx) {
		t.Fatalf("no posts to round-trip: %v", it.Err())
	}
	first := it.Item()
	if first.Code == "" {
		t.Fatal("expected non-empty shortcode on first post")
	}
	round, err := c.GetPost(ctx, first.Code)
	if err != nil {
		t.Fatalf("GetPost(%s): %v", first.Code, err)
	}
	if round.PK != first.PK {
		t.Fatalf("PK mismatch: %s vs %s", round.PK, first.PK)
	}
	t.Logf("PASS: GetPost(%s) round-trip OK", first.Code)
}

func TestIntegration_GetReels(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	user, err := c.GetProfile(ctx, "natgeo")
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	if !user.HasReels {
		t.Skip("test target has no reels")
	}
	it := c.GetReels(user.ID).WithMaxPages(1)
	count := 0
	for it.Next(ctx) {
		count++
	}
	if err := it.Err(); err != nil {
		t.Fatalf("iterator: %v", err)
	}
	t.Logf("PASS: GetReels returned %d clips", count)
}

func TestIntegration_GetComments(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	user, err := c.GetProfile(ctx, "natgeo")
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	it := c.GetPosts(user.ID).WithMaxPages(1)
	if !it.Next(ctx) {
		t.Fatalf("no posts found: %v", it.Err())
	}
	post := it.Item()
	cit := c.GetComments(post.PK).WithMaxPages(1)
	count := 0
	for cit.Next(ctx) {
		count++
		if count == 1 {
			cm := cit.Item()
			t.Logf("  comment id=%s by=%s likes=%d text=%q", cm.ID,
				safeUsername(cm.User), cm.LikeCount, truncate(cm.Text, 60))
		}
	}
	if err := cit.Err(); err != nil {
		t.Fatalf("iterator: %v", err)
	}
	t.Logf("PASS: GetComments returned %d comments", count)
}

func TestIntegration_GetLikers(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	user, err := c.GetProfile(ctx, "natgeo")
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	it := c.GetPosts(user.ID).WithMaxPages(1)
	if !it.Next(ctx) {
		t.Fatalf("no posts found: %v", it.Err())
	}
	post := it.Item()
	likers, err := c.GetLikers(ctx, post.PK)
	if err != nil {
		t.Fatalf("GetLikers: %v", err)
	}
	t.Logf("PASS: GetLikers returned %d likers", len(likers))
}

func TestIntegration_GetFollowing(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	me, err := c.Me(ctx)
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	it := c.GetFollowing(me.ID).WithMaxPages(1)
	count := 0
	for it.Next(ctx) {
		count++
	}
	if err := it.Err(); err != nil {
		t.Fatalf("iterator: %v", err)
	}
	t.Logf("PASS: GetFollowing returned %d users (1 page)", count)
}

func TestIntegration_GetFriendship(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	user, err := c.GetProfile(ctx, "instagram")
	if err != nil {
		t.Fatalf("GetProfile: %v", err)
	}
	fs, err := c.GetFriendship(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetFriendship: %v", err)
	}
	t.Logf("PASS: relationship with @instagram following=%v followed_by=%v",
		fs.Following, fs.FollowedBy)
}

func TestIntegration_GetHashtag(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tag, err := c.GetHashtag(ctx, "nature")
	if err != nil {
		t.Fatalf("GetHashtag: %v", err)
	}
	if tag.Name != "nature" {
		t.Fatalf("tag.Name = %q, want %q", tag.Name, "nature")
	}
	t.Logf("PASS: #%s media_count=%d", tag.Name, tag.MediaCount)
}

func TestIntegration_GetHashtagPosts(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	it := c.GetHashtagPosts("nature").WithMaxPages(1)
	count := 0
	for it.Next(ctx) {
		count++
	}
	if err := it.Err(); err != nil {
		t.Fatalf("iterator: %v", err)
	}
	if count == 0 {
		t.Fatal("expected at least one hashtag post")
	}
	t.Logf("PASS: GetHashtagPosts(#nature) returned %d posts", count)
}

func TestIntegration_Search(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := c.Search(ctx, "national geographic")
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	t.Logf("PASS: Search returned users=%d hashtags=%d places=%d",
		len(res.Users), len(res.Hashtags), len(res.Places))
	if len(res.Users) == 0 && len(res.Hashtags) == 0 {
		t.Fatal("expected at least some search results")
	}
}

func TestIntegration_SearchUsers(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	users, err := c.SearchUsers(ctx, "natgeo")
	if err != nil {
		t.Fatalf("SearchUsers: %v", err)
	}
	if len(users) == 0 {
		t.Fatal("expected at least 1 user")
	}
	t.Logf("PASS: SearchUsers returned %d users (top: @%s)", len(users), users[0].Username)
}

func TestIntegration_StoryTray(t *testing.T) {
	c := newClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tray, err := c.GetStoryTray(ctx)
	if err != nil {
		t.Fatalf("GetStoryTray: %v", err)
	}
	t.Logf("PASS: story tray returned %d reels", len(tray))
}

func safeUsername(u *instagram.User) string {
	if u == nil {
		return ""
	}
	return u.Username
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
