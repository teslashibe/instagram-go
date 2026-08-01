package instagram

import (
	"fmt"
	"testing"
)

const largeInstagramID = "3925989427651196285"

func TestParseUserPreservesIdentifierLexemes(t *testing.T) {
	for _, field := range []string{"pk", "pk_id", "id", "user_id"} {
		for _, value := range []string{largeInstagramID, `"` + largeInstagramID + `"`} {
			name := field + "/" + value
			t.Run(name, func(t *testing.T) {
				raw := []byte(fmt.Sprintf(`{%q:%s,"username":"coffee"}`, field, value))
				user, err := parseUser(raw)
				if err != nil {
					t.Fatalf("parseUser: %v", err)
				}
				if user.ID != largeInstagramID {
					t.Fatalf("User.ID = %q, want %q", user.ID, largeInstagramID)
				}
			})
		}
	}
}

func TestParseUserIdentifierFallbacks(t *testing.T) {
	user, err := parseUser([]byte(`{
		"pk_id": 0,
		"pk": "",
		"id": null,
		"user_id": 3925989427651196285
	}`))
	if err != nil {
		t.Fatalf("parseUser: %v", err)
	}
	if user.ID != largeInstagramID {
		t.Fatalf("User.ID = %q, want fallback %q", user.ID, largeInstagramID)
	}

	if _, err := parseUser([]byte(`{"pk":0,"pk_id":null,"id":"","user_id":0}`)); err == nil {
		t.Fatal("parseUser accepted a payload with no usable identifier")
	}
}

func TestParsePostPreservesIdentifierLexemes(t *testing.T) {
	for _, field := range []string{"pk", "pk_id", "id"} {
		for _, value := range []string{largeInstagramID, `"` + largeInstagramID + `"`} {
			name := field + "/" + value
			t.Run(name, func(t *testing.T) {
				raw := []byte(fmt.Sprintf(`{%q:%s,"code":"COFFEE","media_type":1}`, field, value))
				post, err := parsePost(raw)
				if err != nil {
					t.Fatalf("parsePost: %v", err)
				}
				if post.PK != largeInstagramID {
					t.Fatalf("Post.PK = %q, want %q", post.PK, largeInstagramID)
				}
				if field == "id" && post.ID != largeInstagramID {
					t.Fatalf("Post.ID = %q, want %q", post.ID, largeInstagramID)
				}
			})
		}
	}
}

func TestParsePostPreservesCaptionUserIDLexemes(t *testing.T) {
	for _, value := range []string{largeInstagramID, `"` + largeInstagramID + `"`} {
		t.Run(value, func(t *testing.T) {
			raw := []byte(fmt.Sprintf(`{"pk":1,"caption":{"user_id":%s}}`, value))
			post, err := parsePost(raw)
			if err != nil {
				t.Fatalf("parsePost: %v", err)
			}
			if post.CaptionUserID != largeInstagramID {
				t.Fatalf("CaptionUserID = %q, want %q", post.CaptionUserID, largeInstagramID)
			}
		})
	}
}

func TestParsePostIdentifierPrecedenceAndCompositeFallback(t *testing.T) {
	post, err := parsePost([]byte(`{
		"pk_id": "77",
		"pk": 3925989427651196285,
		"id": "3925989427651196285_4635605442"
	}`))
	if err != nil {
		t.Fatalf("parsePost precedence: %v", err)
	}
	if post.PK != "77" || post.ID != "3925989427651196285_4635605442" {
		t.Fatalf("unexpected precedence result: %#v", post)
	}

	post, err = parsePost([]byte(`{"pk":0,"id":"3925989427651196285_4635605442"}`))
	if err != nil {
		t.Fatalf("parsePost fallback: %v", err)
	}
	if post.PK != largeInstagramID {
		t.Fatalf("Post.PK = %q, want composite fallback %q", post.PK, largeInstagramID)
	}
}
