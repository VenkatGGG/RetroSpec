package api

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
)

var (
	eventEmailRegex      = regexp.MustCompile(`(?i)\b[\w.+-]+@[\w.-]+\.[a-z]{2,}\b`)
	eventUUIDRegex       = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}\b`)
	eventBearerRegex     = regexp.MustCompile(`(?i)\bbearer\s+[a-z0-9._~+/=-]{8,}\b`)
	eventHexTokenRegex   = regexp.MustCompile(`(?i)\b[0-9a-f]{24,}\b`)
	eventLongNumberRegex = regexp.MustCompile(`\b\d{12,19}\b`)
	eventCardRegex       = regexp.MustCompile(`\b(?:\d[ -]*?){13,16}\b`)
)

func sanitizeEventPayload(raw json.RawMessage) (json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()

	var payload any
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}

	sanitized := sanitizeEventValue(payload, "")
	encoded, err := json.Marshal(sanitized)
	if err != nil {
		return nil, err
	}
	return encoded, nil
}

func sanitizeEventValue(value any, key string) any {
	switch typed := value.(type) {
	case map[string]any:
		sanitized := make(map[string]any, len(typed))
		for childKey, childValue := range typed {
			sanitized[childKey] = sanitizeEventValue(childValue, childKey)
		}
		return sanitized
	case []any:
		sanitized := make([]any, 0, len(typed))
		for _, childValue := range typed {
			sanitized = append(sanitized, sanitizeEventValue(childValue, key))
		}
		return sanitized
	case string:
		return sanitizeEventString(typed, key)
	default:
		return value
	}
}

func sanitizeEventString(value string, key string) string {
	normalizedKey := strings.ToLower(strings.TrimSpace(key))
	if isSensitiveEventKey(normalizedKey) {
		return "<redacted>"
	}

	redacted := value
	redacted = eventEmailRegex.ReplaceAllString(redacted, "<email>")
	redacted = eventUUIDRegex.ReplaceAllString(redacted, "<uuid>")
	redacted = eventBearerRegex.ReplaceAllString(redacted, "<token>")
	redacted = eventHexTokenRegex.ReplaceAllString(redacted, "<token>")
	redacted = eventCardRegex.ReplaceAllString(redacted, "<card-number>")
	redacted = eventLongNumberRegex.ReplaceAllString(redacted, "<long-number>")
	if len(redacted) > 6_000 {
		return redacted[:6_000]
	}
	return redacted
}

func isSensitiveEventKey(key string) bool {
	if key == "" {
		return false
	}
	if strings.Contains(key, "password") ||
		strings.Contains(key, "passwd") ||
		strings.Contains(key, "secret") ||
		strings.Contains(key, "token") ||
		strings.Contains(key, "authorization") ||
		strings.Contains(key, "cookie") ||
		strings.Contains(key, "set-cookie") ||
		strings.Contains(key, "api_key") ||
		strings.Contains(key, "apikey") ||
		strings.Contains(key, "session") {
		return true
	}
	return strings.Contains(key, "email")
}
