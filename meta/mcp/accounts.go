package mcp

import (
	"context"

	"github.com/teslashibe/instagram-go/meta"
	"github.com/teslashibe/mcptool"
)

type ListPagesInput struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"description=maximum Pages to return,minimum=1,maximum=100,default=25"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque next_cursor from a previous response"`
}

func listPages(ctx context.Context, c *meta.Client, in ListPagesInput) (any, error) {
	page, err := c.ListPages(ctx, meta.ListOptions{Limit: in.Limit, Cursor: in.Cursor})
	return page, toolError(err)
}

type SelectInstagramAccountInput struct {
	PageID             string `json:"page_id" jsonschema:"description=Facebook Page ID,required"`
	InstagramAccountID string `json:"instagram_account_id" jsonschema:"description=linked Instagram professional account ID,required"`
}

func selectInstagramAccount(ctx context.Context, c *meta.Client, in SelectInstagramAccountInput) (any, error) {
	account, err := c.SelectInstagramBusinessAccount(ctx, in.PageID, in.InstagramAccountID)
	return account, toolError(err)
}

type ListAdAccountsInput struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"description=maximum ad accounts to return,minimum=1,maximum=100,default=25"`
	Cursor string `json:"cursor,omitempty" jsonschema:"description=opaque next_cursor from a previous response"`
}

func listAdAccounts(ctx context.Context, c *meta.Client, in ListAdAccountsInput) (any, error) {
	page, err := c.ListAdAccounts(ctx, meta.ListOptions{Limit: in.Limit, Cursor: in.Cursor})
	return page, toolError(err)
}

type GetAdAccountInput struct {
	AdAccountID string `json:"ad_account_id" jsonschema:"description=numeric ad account ID with optional act_ prefix,required"`
}

func getAdAccount(ctx context.Context, c *meta.Client, in GetAdAccountInput) (any, error) {
	account, err := c.GetAdAccount(ctx, in.AdAccountID)
	return account, toolError(err)
}

func selectAdAccount(ctx context.Context, c *meta.Client, in GetAdAccountInput) (any, error) {
	account, err := c.SelectAdAccount(ctx, in.AdAccountID)
	return account, toolError(err)
}

var accountTools = []mcptool.Tool{
	mcptool.Define[*meta.Client, ListPagesInput]("instagram_meta_list_pages", "List Facebook Pages and their linked Instagram professional accounts", "ListPages", listPages),
	mcptool.Define[*meta.Client, SelectInstagramAccountInput]("instagram_meta_select_account", "Verify and select a Page-linked Instagram professional account", "SelectInstagramBusinessAccount", selectInstagramAccount),
	mcptool.Define[*meta.Client, ListAdAccountsInput]("instagram_meta_list_ad_accounts", "List ad accounts available to the connected Meta user", "ListAdAccounts", listAdAccounts),
	mcptool.Define[*meta.Client, GetAdAccountInput]("instagram_meta_get_ad_account", "Read one Meta ad account including currency and timezone", "GetAdAccount", getAdAccount),
	mcptool.Define[*meta.Client, GetAdAccountInput]("instagram_meta_select_ad_account", "Verify and select a Meta ad account for later reads", "SelectAdAccount", selectAdAccount),
}
