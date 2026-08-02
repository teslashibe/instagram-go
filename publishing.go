package instagram

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PublishingCaptureVersion identifies the reviewed live burner capture compiled
// into the SDK. It intentionally remains empty while the repository contains
// only draft/offline fixtures: all publishing methods fail closed before
// consuming a stream or making an HTTP request. A future implementation may set
// this only in the same change that commits the reviewed, date-stamped capture.
const PublishingCaptureVersion = ""

const publishingDraftVersion = "2026-08-01-draft-v1"

// PublishedMediaKind identifies the captured configure/delete contract for a
// created media item.
type PublishedMediaKind string

const (
	PublishedMediaPhoto PublishedMediaKind = "photo"
	PublishedMediaReel  PublishedMediaKind = "reel"
	PublishedMediaStory PublishedMediaKind = "story"
)

// UploadSource describes a bounded media stream. Size is the exact decoded byte
// length, not a base64 length. Reader is consumed once by a publish call.
type UploadSource struct {
	Reader   io.Reader
	Filename string
	MIMEType string
	Size     int64
	Width    int
	Height   int
	Duration time.Duration
}

// PublishPhotoInput is the typed input for a single-image feed publication.
type PublishPhotoInput struct {
	Media          UploadSource
	Caption        string
	IdempotencyKey string
}

// PublishReelInput is the typed input for a Reel. Thumbnail is required because
// the captured Reel contract contains a separate thumbnail upload.
type PublishReelInput struct {
	Media          UploadSource
	Thumbnail      UploadSource
	Caption        string
	IdempotencyKey string
}

// PublishStoryInput is the typed input for a video Story. Thumbnail is required.
// Photo Story publishing is deliberately absent until separately captured.
type PublishStoryInput struct {
	Media          UploadSource
	Thumbnail      *UploadSource
	Caption        string
	IdempotencyKey string
}

// PublishResult identifies exactly one created media item and the deterministic
// identifiers used across upload, processing, and configure stages.
type PublishResult struct {
	MediaID  string             `json:"media_id"`
	Code     string             `json:"code,omitempty"`
	UploadID string             `json:"upload_id"`
	ClientID string             `json:"client_id"`
	Kind     PublishedMediaKind `json:"kind"`
}

// PublishError reports the safe protocol stage and deterministic identifiers
// involved in a failure. It never retains media bytes, captions, or credentials.
type PublishError struct {
	Stage      string
	UploadID   string
	ClientID   string
	Partial    bool
	Underlying error
}

func (e *PublishError) Error() string {
	if e == nil {
		return "instagram: publish failed"
	}
	return fmt.Sprintf("instagram: publish stage %s failed (upload_id=%s client_id=%s): %v",
		e.Stage, e.UploadID, e.ClientID, e.Underlying)
}

func (e *PublishError) Unwrap() []error {
	if e == nil {
		return nil
	}
	out := []error{e.Underlying}
	if e.Partial {
		out = append(out, ErrPartialUpload)
	}
	return out
}

type preparedUpload struct {
	bytes      []byte
	filename   string
	mimeType   string
	width      int
	height     int
	durationMS int64
	uploadID   string
	clientID   string
}

// PublishPhoto uploads and configures one feed photo.
func (c *Client) PublishPhoto(ctx context.Context, in PublishPhotoInput) (*PublishResult, error) {
	if err := c.ensurePublishingCapture(); err != nil {
		return nil, err
	}
	media, err := c.prepareUpload("photo", in.IdempotencyKey, in.Media, false)
	if err != nil {
		return nil, err
	}
	if err := c.uploadAsset(ctx, media, "1", media.uploadID); err != nil {
		return nil, publishStageError("photo_upload", media, uploadFailureMayBePartial(err), err)
	}
	if err := c.waitForProcessing(ctx, media); err != nil {
		return nil, publishStageError("photo_processing", media, true, err)
	}
	form := c.baseConfigureForm(media, in.Caption)
	return c.configurePublishedMedia(ctx, "photo", "/api/v1/media/configure/", media, form)
}

// PublishReel uploads a video and its thumbnail, waits for captured media
// processing states, and configures one Reel.
func (c *Client) PublishReel(ctx context.Context, in PublishReelInput) (*PublishResult, error) {
	if err := c.ensurePublishingCapture(); err != nil {
		return nil, err
	}
	media, err := c.prepareUpload("reel", in.IdempotencyKey, in.Media, true)
	if err != nil {
		return nil, err
	}
	thumb, err := c.prepareRelatedImage("reel_thumbnail", in.IdempotencyKey, in.Thumbnail, media)
	if err != nil {
		return nil, err
	}
	if err := c.uploadAsset(ctx, media, "2", media.uploadID); err != nil {
		return nil, publishStageError("reel_video_upload", media, uploadFailureMayBePartial(err), err)
	}
	if err := c.uploadAsset(ctx, thumb, "1", media.uploadID+"_0"); err != nil {
		return nil, publishStageError("reel_thumbnail_upload", media, true, err)
	}
	if err := c.finishAndWaitForVideo(ctx, media, "clips"); err != nil {
		return nil, publishStageError("reel_processing", media, true, err)
	}
	form := c.baseConfigureForm(media, in.Caption)
	form.Set("clips_share_preview_to_feed", "1")
	form.Set("clips_audio_type", "original")
	form.Set("poster_frame_index", "0")
	return c.configurePublishedMedia(ctx, "reel", "/api/v1/media/configure_to_clips/", media, form)
}

// PublishStory publishes one video Story using the captured thumbnail and
// processing/status contracts before configure.
func (c *Client) PublishStory(ctx context.Context, in PublishStoryInput) (*PublishResult, error) {
	if err := c.ensurePublishingCapture(); err != nil {
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(in.Media.MIMEType), "video/mp4") {
		return nil, fmt.Errorf("%w: only captured video Story publishing is supported", ErrInvalidPublishInput)
	}
	if in.Thumbnail == nil {
		return nil, fmt.Errorf("%w: video story thumbnail required", ErrInvalidPublishInput)
	}
	media, err := c.prepareUpload("story", in.IdempotencyKey, in.Media, true)
	if err != nil {
		return nil, err
	}
	if err := c.uploadAsset(ctx, media, "2", media.uploadID); err != nil {
		return nil, publishStageError("story_media_upload", media, uploadFailureMayBePartial(err), err)
	}
	thumb, err := c.prepareRelatedImage("story_thumbnail", in.IdempotencyKey, *in.Thumbnail, media)
	if err != nil {
		return nil, publishStageError("story_thumbnail_prepare", media, true, err)
	}
	if err := c.uploadAsset(ctx, thumb, "1", media.uploadID+"_0"); err != nil {
		return nil, publishStageError("story_thumbnail_upload", media, true, err)
	}
	if err := c.finishAndWaitForVideo(ctx, media, "story"); err != nil {
		return nil, publishStageError("story_processing", media, true, err)
	}
	form := c.baseConfigureForm(media, in.Caption)
	form.Set("configure_mode", "1")
	form.Set("story_media_creation_date", strconv.FormatInt(time.Now().Unix(), 10))
	return c.configurePublishedMedia(ctx, "story", "/api/v1/media/configure_to_story/", media, form)
}

// DeleteMedia deletes exactly one caller-supplied media ID using the per-kind
// deletion discriminator established by the reviewed capture. It exists for
// burner verification cleanup and deliberately does not enumerate account data.
func (c *Client) DeleteMedia(ctx context.Context, mediaID string, kind PublishedMediaKind) error {
	if err := c.ensurePublishingCapture(); err != nil {
		return err
	}
	mediaID = strings.TrimSpace(mediaID)
	if mediaID == "" || strings.ContainsAny(mediaID, "/?&#") {
		return errors.New("instagram: DeleteMedia: exact media ID required")
	}
	mediaType, err := deleteMediaType(kind)
	if err != nil {
		return err
	}
	form := url.Values{"media_type": {mediaType}}
	deleteCtx, cancel := context.WithTimeout(ctx, c.uploadTimeout)
	defer cancel()
	return c.doJSON(deleteCtx, http.MethodPost, "/api/v1/media/"+url.PathEscape(mediaID)+"/delete/", nil,
		&requestOptions{
			Host: requestHostAPI, IsWrite: true, FormBody: form, MaxAttempts: 1,
			ContextControlsTimeout: true,
		}, nil)
}

func deleteMediaType(kind PublishedMediaKind) (string, error) {
	switch kind {
	case PublishedMediaPhoto:
		return "PHOTO", nil
	case PublishedMediaReel:
		return "VIDEO", nil
	case PublishedMediaStory:
		return "STORY", nil
	default:
		return "", fmt.Errorf("%w: unknown published media kind %q", ErrInvalidPublishInput, kind)
	}
}

func (c *Client) ensurePublishingCapture() error {
	if PublishingCaptureVersion != "" || c.allowUnverifiedPublishing {
		return nil
	}
	return fmt.Errorf("%w: commit a reviewed date-stamped burner capture before enabling mutations",
		ErrPublishingCaptureRequired)
}

func (c *Client) prepareUpload(kind, key string, source UploadSource, video bool) (preparedUpload, error) {
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 128 {
		return preparedUpload{}, fmt.Errorf("%w: %s idempotency key must contain 1-128 characters", ErrInvalidPublishInput, kind)
	}
	if err := validateSourceMetadata(source, video); err != nil {
		return preparedUpload{}, fmt.Errorf("%w: %s: %v", ErrInvalidPublishInput, kind, err)
	}
	limit := c.maxPhotoUploadBytes
	if video {
		limit = c.maxVideoUploadBytes
	}
	raw, err := readBoundedUpload(source.Reader, source.Size, limit)
	if err != nil {
		return preparedUpload{}, fmt.Errorf("instagram: %s: %w", kind, err)
	}
	digest := sha256.Sum256(raw)
	identity := fmt.Sprintf("%s\x00%s\x00%s\x00%d\x00%d\x00%d\x00%d\x00%s",
		publishingProtocolVersion(), kind, key, len(raw), source.Width, source.Height,
		source.Duration.Milliseconds(), hex.EncodeToString(digest[:]))
	idDigest := sha256.Sum256([]byte(identity))
	return preparedUpload{
		bytes: raw, filename: source.Filename, mimeType: normalizedMIME(source.MIMEType),
		width: source.Width, height: source.Height, durationMS: source.Duration.Milliseconds(),
		uploadID: deterministicUploadID(idDigest), clientID: deterministicUUID(idDigest),
	}, nil
}

func publishingProtocolVersion() string {
	if PublishingCaptureVersion != "" {
		return PublishingCaptureVersion
	}
	return publishingDraftVersion
}

func (c *Client) prepareRelatedImage(kind, key string, source UploadSource, parent preparedUpload) (preparedUpload, error) {
	if err := validateSourceMetadata(source, false); err != nil {
		return preparedUpload{}, fmt.Errorf("%w: %s: %v", ErrInvalidPublishInput, kind, err)
	}
	raw, err := readBoundedUpload(source.Reader, source.Size, c.maxPhotoUploadBytes)
	if err != nil {
		return preparedUpload{}, fmt.Errorf("instagram: %s: %w", kind, err)
	}
	return preparedUpload{
		bytes: raw, filename: source.Filename, mimeType: normalizedMIME(source.MIMEType),
		width: source.Width, height: source.Height, uploadID: parent.uploadID, clientID: parent.clientID,
	}, nil
}

func validateSourceMetadata(source UploadSource, video bool) error {
	if source.Reader == nil {
		return errors.New("media reader required")
	}
	if strings.TrimSpace(source.Filename) == "" || filepath.Base(source.Filename) != source.Filename {
		return errors.New("filename must be a basename")
	}
	if source.Size <= 0 {
		return errors.New("exact positive byte size required")
	}
	if source.Width <= 0 || source.Height <= 0 {
		return errors.New("positive width and height required")
	}
	got := normalizedMIME(source.MIMEType)
	if video {
		if got != "video/mp4" {
			return fmt.Errorf("video MIME type must be video/mp4, got %q", got)
		}
		if source.Duration <= 0 {
			return errors.New("positive video duration required")
		}
		return nil
	}
	if got != "image/jpeg" {
		return fmt.Errorf("photo MIME type must be image/jpeg, got %q", got)
	}
	if source.Duration != 0 {
		return errors.New("photo duration must be zero")
	}
	return nil
}

func normalizedMIME(value string) string {
	parsed, _, err := mime.ParseMediaType(strings.TrimSpace(value))
	if err != nil {
		return strings.ToLower(strings.TrimSpace(value))
	}
	return strings.ToLower(parsed)
}

func readBoundedUpload(reader io.Reader, declared, limit int64) ([]byte, error) {
	if declared > limit {
		return nil, fmt.Errorf("%w: declared %d bytes exceeds %d-byte limit", ErrUploadTooLarge, declared, limit)
	}
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, fmt.Errorf("read media: %w", err)
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("%w: stream exceeds %d-byte limit", ErrUploadTooLarge, limit)
	}
	if int64(len(raw)) != declared {
		return nil, fmt.Errorf("%w: declared media size %d does not match stream size %d",
			ErrInvalidPublishInput, declared, len(raw))
	}
	return raw, nil
}

func deterministicUploadID(sum [32]byte) string {
	const base uint64 = 100_000_000_000_000_000
	return strconv.FormatUint(base+binary.BigEndian.Uint64(sum[:8])%900_000_000_000_000_000, 10)
}

func deterministicUUID(sum [32]byte) string {
	b := append([]byte(nil), sum[:16]...)
	b[6] = (b[6] & 0x0f) | 0x50
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		binary.BigEndian.Uint32(b[0:4]), binary.BigEndian.Uint16(b[4:6]),
		binary.BigEndian.Uint16(b[6:8]), binary.BigEndian.Uint16(b[8:10]), b[10:16])
}

func (c *Client) uploadAsset(ctx context.Context, media preparedUpload, mediaType, entityName string) error {
	params := map[string]string{
		"upload_id": media.uploadID, "media_type": mediaType, "xsharing_user_ids": "[]",
		"upload_media_width": strconv.Itoa(media.width), "upload_media_height": strconv.Itoa(media.height),
	}
	if media.durationMS > 0 {
		params["upload_media_duration_ms"] = strconv.FormatInt(media.durationMS, 10)
	}
	paramsJSON, err := json.Marshal(params)
	if err != nil {
		return err
	}
	uploadCtx, cancel := context.WithTimeout(ctx, c.uploadTimeout)
	defer cancel()
	body, _, err := c.doRaw(uploadCtx, http.MethodPost, "/rupload_ig"+uploadEntityKind(mediaType)+"/"+url.PathEscape(entityName),
		nil, &requestOptions{
			Host: requestHostAPI, IsWrite: true, RawBody: media.bytes,
			ContentLength: int64(len(media.bytes)), MaxAttempts: 1,
			ContextControlsTimeout: true,
			ExtraHeaders: map[string]string{
				"X-Entity-Type":              media.mimeType,
				"X-Entity-Name":              entityName,
				"X-Entity-Length":            strconv.Itoa(len(media.bytes)),
				"Offset":                     "0",
				"X-Instagram-Rupload-Params": string(paramsJSON),
				"X_FB_PHOTO_WATERFALL_ID":    media.clientID,
			},
		})
	if err != nil {
		return err
	}
	var envelope struct {
		Status   string `json:"status"`
		UploadID string `json:"upload_id"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("%w: decode rupload response: %v", ErrUnexpectedResponse, err)
	}
	if envelope.Status != "ok" || (envelope.UploadID != "" && envelope.UploadID != media.uploadID) {
		return fmt.Errorf("%w: rupload did not acknowledge deterministic upload ID", ErrUnexpectedResponse)
	}
	return nil
}

func uploadEntityKind(mediaType string) string {
	if mediaType == "2" {
		return "video"
	}
	return "photo"
}

func (c *Client) baseConfigureForm(media preparedUpload, caption string) url.Values {
	return url.Values{
		"upload_id":           {media.uploadID},
		"caption":             {caption},
		"source_type":         {"4"},
		"upload_media_width":  {strconv.Itoa(media.width)},
		"upload_media_height": {strconv.Itoa(media.height)},
		"client_context":      {media.clientID},
		"device_id":           {"android-" + c.cookies.DSUserID},
	}
}

func (c *Client) finishAndWaitForVideo(ctx context.Context, media preparedUpload, kind string) error {
	form := url.Values{
		"upload_id": {media.uploadID}, "source_type": {"4"}, "video": {"1"},
		"media_type": {kind},
	}
	finishCtx, finishCancel := context.WithTimeout(ctx, c.uploadTimeout)
	defer finishCancel()
	if err := c.doJSON(finishCtx, http.MethodPost, "/api/v1/media/upload_finish/", nil,
		&requestOptions{
			Host: requestHostAPI, IsWrite: true, FormBody: form, MaxAttempts: 1,
			ContextControlsTimeout: true,
		}, nil); err != nil {
		return err
	}
	return c.waitForProcessing(ctx, media)
}

func (c *Client) waitForProcessing(ctx context.Context, media preparedUpload) error {
	processCtx, cancel := context.WithTimeout(ctx, c.processingTimeout)
	defer cancel()
	for {
		q := url.Values{"upload_id": {media.uploadID}}
		var response struct {
			Status         string `json:"status"`
			Message        string `json:"message"`
			UploadID       string `json:"upload_id"`
			ProcessingInfo struct {
				State string `json:"state"`
			} `json:"processing_info"`
		}
		err := c.doJSON(processCtx, http.MethodGet, "/api/v1/media/upload_status/", q,
			&requestOptions{
				Host: requestHostAPI, IsWrite: true, MaxAttempts: 1,
				ContextControlsTimeout: true,
			}, &response)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				return ErrProcessingTimeout
			}
			return err
		}
		state := strings.ToLower(strings.TrimSpace(response.ProcessingInfo.State))
		switch state {
		case "ok", "ready", "finished", "complete", "completed", "succeeded", "uploaded":
			return nil
		case "failed", "error", "expired", "rejected":
			return fmt.Errorf("%w: state=%s", ErrProcessingFailed, state)
		case "pending", "processing", "transcoding", "uploading", "in_progress":
			select {
			case <-time.After(500 * time.Millisecond):
			case <-processCtx.Done():
				return fmt.Errorf("%w: %v", ErrProcessingTimeout, processCtx.Err())
			}
		default:
			return fmt.Errorf("%w: unknown upload processing state %q", ErrUnexpectedResponse, state)
		}
	}
}

func (c *Client) configurePublishedMedia(ctx context.Context, kind, path string, media preparedUpload, form url.Values) (*PublishResult, error) {
	var response struct {
		Status string          `json:"status"`
		Media  json.RawMessage `json:"media"`
	}
	configureCtx, cancel := context.WithTimeout(ctx, c.uploadTimeout)
	defer cancel()
	err := c.doJSON(configureCtx, http.MethodPost, path, nil,
		&requestOptions{
			Host: requestHostAPI, IsWrite: true, FormBody: form, MaxAttempts: 1,
			ContextControlsTimeout: true,
		}, &response)
	if err != nil {
		return nil, publishStageError(kind+"_configure", media, true, err)
	}
	if response.Status != "ok" || len(response.Media) == 0 || string(response.Media) == "null" {
		return nil, publishStageError(kind+"_configure", media, true,
			fmt.Errorf("%w: configure returned no media", ErrUnexpectedResponse))
	}
	post, err := parsePost(response.Media)
	if err != nil {
		return nil, publishStageError(kind+"_configure", media, true, err)
	}
	if post.PK == "" {
		return nil, publishStageError(kind+"_configure", media, true,
			fmt.Errorf("%w: configure media has no ID", ErrUnexpectedResponse))
	}
	return &PublishResult{
		MediaID: post.PK, Code: post.Code, UploadID: media.uploadID, ClientID: media.clientID,
		Kind: PublishedMediaKind(kind),
	}, nil
}

func publishStageError(stage string, media preparedUpload, partial bool, err error) error {
	return &PublishError{
		Stage: stage, UploadID: media.uploadID, ClientID: media.clientID, Partial: partial, Underlying: err,
	}
}

func uploadFailureMayBePartial(err error) bool {
	return !errors.Is(err, ErrChallengeRequired) &&
		!errors.Is(err, ErrFeedbackRequired) &&
		!errors.Is(err, ErrWriteSoftBlock) &&
		!errors.Is(err, ErrRateLimited) &&
		!errors.Is(err, ErrInvalidAuth) &&
		!errors.Is(err, ErrSessionExpired) &&
		!errors.Is(err, ErrCSRF)
}
