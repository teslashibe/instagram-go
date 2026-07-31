// Command instagram-login-probe is an end-to-end inventory smoke test. It
// logs in with INSTAGRAM_USERNAME/INSTAGRAM_PASSWORD through the social-login
// sidecar, validates the minted session, then resolves a keyword to a hashtag
// and fetches at least one media post.
//
// Usage:
//
//	INSTAGRAM_USERNAME=.. INSTAGRAM_PASSWORD=.. \
//	SOCIAL_LOGIN_SIDECAR_URL=http://localhost:8190 [INSTAGRAM_PROXY_URL=..] \
//	[INSTAGRAM_SEARCH_QUERY=nature] go run ./cmd/instagram-login-probe
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	instagram "github.com/teslashibe/instagram-go"
)

const (
	defaultSearchQuery       = "nature"
	maxHashtagCandidates     = 3
	defaultProbeTimeout      = 120 * time.Second
	missingConfigurationExit = 2
)

type probeConfig struct {
	username   string
	password   string
	sidecarURL string
	proxyURL   string
	query      string
}

type postIterator interface {
	Next(context.Context) bool
	Item() *instagram.Post
	Err() error
}

type inventoryProbeClient struct {
	me           func(context.Context) (*instagram.User, error)
	search       func(context.Context, string) (*instagram.SearchResult, error)
	hashtagPosts func(string) postIterator
}

type probeDependencies struct {
	login     func(context.Context, instagram.LoginParams) (instagram.LoginResult, error)
	newClient func(instagram.Cookies) (*inventoryProbeClient, error)
}

func main() {
	os.Exit(runMain(os.Getenv, os.Stdout, os.Stderr))
}

func runMain(getenv func(string) string, stdout, stderr io.Writer) int {
	cfg := configFromEnv(getenv)
	if cfg.username == "" || cfg.password == "" || cfg.sidecarURL == "" {
		fmt.Fprintln(stderr, "set INSTAGRAM_USERNAME, INSTAGRAM_PASSWORD, SOCIAL_LOGIN_SIDECAR_URL")
		return missingConfigurationExit
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultProbeTimeout)
	defer cancel()
	if err := runProbe(ctx, cfg, defaultDependencies(), stdout); err != nil {
		fmt.Fprintln(stderr, "probe:", err)
		return 1
	}
	return 0
}

func configFromEnv(getenv func(string) string) probeConfig {
	query := strings.TrimSpace(getenv("INSTAGRAM_SEARCH_QUERY"))
	if query == "" {
		query = defaultSearchQuery
	}
	return probeConfig{
		username:   getenv("INSTAGRAM_USERNAME"),
		password:   getenv("INSTAGRAM_PASSWORD"),
		sidecarURL: getenv("SOCIAL_LOGIN_SIDECAR_URL"),
		proxyURL:   getenv("INSTAGRAM_PROXY_URL"),
		query:      query,
	}
}

func defaultDependencies() probeDependencies {
	return probeDependencies{
		login: instagram.Login,
		newClient: func(cookies instagram.Cookies) (*inventoryProbeClient, error) {
			client, err := instagram.New(cookies)
			if err != nil {
				return nil, err
			}
			return &inventoryProbeClient{
				me:     client.Me,
				search: client.Search,
				hashtagPosts: func(name string) postIterator {
					return client.GetHashtagPosts(name).WithMaxPages(1)
				},
			}, nil
		},
	}
}

func runProbe(ctx context.Context, cfg probeConfig, deps probeDependencies, out io.Writer) error {
	loginResult, err := deps.login(ctx, instagram.LoginParams{
		Username:   cfg.username,
		Password:   cfg.password,
		SidecarURL: cfg.sidecarURL,
		ProxyURL:   cfg.proxyURL,
	})
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	client, err := deps.newClient(loginResult.Cookies)
	if err != nil {
		return fmt.Errorf("validate session: %w", err)
	}
	me, err := client.me(ctx)
	if err != nil {
		return fmt.Errorf("authenticated user: %w", err)
	}
	fmt.Fprintf(out, "PASS: authenticated as @%s (id=%s)\n", me.Username, me.ID)

	searchResult, err := client.search(ctx, cfg.query)
	if err != nil {
		return fmt.Errorf("keyword search %q: %w", cfg.query, err)
	}
	candidates := hashtagCandidates(searchResult)
	if len(candidates) == 0 {
		return fmt.Errorf("keyword search %q returned no hashtag candidates", cfg.query)
	}

	for _, hashtag := range candidates {
		posts, err := collectOnePage(ctx, client.hashtagPosts(hashtag))
		if err != nil {
			return fmt.Errorf("keyword search %q hashtag #%s: %w", cfg.query, hashtag, err)
		}
		if len(posts) == 0 {
			continue
		}
		firstID, firstURL := postEvidence(posts[0])
		fmt.Fprintf(out, "PASS: keyword search query=%q hashtag=#%s posts=%d first_post=%s permalink=%s\n",
			cfg.query, hashtag, len(posts), firstID, firstURL)
		return nil
	}

	return fmt.Errorf("keyword search %q returned zero media posts from %d hashtag candidates", cfg.query, len(candidates))
}

func hashtagCandidates(result *instagram.SearchResult) []string {
	if result == nil {
		return nil
	}
	seen := make(map[string]bool)
	out := make([]string, 0, maxHashtagCandidates)
	for _, hashtag := range result.Hashtags {
		if hashtag == nil {
			continue
		}
		name := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(hashtag.Name)), "#")
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
		if len(out) == maxHashtagCandidates {
			break
		}
	}
	return out
}

func collectOnePage(ctx context.Context, iterator postIterator) ([]*instagram.Post, error) {
	if iterator == nil {
		return nil, fmt.Errorf("post iterator is nil")
	}
	var posts []*instagram.Post
	for iterator.Next(ctx) {
		if post := iterator.Item(); post != nil {
			posts = append(posts, post)
		}
	}
	if err := iterator.Err(); err != nil {
		return nil, err
	}
	return posts, nil
}

func postEvidence(post *instagram.Post) (string, string) {
	if post == nil {
		return "unknown", "unknown"
	}
	id := post.Code
	if id == "" {
		id = post.PK
	}
	if id == "" {
		id = "unknown"
	}
	url := post.PermalinkURL
	if url == "" && post.Code != "" {
		url = "https://www.instagram.com/p/" + post.Code + "/"
	}
	if url == "" {
		url = "unknown"
	}
	return id, url
}
