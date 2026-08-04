package mcp

// Excluded documents official client methods intentionally kept outside the
// agent tool inventory.
var Excluded = map[string]string{
	"AuthorizationURL": "OAuth state and browser redirects are host responsibilities",
	"ExchangeCode":     "OAuth codes must be handled by the host credential flow",
	"RefreshToken":     "token refresh is a host credential lifecycle operation",
	"SelectedAccounts": "selection state is returned by explicit select tools",
	"UpdateAdStatus":   "ad mutations are SDK-only until a separately reviewed write-tool policy is approved",
}
