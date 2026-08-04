package mcp

import (
	"context"
	"errors"
	"time"

	instagram "github.com/teslashibe/instagram-go"
	"github.com/teslashibe/mcptool"
)

// GetRateLimitInput is the typed input for instagram_get_rate_limit.
type GetRateLimitInput struct{}

func getRateLimit(_ context.Context, c *instagram.Client, _ GetRateLimitInput) (any, error) {
	return c.RateLimit(), nil
}

// WaitForCooldownInput is the typed input for instagram_wait_for_cooldown.
type WaitForCooldownInput struct {
	TimeoutSeconds int `json:"timeout_seconds,omitempty" jsonschema:"description=maximum seconds to wait before returning,minimum=1,maximum=300,default=30"`
}

func waitForCooldown(ctx context.Context, c *instagram.Client, in WaitForCooldownInput) (any, error) {
	timeout := in.TimeoutSeconds
	if timeout <= 0 {
		timeout = 30
	}
	if timeout > 300 {
		timeout = 300
	}
	waitCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Second)
	defer cancel()

	err := c.WaitForCooldown(waitCtx)
	if errors.Is(err, context.DeadlineExceeded) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return map[string]any{
			"cleared":    false,
			"rate_limit": c.RateLimit(),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"cleared":    true,
		"rate_limit": c.RateLimit(),
	}, nil
}

var observabilityTools = []mcptool.Tool{
	mcptool.Define[*instagram.Client, GetRateLimitInput](
		"instagram_get_rate_limit",
		"Read Instagram request capacity, cooldown, and connection telemetry",
		"RateLimit",
		getRateLimit,
	),
	mcptool.Define[*instagram.Client, WaitForCooldownInput](
		"instagram_wait_for_cooldown",
		"Wait up to five minutes for Instagram read and write cooldowns to clear",
		"WaitForCooldown",
		waitForCooldown,
	),
}
