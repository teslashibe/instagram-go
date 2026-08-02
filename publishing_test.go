package instagram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type publishRecordedRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

type publishTransport struct {
	mu       sync.Mutex
	requests []publishRecordedRequest
	handler  func(*http.Request, []byte) (int, string, error)
}

func (p *publishTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
	}
	p.mu.Lock()
	p.requests = append(p.requests, publishRecordedRequest{
		Method: req.Method, Path: req.URL.RequestURI(), Header: req.Header.Clone(), Body: append([]byte(nil), body...),
	})
	p.mu.Unlock()
	status, response, err := p.handler(req, body)
	if err != nil {
		return nil, err
	}
	return &http.Response{
		StatusCode: status, Status: http.StatusText(status), Header: make(http.Header),
		Body: io.NopCloser(strings.NewReader(response)), Request: req,
	}, nil
}

func newPublishingClient(t *testing.T, transport *publishTransport, opts ...Option) *Client {
	t.Helper()
	options := []Option{
		WithAPIHost("https://i.instagram.test"),
		WithHTTPClient(&http.Client{Transport: transport}),
		WithSkipSessionValidation(), WithMinRequestGap(0), WithMinWriteGap(0), WithRetry(1, 0),
		WithPublishingTimeouts(time.Second, time.Second),
	}
	options = append(options, opts...)
	client, err := New(Cookies{SessionID: "session", CSRFToken: "csrf", DSUserID: "42"}, options...)
	if err != nil {
		t.Fatal(err)
	}
	client.allowUnverifiedPublishing = true
	return client
}

func photoSource(raw []byte) UploadSource {
	return UploadSource{
		Reader: bytes.NewReader(raw), Filename: "burner.jpg", MIMEType: "image/jpeg",
		Size: int64(len(raw)), Width: 1080, Height: 1350,
	}
}

func videoSource(raw []byte) UploadSource {
	return UploadSource{
		Reader: bytes.NewReader(raw), Filename: "burner.mp4", MIMEType: "video/mp4",
		Size: int64(len(raw)), Width: 1080, Height: 1920, Duration: 3 * time.Second,
	}
}

func successfulPublishHandler(req *http.Request, _ []byte) (int, string, error) {
	switch {
	case strings.Contains(req.URL.Path, "/rupload_ig"):
		var params struct {
			UploadID string `json:"upload_id"`
		}
		if err := json.Unmarshal([]byte(req.Header.Get("X-Instagram-Rupload-Params")), &params); err != nil {
			return 0, "", err
		}
		return http.StatusOK, `{"status":"ok","upload_id":"` + params.UploadID + `"}`, nil
	case req.URL.Path == "/api/v1/media/upload_finish/":
		return http.StatusOK, `{"status":"ok"}`, nil
	case req.URL.Path == "/api/v1/media/upload_status/":
		return http.StatusOK, `{"status":"ok","processing_info":{"state":"ready"}}`, nil
	case strings.Contains(req.URL.Path, "configure"):
		return http.StatusOK, `{"status":"ok","media":{"pk":"999","code":"BURNER","media_type":1}}`, nil
	case strings.HasSuffix(req.URL.Path, "/delete/"):
		return http.StatusOK, `{"status":"ok"}`, nil
	default:
		return http.StatusNotFound, `{}`, nil
	}
}

func TestPublishingFailsClosedWithoutReviewedCaptureBeforeReadingOrHTTP(t *testing.T) {
	transport := &publishTransport{handler: successfulPublishHandler}
	options := []Option{
		WithAPIHost("https://i.instagram.test"),
		WithHTTPClient(&http.Client{Transport: transport}),
		WithSkipSessionValidation(),
	}
	client, err := New(Cookies{SessionID: "session", CSRFToken: "csrf", DSUserID: "42"}, options...)
	if err != nil {
		t.Fatal(err)
	}
	reader := &countingReader{Reader: strings.NewReader("photo")}
	_, err = client.PublishPhoto(context.Background(), PublishPhotoInput{
		Media: UploadSource{
			Reader: reader, Filename: "burner.jpg", MIMEType: "image/jpeg",
			Size: 5, Width: 1, Height: 1,
		},
		IdempotencyKey: "capture-gate",
	})
	if !errors.Is(err, ErrPublishingCaptureRequired) {
		t.Fatalf("error = %v, want ErrPublishingCaptureRequired", err)
	}
	if reader.Reads != 0 || len(transport.requests) != 0 {
		t.Fatalf("capture-gated publish consumed %d reads and made %d requests", reader.Reads, len(transport.requests))
	}
}

type countingReader struct {
	io.Reader
	Reads int
}

func (r *countingReader) Read(p []byte) (int, error) {
	r.Reads++
	return r.Reader.Read(p)
}

func TestPublishPhotoUsesDeterministicCapturedContract(t *testing.T) {
	raw := []byte("disposable-photo")
	run := func() (*PublishResult, []publishRecordedRequest) {
		transport := &publishTransport{handler: successfulPublishHandler}
		client := newPublishingClient(t, transport)
		result, err := client.PublishPhoto(context.Background(), PublishPhotoInput{
			Media: photoSource(raw), Caption: "burner", IdempotencyKey: "photo-test-1",
		})
		if err != nil {
			t.Fatalf("PublishPhoto: %v", err)
		}
		return result, transport.requests
	}
	first, firstRequests := run()
	second, secondRequests := run()
	if first.UploadID != second.UploadID || first.ClientID != second.ClientID {
		t.Fatalf("IDs are not deterministic: %#v vs %#v", first, second)
	}
	if first.MediaID != "999" || first.Kind != "photo" {
		t.Fatalf("result = %#v", first)
	}
	if len(firstRequests) != 3 || len(secondRequests) != 3 {
		t.Fatalf("request counts = %d, %d", len(firstRequests), len(secondRequests))
	}
	upload := firstRequests[0]
	if upload.Method != http.MethodPost || !strings.HasPrefix(upload.Path, "/rupload_igphoto/") {
		t.Fatalf("upload request = %s %s", upload.Method, upload.Path)
	}
	if !bytes.Equal(upload.Body, raw) || upload.Header.Get("X-Entity-Length") != "16" ||
		upload.Header.Get("X-Entity-Type") != "image/jpeg" || upload.Header.Get("Offset") != "0" {
		t.Fatalf("upload headers/body = %#v %q", upload.Header, upload.Body)
	}
	var params map[string]string
	if err := json.Unmarshal([]byte(upload.Header.Get("X-Instagram-Rupload-Params")), &params); err != nil {
		t.Fatal(err)
	}
	if params["upload_id"] != first.UploadID || params["upload_media_width"] != "1080" {
		t.Fatalf("rupload params = %#v", params)
	}
	if !strings.HasPrefix(firstRequests[1].Path, "/api/v1/media/upload_status/?") {
		t.Fatalf("status path = %s", firstRequests[1].Path)
	}
	configure := firstRequests[2]
	if configure.Path != "/api/v1/media/configure/" {
		t.Fatalf("configure path = %s", configure.Path)
	}
	form, _ := url.ParseQuery(string(configure.Body))
	if form.Get("upload_id") != first.UploadID || form.Get("client_context") != first.ClientID ||
		form.Get("caption") != "burner" {
		t.Fatalf("configure form = %#v", form)
	}
}

func TestPublishReelAndVideoStorySequence(t *testing.T) {
	tests := []struct {
		name      string
		call      func(*Client) (*PublishResult, error)
		configure string
		kind      PublishedMediaKind
	}{
		{
			name: "reel", configure: "/api/v1/media/configure_to_clips/", kind: PublishedMediaReel,
			call: func(c *Client) (*PublishResult, error) {
				return c.PublishReel(context.Background(), PublishReelInput{
					Media: videoSource([]byte("video")), Thumbnail: photoSource([]byte("thumb")),
					Caption: "burner reel", IdempotencyKey: "reel-1",
				})
			},
		},
		{
			name: "story", configure: "/api/v1/media/configure_to_story/", kind: PublishedMediaStory,
			call: func(c *Client) (*PublishResult, error) {
				thumb := photoSource([]byte("thumb"))
				return c.PublishStory(context.Background(), PublishStoryInput{
					Media: videoSource([]byte("video")), Thumbnail: &thumb,
					Caption: "burner story", IdempotencyKey: "story-1",
				})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &publishTransport{handler: successfulPublishHandler}
			result, err := tt.call(newPublishingClient(t, transport))
			if err != nil {
				t.Fatal(err)
			}
			if result.Kind != tt.kind || len(transport.requests) != 5 {
				t.Fatalf("result/requests = %#v / %d", result, len(transport.requests))
			}
			want := []string{
				"/rupload_igvideo/", "/rupload_igphoto/", "/api/v1/media/upload_finish/",
				"/api/v1/media/upload_status/", tt.configure,
			}
			for index, prefix := range want {
				if !strings.HasPrefix(transport.requests[index].Path, prefix) {
					t.Fatalf("request %d = %s, want prefix %s", index, transport.requests[index].Path, prefix)
				}
			}
			if !strings.HasSuffix(strings.Split(transport.requests[1].Path, "?")[0], "_0") {
				t.Fatalf("thumbnail entity path = %s", transport.requests[1].Path)
			}
		})
	}
}

func TestPublishStoryRejectsUncapturedPhotoStoryBeforeHTTP(t *testing.T) {
	transport := &publishTransport{handler: successfulPublishHandler}
	client := newPublishingClient(t, transport)
	_, err := client.PublishStory(context.Background(), PublishStoryInput{
		Media: photoSource([]byte("photo")), IdempotencyKey: "photo-story",
	})
	if !errors.Is(err, ErrInvalidPublishInput) || !strings.Contains(err.Error(), "only captured video Story") {
		t.Fatalf("error = %v", err)
	}
	if len(transport.requests) != 0 {
		t.Fatalf("photo Story made %d HTTP requests", len(transport.requests))
	}
}

func TestPublishingRejectsOversizeAndMismatchedStreamsBeforeHTTP(t *testing.T) {
	transport := &publishTransport{handler: successfulPublishHandler}
	client := newPublishingClient(t, transport, WithPublishingLimits(4, 8))
	source := photoSource([]byte("12345"))
	_, err := client.PublishPhoto(context.Background(), PublishPhotoInput{
		Media: source, IdempotencyKey: "oversize",
	})
	if !errors.Is(err, ErrUploadTooLarge) {
		t.Fatalf("oversize error = %v", err)
	}
	source = photoSource([]byte("123"))
	source.Size = 4
	_, err = client.PublishPhoto(context.Background(), PublishPhotoInput{
		Media: source, IdempotencyKey: "mismatch",
	})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatch error = %v", err)
	}
	if len(transport.requests) != 0 {
		t.Fatalf("invalid uploads made %d requests", len(transport.requests))
	}
}

func TestPublishingClassifiesFeedbackAndPartialConfigureFailure(t *testing.T) {
	transport := &publishTransport{handler: func(req *http.Request, body []byte) (int, string, error) {
		if strings.Contains(req.URL.Path, "rupload") {
			return successfulPublishHandler(req, body)
		}
		return http.StatusOK, `{"status":"fail","message":"feedback_required"}`, nil
	}}
	client := newPublishingClient(t, transport, WithRateLimitCooldown(time.Millisecond, time.Minute))
	_, err := client.PublishPhoto(context.Background(), PublishPhotoInput{
		Media: photoSource([]byte("photo")), IdempotencyKey: "feedback",
	})
	for _, want := range []error{ErrFeedbackRequired, ErrWriteSoftBlock, ErrPartialUpload} {
		if !errors.Is(err, want) {
			t.Fatalf("error = %v, want errors.Is(%v)", err, want)
		}
	}
	if !client.RateLimit().CooldownWriteUntil.After(time.Now()) {
		t.Fatal("feedback did not apply write cooldown")
	}
}

func TestPublishingClassifiesChallengeAndPossiblePartialUpload(t *testing.T) {
	t.Run("challenge is not partial", func(t *testing.T) {
		transport := &publishTransport{handler: func(*http.Request, []byte) (int, string, error) {
			return http.StatusOK, `{"status":"fail","message":"challenge_required"}`, nil
		}}
		client := newPublishingClient(t, transport)
		_, err := client.PublishPhoto(context.Background(), PublishPhotoInput{
			Media: photoSource([]byte("photo")), IdempotencyKey: "challenge",
		})
		if !errors.Is(err, ErrChallengeRequired) || errors.Is(err, ErrPartialUpload) {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("transport failure may be partial", func(t *testing.T) {
		transport := &publishTransport{handler: func(*http.Request, []byte) (int, string, error) {
			return 0, "", errors.New("connection reset after body write")
		}}
		client := newPublishingClient(t, transport)
		_, err := client.PublishPhoto(context.Background(), PublishPhotoInput{
			Media: photoSource([]byte("photo")), IdempotencyKey: "partial",
		})
		if !errors.Is(err, ErrPartialUpload) {
			t.Fatalf("error = %v, want ErrPartialUpload", err)
		}
	})
}

func TestPublishingClassifiesProcessingFailureAndTimeout(t *testing.T) {
	tests := []struct {
		name       string
		statusBody string
		timeout    time.Duration
		want       error
	}{
		{name: "failed", statusBody: `{"status":"ok","processing_info":{"state":"failed"}}`, timeout: time.Second, want: ErrProcessingFailed},
		{name: "timeout", statusBody: `{"status":"ok","processing_info":{"state":"processing"}}`, timeout: 5 * time.Millisecond, want: ErrProcessingTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport := &publishTransport{handler: func(req *http.Request, body []byte) (int, string, error) {
				if req.URL.Path == "/api/v1/media/upload_status/" {
					return http.StatusOK, tt.statusBody, nil
				}
				return successfulPublishHandler(req, body)
			}}
			client := newPublishingClient(t, transport, WithPublishingTimeouts(time.Second, tt.timeout))
			_, err := client.PublishReel(context.Background(), PublishReelInput{
				Media: videoSource([]byte("video")), Thumbnail: photoSource([]byte("thumb")),
				IdempotencyKey: "processing-" + tt.name,
			})
			if !errors.Is(err, tt.want) || !errors.Is(err, ErrPartialUpload) {
				t.Fatalf("error = %v, want %v and partial", err, tt.want)
			}
		})
	}
}

func TestDeleteMediaUsesOnlyExactSuppliedID(t *testing.T) {
	transport := &publishTransport{handler: successfulPublishHandler}
	client := newPublishingClient(t, transport)
	tests := []struct {
		kind PublishedMediaKind
		want string
	}{
		{PublishedMediaPhoto, "PHOTO"},
		{PublishedMediaReel, "VIDEO"},
		{PublishedMediaStory, "STORY"},
	}
	for _, tt := range tests {
		if err := client.DeleteMedia(context.Background(), "999_42", tt.kind); err != nil {
			t.Fatal(err)
		}
		form, err := url.ParseQuery(string(transport.requests[len(transport.requests)-1].Body))
		if err != nil || form.Get("media_type") != tt.want {
			t.Fatalf("delete form = %q, want media_type=%s", transport.requests[len(transport.requests)-1].Body, tt.want)
		}
	}
	if len(transport.requests) != 3 || transport.requests[0].Path != "/api/v1/media/999_42/delete/" {
		t.Fatalf("delete requests = %#v", transport.requests)
	}
	if err := client.DeleteMedia(context.Background(), "../all", PublishedMediaPhoto); err == nil {
		t.Fatal("unsafe media ID accepted")
	}
	if err := client.DeleteMedia(context.Background(), "999_42", "unknown"); !errors.Is(err, ErrInvalidPublishInput) {
		t.Fatalf("unknown kind error = %v", err)
	}
	if len(transport.requests) != 3 {
		t.Fatal("unsafe ID made an HTTP request")
	}
}

func TestPublishingTimeoutOverridesDefaultHTTPClientTimeout(t *testing.T) {
	transport := &publishTransport{handler: func(req *http.Request, body []byte) (int, string, error) {
		if strings.Contains(req.URL.Path, "/rupload_ig") {
			select {
			case <-time.After(40 * time.Millisecond):
				return successfulPublishHandler(req, body)
			case <-req.Context().Done():
				return 0, "", req.Context().Err()
			}
		}
		return successfulPublishHandler(req, body)
	}}
	client := newPublishingClient(t, transport,
		WithHTTPClient(&http.Client{Transport: transport, Timeout: 5 * time.Millisecond}),
		WithPublishingTimeouts(100*time.Millisecond, time.Second),
	)
	if _, err := client.PublishPhoto(context.Background(), PublishPhotoInput{
		Media: photoSource([]byte("photo")), IdempotencyKey: "timeout-override",
	}); err != nil {
		t.Fatalf("publishing deadline was truncated by http.Client.Timeout: %v", err)
	}
}
