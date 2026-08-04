package instagram_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	instagram "github.com/teslashibe/instagram-go"
)

// TestIntegration_PublishDisposableBurnerMedia is deliberately one serial test:
// every created ID is registered for exact-ID cleanup before verification.
func TestIntegration_PublishDisposableBurnerMedia(t *testing.T) {
	if os.Getenv("IG_PUBLISH_LIVE_TEST") != "1" {
		t.Skip("set IG_PUBLISH_LIVE_TEST=1 for burner publishing verification")
	}
	if instagram.PublishingCaptureVersion == "" {
		t.Fatal("IG_PUBLISH_LIVE_TEST=1 requested live verification, but no reviewed publishing capture is compiled in")
	}
	if os.Getenv("IG_PUBLISH_BURNER_ACK") != "DISPOSABLE_BURNER_CONTENT" {
		t.Fatal("IG_PUBLISH_BURNER_ACK=DISPOSABLE_BURNER_CONTENT is required")
	}
	cookies := instagram.Cookies{
		SessionID: os.Getenv("IG_SESSIONID"), CSRFToken: os.Getenv("IG_CSRFTOKEN"),
		DSUserID: os.Getenv("IG_DS_USER_ID"), Datr: os.Getenv("IG_DATR"),
		Mid: os.Getenv("IG_MID"), IgDid: os.Getenv("IG_DID"), Rur: os.Getenv("IG_RUR"),
	}
	if cookies.SessionID == "" || cookies.CSRFToken == "" || cookies.DSUserID == "" {
		t.Fatal("IG_SESSIONID, IG_CSRFTOKEN, and IG_DS_USER_ID are required")
	}
	client, err := instagram.New(cookies)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	photo := liveSource(t, "IG_PUBLISH_PHOTO_PATH", "image/jpeg", 1080, 1350, 0)
	reel := liveSource(t, "IG_PUBLISH_REEL_PATH", "video/mp4", 1080, 1920, liveDuration(t, "IG_PUBLISH_REEL_DURATION_MS"))
	reelThumb := liveSource(t, "IG_PUBLISH_REEL_THUMBNAIL_PATH", "image/jpeg", 1080, 1920, 0)
	story := liveSource(t, "IG_PUBLISH_STORY_PATH", "video/mp4", 1080, 1920, liveDuration(t, "IG_PUBLISH_STORY_DURATION_MS"))
	storyThumb := liveSource(t, "IG_PUBLISH_STORY_THUMBNAIL_PATH", "image/jpeg", 1080, 1920, 0)

	results := make([]*instagram.PublishResult, 0, 3)
	cleanup := func(result *instagram.PublishResult) {
		results = append(results, result)
	}
	t.Cleanup(func() {
		for index := len(results) - 1; index >= 0; index-- {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Minute)
			err := client.DeleteMedia(cleanupCtx, results[index].MediaID, results[index].Kind)
			if err == nil {
				err = confirmDeletedMedia(cleanupCtx, client, results[index].MediaID)
			}
			cleanupCancel()
			if err != nil {
				t.Errorf("cleanup and confirm exact media ID %s: %v", results[index].MediaID, err)
			}
		}
	})

	photoResult, err := client.PublishPhoto(ctx, instagram.PublishPhotoInput{
		Media: photo, Caption: "disposable burner photo", IdempotencyKey: "live-photo-" + strconv.FormatInt(time.Now().Unix(), 10),
	})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(photoResult)
	verifyCreatedMedia(t, ctx, client, photoResult)

	reelResult, err := client.PublishReel(ctx, instagram.PublishReelInput{
		Media: reel, Thumbnail: reelThumb, Caption: "disposable burner reel",
		IdempotencyKey: "live-reel-" + strconv.FormatInt(time.Now().Unix(), 10),
	})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(reelResult)
	verifyCreatedMedia(t, ctx, client, reelResult)

	storyResult, err := client.PublishStory(ctx, instagram.PublishStoryInput{
		Media: story, Thumbnail: &storyThumb, Caption: "disposable burner story",
		IdempotencyKey: "live-story-" + strconv.FormatInt(time.Now().Unix(), 10),
	})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(storyResult)
	verifyCreatedMedia(t, ctx, client, storyResult)
}

func confirmDeletedMedia(ctx context.Context, client *instagram.Client, mediaID string) error {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		_, err := client.GetPostByID(ctx, mediaID)
		if errors.Is(err, instagram.ErrNotFound) || errors.Is(err, instagram.ErrMediaUnavailable) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("post-delete readback: %w", err)
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return fmt.Errorf("media remained readable after exact-ID delete: %w", ctx.Err())
		}
	}
}

func liveSource(t *testing.T, env, mimeType string, width, height int, duration time.Duration) instagram.UploadSource {
	t.Helper()
	path := os.Getenv(env)
	if path == "" {
		t.Fatalf("%s is required", env)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	info, err := file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	return instagram.UploadSource{
		Reader: file, Filename: info.Name(), MIMEType: mimeType, Size: info.Size(),
		Width: width, Height: height, Duration: duration,
	}
}

func liveDuration(t *testing.T, env string) time.Duration {
	t.Helper()
	value, err := strconv.ParseInt(os.Getenv(env), 10, 64)
	if err != nil || value <= 0 {
		t.Fatalf("%s must be a positive millisecond duration", env)
	}
	return time.Duration(value) * time.Millisecond
}

func verifyCreatedMedia(t *testing.T, ctx context.Context, client *instagram.Client, result *instagram.PublishResult) {
	t.Helper()
	if result == nil || result.MediaID == "" {
		t.Fatalf("publish returned no exact media ID: %#v", result)
	}
	post, err := client.GetPostByID(ctx, result.MediaID)
	if err != nil {
		t.Fatalf("verify exact media ID %s: %v", result.MediaID, err)
	}
	if post.PK != result.MediaID {
		t.Fatalf("verified media ID %s, want %s", post.PK, result.MediaID)
	}
	t.Log(fmt.Sprintf("PASS: created disposable %s media_id=%s", result.Kind, result.MediaID))
}
