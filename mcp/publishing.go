package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	instagram "github.com/teslashibe/instagram-go"
	"github.com/teslashibe/mcptool"
)

const (
	maxMCPPhotoBytes = 8 << 20
	maxMCPVideoBytes = 64 << 20
	maxMCPTimeout    = 2 * time.Minute
)

// PublishPhotoInput is the explicitly confirmed input for
// instagram_publish_photo. MediaBase64 is bounded before decoding.
type PublishPhotoInput struct {
	ConfirmMutation bool   `json:"confirm_mutation" jsonschema:"description=must be true to confirm publishing content to Instagram,required"`
	MediaBase64     string `json:"media_base64" jsonschema:"description=base64-encoded JPEG bytes,required"`
	Filename        string `json:"filename" jsonschema:"description=JPEG basename,required"`
	MIMEType        string `json:"mime_type" jsonschema:"description=must be image/jpeg,required"`
	ByteSize        int64  `json:"byte_size" jsonschema:"description=exact decoded byte length,minimum=1,maximum=8388608,required"`
	Width           int    `json:"width" jsonschema:"description=source width in pixels,minimum=1,required"`
	Height          int    `json:"height" jsonschema:"description=source height in pixels,minimum=1,required"`
	Caption         string `json:"caption,omitempty" jsonschema:"description=plain-text feed caption"`
	IdempotencyKey  string `json:"idempotency_key" jsonschema:"description=stable caller key used to derive deterministic upload IDs,minLength=1,maxLength=128,required"`
}

// PublishReelInput is the explicitly confirmed input for
// instagram_publish_reel.
type PublishReelInput struct {
	ConfirmMutation bool   `json:"confirm_mutation" jsonschema:"description=must be true to confirm publishing content to Instagram,required"`
	MediaBase64     string `json:"media_base64" jsonschema:"description=base64-encoded MP4 bytes,required"`
	Filename        string `json:"filename" jsonschema:"description=MP4 basename,required"`
	MIMEType        string `json:"mime_type" jsonschema:"description=must be video/mp4,required"`
	ByteSize        int64  `json:"byte_size" jsonschema:"description=exact decoded byte length,minimum=1,maximum=67108864,required"`
	Width           int    `json:"width" jsonschema:"description=video width in pixels,minimum=1,required"`
	Height          int    `json:"height" jsonschema:"description=video height in pixels,minimum=1,required"`
	DurationMS      int64  `json:"duration_ms" jsonschema:"description=video duration in milliseconds,minimum=1,required"`
	ThumbnailBase64 string `json:"thumbnail_base64" jsonschema:"description=base64-encoded JPEG thumbnail bytes,required"`
	ThumbnailName   string `json:"thumbnail_filename" jsonschema:"description=thumbnail JPEG basename,required"`
	ThumbnailSize   int64  `json:"thumbnail_byte_size" jsonschema:"description=exact decoded thumbnail length,minimum=1,maximum=8388608,required"`
	ThumbnailWidth  int    `json:"thumbnail_width" jsonschema:"description=thumbnail width in pixels,minimum=1,required"`
	ThumbnailHeight int    `json:"thumbnail_height" jsonschema:"description=thumbnail height in pixels,minimum=1,required"`
	Caption         string `json:"caption,omitempty" jsonschema:"description=plain-text Reel caption"`
	IdempotencyKey  string `json:"idempotency_key" jsonschema:"description=stable caller key used to derive deterministic upload IDs,minLength=1,maxLength=128,required"`
}

// PublishStoryInput is the explicitly confirmed input for
// instagram_publish_story. Video stories require thumbnail fields.
type PublishStoryInput struct {
	ConfirmMutation bool   `json:"confirm_mutation" jsonschema:"description=must be true to confirm publishing content to Instagram,required"`
	MediaBase64     string `json:"media_base64" jsonschema:"description=base64-encoded JPEG or MP4 bytes,required"`
	Filename        string `json:"filename" jsonschema:"description=media basename,required"`
	MIMEType        string `json:"mime_type" jsonschema:"description=image/jpeg or video/mp4,required"`
	ByteSize        int64  `json:"byte_size" jsonschema:"description=exact decoded byte length,minimum=1,maximum=67108864,required"`
	Width           int    `json:"width" jsonschema:"description=source width in pixels,minimum=1,required"`
	Height          int    `json:"height" jsonschema:"description=source height in pixels,minimum=1,required"`
	DurationMS      int64  `json:"duration_ms,omitempty" jsonschema:"description=video duration in milliseconds; zero for photos,minimum=0"`
	ThumbnailBase64 string `json:"thumbnail_base64,omitempty" jsonschema:"description=required JPEG thumbnail for video stories"`
	ThumbnailName   string `json:"thumbnail_filename,omitempty" jsonschema:"description=thumbnail JPEG basename"`
	ThumbnailSize   int64  `json:"thumbnail_byte_size,omitempty" jsonschema:"description=exact decoded thumbnail length,minimum=0,maximum=8388608"`
	ThumbnailWidth  int    `json:"thumbnail_width,omitempty" jsonschema:"description=thumbnail width in pixels,minimum=0"`
	ThumbnailHeight int    `json:"thumbnail_height,omitempty" jsonschema:"description=thumbnail height in pixels,minimum=0"`
	Caption         string `json:"caption,omitempty" jsonschema:"description=plain-text Story caption"`
	IdempotencyKey  string `json:"idempotency_key" jsonschema:"description=stable caller key used to derive deterministic upload IDs,minLength=1,maxLength=128,required"`
}

func publishPhoto(ctx context.Context, c *instagram.Client, in PublishPhotoInput) (any, error) {
	if err := requireMutationConfirmation(in.ConfirmMutation); err != nil {
		return nil, err
	}
	media, err := decodeMCPMedia(in.MediaBase64, in.ByteSize, maxMCPPhotoBytes)
	if err != nil {
		return nil, err
	}
	callCtx, cancel := context.WithTimeout(ctx, maxMCPTimeout)
	defer cancel()
	result, err := c.PublishPhoto(callCtx, instagram.PublishPhotoInput{
		Media: instagram.UploadSource{
			Reader: bytes.NewReader(media), Filename: in.Filename, MIMEType: in.MIMEType,
			Size: in.ByteSize, Width: in.Width, Height: in.Height,
		},
		Caption: in.Caption, IdempotencyKey: in.IdempotencyKey,
	})
	if err != nil {
		return nil, publishingToolError(err)
	}
	return result, nil
}

func publishReel(ctx context.Context, c *instagram.Client, in PublishReelInput) (any, error) {
	if err := requireMutationConfirmation(in.ConfirmMutation); err != nil {
		return nil, err
	}
	video, err := decodeMCPMedia(in.MediaBase64, in.ByteSize, maxMCPVideoBytes)
	if err != nil {
		return nil, err
	}
	thumb, err := decodeMCPMedia(in.ThumbnailBase64, in.ThumbnailSize, maxMCPPhotoBytes)
	if err != nil {
		return nil, prefixToolError("invalid thumbnail: ", err)
	}
	callCtx, cancel := context.WithTimeout(ctx, maxMCPTimeout)
	defer cancel()
	result, err := c.PublishReel(callCtx, instagram.PublishReelInput{
		Media: instagram.UploadSource{
			Reader: bytes.NewReader(video), Filename: in.Filename, MIMEType: in.MIMEType,
			Size: in.ByteSize, Width: in.Width, Height: in.Height, Duration: time.Duration(in.DurationMS) * time.Millisecond,
		},
		Thumbnail: instagram.UploadSource{
			Reader: bytes.NewReader(thumb), Filename: in.ThumbnailName, MIMEType: "image/jpeg",
			Size: in.ThumbnailSize, Width: in.ThumbnailWidth, Height: in.ThumbnailHeight,
		},
		Caption: in.Caption, IdempotencyKey: in.IdempotencyKey,
	})
	if err != nil {
		return nil, publishingToolError(err)
	}
	return result, nil
}

func publishStory(ctx context.Context, c *instagram.Client, in PublishStoryInput) (any, error) {
	if err := requireMutationConfirmation(in.ConfirmMutation); err != nil {
		return nil, err
	}
	limit := int64(maxMCPPhotoBytes)
	if strings.EqualFold(strings.TrimSpace(in.MIMEType), "video/mp4") {
		limit = maxMCPVideoBytes
	}
	media, err := decodeMCPMedia(in.MediaBase64, in.ByteSize, limit)
	if err != nil {
		return nil, err
	}
	var thumbnail *instagram.UploadSource
	if in.ThumbnailBase64 != "" || in.ThumbnailSize != 0 {
		thumb, err := decodeMCPMedia(in.ThumbnailBase64, in.ThumbnailSize, maxMCPPhotoBytes)
		if err != nil {
			return nil, prefixToolError("invalid thumbnail: ", err)
		}
		thumbnail = &instagram.UploadSource{
			Reader: bytes.NewReader(thumb), Filename: in.ThumbnailName, MIMEType: "image/jpeg",
			Size: in.ThumbnailSize, Width: in.ThumbnailWidth, Height: in.ThumbnailHeight,
		}
	}
	callCtx, cancel := context.WithTimeout(ctx, maxMCPTimeout)
	defer cancel()
	result, err := c.PublishStory(callCtx, instagram.PublishStoryInput{
		Media: instagram.UploadSource{
			Reader: bytes.NewReader(media), Filename: in.Filename, MIMEType: in.MIMEType,
			Size: in.ByteSize, Width: in.Width, Height: in.Height, Duration: time.Duration(in.DurationMS) * time.Millisecond,
		},
		Thumbnail: thumbnail, Caption: in.Caption, IdempotencyKey: in.IdempotencyKey,
	})
	if err != nil {
		return nil, publishingToolError(err)
	}
	return result, nil
}

func requireMutationConfirmation(confirmed bool) error {
	if confirmed {
		return nil
	}
	return &mcptool.Error{
		Code:    "mutation_confirmation_required",
		Message: "confirm_mutation must be true before publishing content to Instagram",
	}
}

func decodeMCPMedia(encoded string, declared, limit int64) ([]byte, error) {
	if declared <= 0 {
		return nil, &mcptool.Error{Code: "invalid_input", Message: "byte_size must be positive"}
	}
	if declared > limit {
		return nil, &mcptool.Error{Code: "upload_too_large", Message: fmt.Sprintf("decoded media exceeds %d-byte MCP limit", limit)}
	}
	if int64(base64.StdEncoding.DecodedLen(len(encoded))) > limit+2 {
		return nil, &mcptool.Error{Code: "upload_too_large", Message: fmt.Sprintf("encoded media exceeds %d-byte MCP limit", limit)}
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, &mcptool.Error{Code: "invalid_input", Message: "media_base64 is not valid standard base64"}
	}
	if int64(len(raw)) != declared {
		return nil, &mcptool.Error{Code: "invalid_input", Message: "byte_size does not match decoded media length"}
	}
	return raw, nil
}

func publishingToolError(err error) error {
	switch {
	case errors.Is(err, instagram.ErrInvalidAuth), errors.Is(err, instagram.ErrSessionExpired):
		return &mcptool.Error{Code: "credential_expired", Message: "Instagram session expired; reconnect Instagram"}
	case errors.Is(err, instagram.ErrChallengeRequired):
		return &mcptool.Error{Code: "challenge_required", Message: "Instagram requires an account challenge before publishing"}
	case errors.Is(err, instagram.ErrFeedbackRequired):
		return &mcptool.Error{Code: "feedback_required", Message: "Instagram rejected publishing and applied a write cooldown"}
	case errors.Is(err, instagram.ErrWriteSoftBlock), errors.Is(err, instagram.ErrRateLimited):
		return &mcptool.Error{Code: "write_cooldown", Message: "Instagram publishing is write-limited; wait for the client cooldown"}
	case errors.Is(err, instagram.ErrProcessingTimeout):
		return &mcptool.Error{Code: "processing_timeout", Message: "Instagram did not finish processing media before the bounded timeout"}
	case errors.Is(err, instagram.ErrProcessingFailed):
		return &mcptool.Error{Code: "processing_failed", Message: "Instagram media processing reached a terminal failure"}
	case errors.Is(err, instagram.ErrPartialUpload):
		return &mcptool.Error{Code: "partial_upload", Message: "Instagram may hold a partial upload; retry only with the same idempotency key"}
	case errors.Is(err, instagram.ErrUploadTooLarge):
		return &mcptool.Error{Code: "upload_too_large", Message: "media exceeds the configured upload limit"}
	case errors.Is(err, instagram.ErrInvalidPublishInput):
		return &mcptool.Error{Code: "invalid_input", Message: err.Error()}
	default:
		return err
	}
}

func prefixToolError(prefix string, err error) error {
	var toolErr *mcptool.Error
	if errors.As(err, &toolErr) {
		return &mcptool.Error{
			Code: toolErr.Code, Message: prefix + toolErr.Message, Retryable: toolErr.Retryable, Data: toolErr.Data,
		}
	}
	return &mcptool.Error{Code: "invalid_input", Message: prefix + err.Error()}
}

func classifiedPublishingTool(tool mcptool.Tool) mcptool.Tool {
	tool.Tags = []string{"write", "publishing", "mutation"}
	return tool
}

var publishingTools = []mcptool.Tool{
	classifiedPublishingTool(mcptool.Define[*instagram.Client, PublishPhotoInput](
		"instagram_publish_photo",
		"Publish one confirmed, size-bounded JPEG photo to Instagram",
		"PublishPhoto",
		publishPhoto,
	)),
	classifiedPublishingTool(mcptool.Define[*instagram.Client, PublishReelInput](
		"instagram_publish_reel",
		"Publish one confirmed, size-bounded MP4 Reel and JPEG thumbnail",
		"PublishReel",
		publishReel,
	)),
	classifiedPublishingTool(mcptool.Define[*instagram.Client, PublishStoryInput](
		"instagram_publish_story",
		"Publish one confirmed, size-bounded photo or video Story",
		"PublishStory",
		publishStory,
	)),
}
