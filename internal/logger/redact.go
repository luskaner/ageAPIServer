package logger

import (
	"net/http"
	"strings"
)

// Redacted is what replaces a credential value in a log line.
const Redacted = "<redacted>"

// sensitiveHeaders are the headers whose value is never logged.
var sensitiveHeaders = map[string]bool{
	"authorization":       true,
	"proxy-authorization": true,
	"cookie":              true,
	"set-cookie":          true,
	"x-api-key":           true,
}

// RedactHeader returns the value that may be logged for a header. Sensitive
// headers keep only their scheme, so a log line still shows that a Steam token
// was present without showing the token.
func RedactHeader(name string, value string) string {
	lower := strings.ToLower(name)
	if !sensitiveHeaders[lower] {
		return value
	}
	if lower == "cookie" || lower == "set-cookie" {
		return RedactCookie(value)
	}
	if scheme, _, found := strings.Cut(value, " "); found {
		return scheme + " " + Redacted
	}
	return Redacted
}

// RedactHeaders renders the headers of a request or response for a log line.
func RedactHeaders(header http.Header) map[string]string {
	redacted := make(map[string]string, len(header))
	for name, values := range header {
		redacted[name] = RedactHeader(name, strings.Join(values, ", "))
	}
	return redacted
}

// RedactAuthorization is RedactHeader specialised for the Steam token header.
func RedactAuthorization(value string) string {
	return RedactHeader("Authorization", value)
}

// RedactCookie is RedactHeader specialised for the session cookie header. It
// keeps the cookie names and drops every value.
func RedactCookie(value string) string {
	if value == "" {
		return ""
	}
	parts := strings.Split(value, ";")
	for i, part := range parts {
		name, _, found := strings.Cut(strings.TrimSpace(part), "=")
		if found {
			parts[i] = name + "=" + Redacted
		} else {
			parts[i] = Redacted
		}
	}
	return strings.Join(parts, "; ")
}
