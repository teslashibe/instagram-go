package instagram

// Persisted GraphQL operations are centralized here because Instagram rotates
// document IDs independently of SDK releases. Every value below is backed by
// the dated inventory in docs/inventory/search-graphql.md.
type graphqlOperation struct {
	FriendlyName string
	DocID        string
}

const (
	keywordSearchInitialFriendlyName    = "PolarisKeywordSearchExplorePageRelayQuery"
	keywordSearchInitialDocID           = "26586987494245638"
	keywordSearchPaginationFriendlyName = "PolarisKeywordSearchExplorePageRelayPaginationQuery"
	keywordSearchPaginationDocID        = "26577336451926911"
)

var (
	keywordSearchInitialOperation = graphqlOperation{
		FriendlyName: keywordSearchInitialFriendlyName,
		DocID:        keywordSearchInitialDocID,
	}
	keywordSearchPaginationOperation = graphqlOperation{
		FriendlyName: keywordSearchPaginationFriendlyName,
		DocID:        keywordSearchPaginationDocID,
	}
)
