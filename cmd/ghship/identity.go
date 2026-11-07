package main

import (
	"regexp"
	"strings"
)

// The legacy pattern remains valid for finding and removing existing files.
var legacyLoginPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]{0,38}$`)

func validStandardLogin(value string) bool {
	return legacyLoginPattern.MatchString(value) && !strings.HasSuffix(value, "-") && !strings.Contains(value, "--")
}

// GitHub managed users append an enterprise shortcode after an underscore.
// The special setup account similarly ends in _admin.
func validGitHubLogin(login string) bool {
	if validStandardLogin(login) {
		return true
	}
	if len(login) > 39 {
		return false
	}
	name, suffix, ok := strings.Cut(login, "_")
	if !ok || !validStandardLogin(name) || len(suffix) < 3 || len(suffix) > 8 {
		return false
	}
	for _, c := range suffix {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return false
		}
	}
	return true
}

func validStoredLogin(login string) bool {
	return legacyLoginPattern.MatchString(login) || validGitHubLogin(login)
}
