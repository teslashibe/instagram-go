package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	instagram "github.com/teslashibe/instagram-go"
)

type fakePostIterator struct {
	items []*instagram.Post
	err   error
	index int
}

func (it *fakePostIterator) Next(context.Context) bool {
	if it.index >= len(it.items) {
		return false
	}
	it.index++
	return true
}

func (it *fakePostIterator) Item() *instagram.Post {
	if it.index == 0 || it.index > len(it.items) {
		return nil
	}
	return it.items[it.index-1]
}

func (it *fakePostIterator) Err() error { return it.err }

func TestRunProbeKeywordSearchReturnsPosts(t *testing.T) {
	cfg := probeConfig{
		username:   "burner@example.com",
		password:   "super-secret-password",
		sidecarURL: "http://sidecar.test",
		proxyURL:   "http://proxy.test",
		query:      defaultSearchQuery,
	}
	cookies := instagram.Cookies{SessionID: "secret-session", CSRFToken: "secret-csrf", DSUserID: "123"}
	var searched, selected string
	deps := successfulDependencies(cookies)
	deps.login = func(_ context.Context, params instagram.LoginParams) (instagram.LoginResult, error) {
		if params.Username != cfg.username || params.Password != cfg.password || params.SidecarURL != cfg.sidecarURL || params.ProxyURL != cfg.proxyURL {
			t.Fatalf("unexpected login params: %#v", params)
		}
		return instagram.LoginResult{Cookies: cookies}, nil
	}
	deps.newClient = func(got instagram.Cookies) (*inventoryProbeClient, error) {
		if got != cookies {
			t.Fatalf("cookies = %#v, want minted cookies", got)
		}
		return &inventoryProbeClient{
			me: func(context.Context) (*instagram.User, error) {
				return &instagram.User{ID: "123", Username: "burner"}, nil
			},
			search: func(_ context.Context, query string) (*instagram.SearchResult, error) {
				searched = query
				return &instagram.SearchResult{Hashtags: []*instagram.Hashtag{
					{Name: "Nature"},
					{Name: "nature"},
				}}, nil
			},
			hashtagPosts: func(name string) postIterator {
				selected = name
				return &fakePostIterator{items: []*instagram.Post{
					{PK: "456", Code: "ABC123", PermalinkURL: "https://www.instagram.com/p/ABC123/"},
					{PK: "789", Code: "DEF456"},
				}}
			},
		}, nil
	}

	var out bytes.Buffer
	if err := runProbe(context.Background(), cfg, deps, &out); err != nil {
		t.Fatalf("runProbe: %v", err)
	}
	if searched != defaultSearchQuery {
		t.Fatalf("searched %q, want %q", searched, defaultSearchQuery)
	}
	if selected != "nature" {
		t.Fatalf("selected hashtag %q, want nature", selected)
	}
	for _, want := range []string{"authenticated as @burner", `query="nature"`, "hashtag=#nature", "posts=2", "first_post=ABC123"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output %q does not contain %q", out.String(), want)
		}
	}
	for _, secret := range []string{cfg.username, cfg.password, cookies.SessionID, cookies.CSRFToken} {
		if strings.Contains(out.String(), secret) {
			t.Errorf("output leaked secret %q: %s", secret, out.String())
		}
	}
}

func TestRunProbeFailures(t *testing.T) {
	probeErr := errors.New("probe dependency failed")
	tests := []struct {
		name string
		edit func(*probeDependencies)
		want string
	}{
		{
			name: "login",
			edit: func(deps *probeDependencies) {
				deps.login = func(context.Context, instagram.LoginParams) (instagram.LoginResult, error) {
					return instagram.LoginResult{}, probeErr
				}
			},
			want: "login",
		},
		{
			name: "session validation",
			edit: func(deps *probeDependencies) {
				deps.newClient = func(instagram.Cookies) (*inventoryProbeClient, error) { return nil, probeErr }
			},
			want: "validate session",
		},
		{
			name: "authenticated user",
			edit: func(deps *probeDependencies) {
				deps.newClient = func(instagram.Cookies) (*inventoryProbeClient, error) {
					client := successfulClient()
					client.me = func(context.Context) (*instagram.User, error) { return nil, probeErr }
					return client, nil
				}
			},
			want: "authenticated user",
		},
		{
			name: "keyword search",
			edit: func(deps *probeDependencies) {
				deps.newClient = func(instagram.Cookies) (*inventoryProbeClient, error) {
					client := successfulClient()
					client.search = func(context.Context, string) (*instagram.SearchResult, error) { return nil, probeErr }
					return client, nil
				}
			},
			want: "keyword search",
		},
		{
			name: "no hashtag candidates",
			edit: func(deps *probeDependencies) {
				deps.newClient = func(instagram.Cookies) (*inventoryProbeClient, error) {
					client := successfulClient()
					client.search = func(context.Context, string) (*instagram.SearchResult, error) {
						return &instagram.SearchResult{}, nil
					}
					return client, nil
				}
			},
			want: "no hashtag candidates",
		},
		{
			name: "post iterator",
			edit: func(deps *probeDependencies) {
				deps.newClient = func(instagram.Cookies) (*inventoryProbeClient, error) {
					client := successfulClient()
					client.hashtagPosts = func(string) postIterator { return &fakePostIterator{err: probeErr} }
					return client, nil
				}
			},
			want: "probe dependency failed",
		},
		{
			name: "zero posts",
			edit: func(deps *probeDependencies) {
				deps.newClient = func(instagram.Cookies) (*inventoryProbeClient, error) {
					client := successfulClient()
					client.hashtagPosts = func(string) postIterator { return &fakePostIterator{} }
					return client, nil
				}
			},
			want: "zero media posts",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deps := successfulDependencies(instagram.Cookies{})
			test.edit(&deps)
			err := runProbe(context.Background(), probeConfig{query: defaultSearchQuery}, deps, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want error containing %q", err, test.want)
			}
		})
	}
}

func TestRunProbeTriesUpToThreeHashtagCandidates(t *testing.T) {
	client := successfulClient()
	client.search = func(context.Context, string) (*instagram.SearchResult, error) {
		return &instagram.SearchResult{Hashtags: []*instagram.Hashtag{
			{Name: "one"}, {Name: "two"}, {Name: "three"}, {Name: "four"},
		}}, nil
	}
	var visited []string
	client.hashtagPosts = func(name string) postIterator {
		visited = append(visited, name)
		if name == "three" {
			return &fakePostIterator{items: []*instagram.Post{{PK: "1"}}}
		}
		return &fakePostIterator{}
	}
	deps := successfulDependencies(instagram.Cookies{})
	deps.newClient = func(instagram.Cookies) (*inventoryProbeClient, error) { return client, nil }

	if err := runProbe(context.Background(), probeConfig{query: "topic"}, deps, &bytes.Buffer{}); err != nil {
		t.Fatalf("runProbe: %v", err)
	}
	if got, want := strings.Join(visited, ","), "one,two,three"; got != want {
		t.Fatalf("visited %q, want %q", got, want)
	}
}

func TestRunMainMissingConfiguration(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runMain(func(string) string { return "" }, &stdout, &stderr); code != missingConfigurationExit {
		t.Fatalf("exit code = %d, want %d", code, missingConfigurationExit)
	}
	if !strings.Contains(stderr.String(), "INSTAGRAM_USERNAME") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestLiveInventoryProbe(t *testing.T) {
	if os.Getenv("INSTAGRAM_LIVE_TEST") != "1" {
		t.Skip("set INSTAGRAM_LIVE_TEST=1 to run the burner-credential inventory probe")
	}
	cfg := configFromEnv(os.Getenv)
	var missing []string
	for name, value := range map[string]string{
		"INSTAGRAM_USERNAME":       cfg.username,
		"INSTAGRAM_PASSWORD":       cfg.password,
		"SOCIAL_LOGIN_SIDECAR_URL": cfg.sidecarURL,
	} {
		if value == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("INSTAGRAM_LIVE_TEST=1 requires %s", strings.Join(missing, ", "))
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultProbeTimeout)
	defer cancel()
	var out bytes.Buffer
	if err := runProbe(ctx, cfg, defaultDependencies(), &out); err != nil {
		t.Fatalf("live inventory probe: %v", err)
	}
	t.Log(strings.TrimSpace(out.String()))
}

func successfulDependencies(cookies instagram.Cookies) probeDependencies {
	return probeDependencies{
		login: func(context.Context, instagram.LoginParams) (instagram.LoginResult, error) {
			return instagram.LoginResult{Cookies: cookies}, nil
		},
		newClient: func(instagram.Cookies) (*inventoryProbeClient, error) {
			return successfulClient(), nil
		},
	}
}

func successfulClient() *inventoryProbeClient {
	return &inventoryProbeClient{
		me: func(context.Context) (*instagram.User, error) {
			return &instagram.User{ID: "123", Username: "burner"}, nil
		},
		search: func(context.Context, string) (*instagram.SearchResult, error) {
			return &instagram.SearchResult{Hashtags: []*instagram.Hashtag{{Name: "nature"}}}, nil
		},
		hashtagPosts: func(string) postIterator {
			return &fakePostIterator{items: []*instagram.Post{{PK: "456", Code: "ABC123"}}}
		},
	}
}

func Example_runProbe() {
	fmt.Println("PASS: keyword search query=\"nature\" hashtag=#nature posts=12 first_post=ABC123 permalink=https://www.instagram.com/p/ABC123/")
	// Output:
	// PASS: keyword search query="nature" hashtag=#nature posts=12 first_post=ABC123 permalink=https://www.instagram.com/p/ABC123/
}

func TestDefaultProbeTimeoutAllowsLiveLogin(t *testing.T) {
	if defaultProbeTimeout < 2*time.Minute {
		t.Fatalf("default probe timeout %s is too short for browser login", defaultProbeTimeout)
	}
}
