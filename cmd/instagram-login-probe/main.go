// Command instagram-login-probe is a smoke test for credential login via the
// social-login sidecar. It logs in with INSTAGRAM_USERNAME/INSTAGRAM_PASSWORD
// through the sidecar, then calls Me() to confirm the minted session is live.
//
// Usage:
//
//	INSTAGRAM_USERNAME=.. INSTAGRAM_PASSWORD=.. \
//	SOCIAL_LOGIN_SIDECAR_URL=http://localhost:8190 [INSTAGRAM_PROXY_URL=..] \
//	go run ./cmd/instagram-login-probe
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	instagram "github.com/teslashibe/instagram-go"
)

func main() {
	user := os.Getenv("INSTAGRAM_USERNAME")
	pass := os.Getenv("INSTAGRAM_PASSWORD")
	sidecar := os.Getenv("SOCIAL_LOGIN_SIDECAR_URL")
	if user == "" || pass == "" || sidecar == "" {
		fmt.Fprintln(os.Stderr, "set INSTAGRAM_USERNAME, INSTAGRAM_PASSWORD, SOCIAL_LOGIN_SIDECAR_URL")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	res, err := instagram.Login(ctx, instagram.LoginParams{
		Username:   user,
		Password:   pass,
		SidecarURL: sidecar,
		ProxyURL:   os.Getenv("INSTAGRAM_PROXY_URL"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "login:", err)
		os.Exit(1)
	}
	fmt.Printf("login ok: sessionid_len=%d ds_user_id=%s\n", len(res.Cookies.SessionID), res.Cookies.DSUserID)

	c, err := instagram.New(res.Cookies)
	if err != nil {
		fmt.Fprintln(os.Stderr, "new:", err)
		os.Exit(1)
	}
	me, err := c.Me(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "me:", err)
		os.Exit(1)
	}
	fmt.Printf("PASS: authenticated as @%s (id=%s)\n", me.Username, me.ID)
}
