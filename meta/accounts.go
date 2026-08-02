package meta

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ListPages returns Facebook Pages visible to the user and their linked
// Instagram professional accounts. Page access tokens are never requested.
func (c *Client) ListPages(ctx context.Context, options ListOptions) (Page[FacebookPage], error) {
	path := "me/accounts"
	q, binding, err := preparePageQuery(path, url.Values{
		"fields": {"id,name,category,instagram_business_account{id,username,name}"},
	}, options)
	if err != nil {
		return Page[FacebookPage]{}, err
	}
	var raw graphPage[FacebookPage]
	if err := c.doJSON(ctx, http.MethodGet, path, q, nil,
		[]Scope{ScopePagesShowList, ScopePagesReadEngagement, ScopeInstagramBasic}, &raw); err != nil {
		return Page[FacebookPage]{}, err
	}
	return finishPage(raw, binding)
}

// SelectInstagramBusinessAccount verifies the Page linkage before updating
// the client's selected professional Instagram account.
func (c *Client) SelectInstagramBusinessAccount(ctx context.Context, pageID, accountID string) (*InstagramBusinessAccount, error) {
	pageID = strings.TrimSpace(pageID)
	accountID = strings.TrimSpace(accountID)
	if pageID == "" || accountID == "" {
		return nil, fmt.Errorf("%w: page_id and instagram_account_id are required", ErrInvalidInput)
	}
	var page FacebookPage
	q := url.Values{"fields": {"id,name,instagram_business_account{id,username,name}"}}
	if err := c.doJSON(ctx, http.MethodGet, pageID, q, nil,
		[]Scope{ScopePagesShowList, ScopePagesReadEngagement, ScopeInstagramBasic}, &page); err != nil {
		return nil, err
	}
	if page.InstagramBusinessAccount == nil {
		return nil, fmt.Errorf("%w: Facebook Page %s has no linked Instagram professional account", ErrNotFound, pageID)
	}
	if page.InstagramBusinessAccount.ID != accountID {
		return nil, fmt.Errorf("%w: Instagram account %s is not linked to Facebook Page %s", ErrInvalidInput, accountID, pageID)
	}
	c.selectionMu.Lock()
	c.selection.PageID = pageID
	c.selection.InstagramAccountID = accountID
	c.selectionMu.Unlock()
	copy := *page.InstagramBusinessAccount
	return &copy, nil
}

func (c *Client) ListAdAccounts(ctx context.Context, options ListOptions) (Page[AdAccount], error) {
	path := "me/adaccounts"
	q, binding, err := preparePageQuery(path, url.Values{
		"fields": {"id,account_id,name,currency,timezone_name,account_status,disable_reason"},
	}, options)
	if err != nil {
		return Page[AdAccount]{}, err
	}
	var raw graphPage[AdAccount]
	if err := c.doJSON(ctx, http.MethodGet, path, q, nil, []Scope{ScopeAdsRead}, &raw); err != nil {
		return Page[AdAccount]{}, err
	}
	return finishPage(raw, binding)
}

func (c *Client) GetAdAccount(ctx context.Context, accountID string) (*AdAccount, error) {
	id, err := requireAdAccountID(accountID)
	if err != nil {
		return nil, err
	}
	var account AdAccount
	q := url.Values{"fields": {"id,account_id,name,currency,timezone_name,account_status,disable_reason"}}
	if err := c.doJSON(ctx, http.MethodGet, id, q, nil, []Scope{ScopeAdsRead}, &account); err != nil {
		return nil, err
	}
	return &account, nil
}

func (c *Client) SelectAdAccount(ctx context.Context, accountID string) (*AdAccount, error) {
	account, err := c.GetAdAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	c.selectionMu.Lock()
	c.selection.AdAccountID = normalizeAdAccountID(firstNonEmpty(account.AccountID, account.ID))
	c.selectionMu.Unlock()
	return account, nil
}

func (c *Client) SelectedAccounts() AccountSelection {
	c.selectionMu.RLock()
	defer c.selectionMu.RUnlock()
	return c.selection
}

func (c *Client) selectedInstagramAccount(explicit string) (string, error) {
	if explicit = strings.TrimSpace(explicit); explicit != "" {
		return explicit, nil
	}
	c.selectionMu.RLock()
	id := c.selection.InstagramAccountID
	c.selectionMu.RUnlock()
	if id == "" {
		return "", fmt.Errorf("%w: select an Instagram professional account", ErrInvalidInput)
	}
	return id, nil
}

func (c *Client) selectedAdAccount(explicit string) (string, error) {
	if explicit = normalizeAdAccountID(explicit); explicit != "" {
		return "act_" + explicit, nil
	}
	c.selectionMu.RLock()
	id := c.selection.AdAccountID
	c.selectionMu.RUnlock()
	if id == "" {
		return "", fmt.Errorf("%w: select an ad account", ErrInvalidInput)
	}
	return "act_" + normalizeAdAccountID(id), nil
}

func requireAdAccountID(id string) (string, error) {
	id = normalizeAdAccountID(id)
	if id == "" {
		return "", fmt.Errorf("%w: ad_account_id is required", ErrInvalidInput)
	}
	return "act_" + id, nil
}

func normalizeAdAccountID(id string) string {
	return strings.TrimPrefix(strings.TrimSpace(id), "act_")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
