package store

import (
	"strings"
	"testing"
)

func TestDeriveClusterKeyNormalizesDynamicValues(t *testing.T) {
	keyA := deriveClusterKey(
		"/checkout/orders/12345",
		"api_error",
		"GET:https://api.example.com/users/12345/orders/9f8f7f6e5d4c3b2a:5xx",
		"GET https://api.example.com/users/alice@example.com/orders/12345 -> 500",
	)
	keyB := deriveClusterKey(
		"/checkout/orders/99999",
		"api_error",
		"GET:https://api.example.com/users/99999/orders/0a1b2c3d4e5f6a7b:5xx",
		"GET https://api.example.com/users/bob@example.com/orders/99999 -> 502",
	)

	if keyA != keyB {
		t.Fatalf("expected keys to match after normalization, got %q vs %q", keyA, keyB)
	}
}

func TestDeriveClusterKeyDiffersByRoute(t *testing.T) {
	keyA := deriveClusterKey(
		"/checkout",
		"validation_failed",
		"invalid:tag:input|id:email",
		"Validation blocked submission: email",
	)
	keyB := deriveClusterKey(
		"/account/profile",
		"validation_failed",
		"invalid:tag:input|id:email",
		"Validation blocked submission: email",
	)

	if keyA == keyB {
		t.Fatalf("expected route-sensitive keys to differ, got %q", keyA)
	}
}

func TestDeriveClusterKeyFallsBackToKnownKind(t *testing.T) {
	key := deriveClusterKey("/checkout", "custom-kind", "", "")
	if !strings.HasPrefix(key, "ui_no_effect:") {
		t.Fatalf("expected unknown kinds to normalize to ui_no_effect, got %q", key)
	}
}
