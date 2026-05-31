package main

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/disgoorg/disgolink/v3/lavalink"
)

var (
	searchPattern = regexp.MustCompile(`^(.{2,4})search:(.+)`)
)

func IsSearchIdentifier(identifier string) bool {
	return searchPattern.MatchString(strings.TrimSpace(identifier))
}

func IsURLIdentifier(identifier string) bool {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return false
	}

	parsed, err := url.ParseRequestURI(identifier)
	if err != nil || parsed.Host == "" {
		return false
	}

	return strings.EqualFold(parsed.Scheme, "http") || strings.EqualFold(parsed.Scheme, "https")
}

func PrepareIdentifier(identifier string) string {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return ""
	}

	if !(IsURLIdentifier(identifier) || IsSearchIdentifier(identifier)) {
		return lavalink.SearchTypeYouTubeMusic.Apply(identifier)
	}
	return identifier
}
