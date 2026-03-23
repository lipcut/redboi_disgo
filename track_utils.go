package main

import (
	"regexp"

	"github.com/disgoorg/disgolink/v3/lavalink"
)

var (
	urlPattern    = regexp.MustCompile("^https?://[-a-zA-Z0-9+&@#/%?=~_|!:,.;]*[-a-zA-Z0-9+&@#/%=~_|]?")
	searchPattern = regexp.MustCompile(`^(.{2})search:(.+)`)
)

func PrepareIdentifier(identifier string) string {
	if !urlPattern.MatchString(identifier) && !searchPattern.MatchString(identifier) {
		return lavalink.SearchTypeYouTubeMusic.Apply(identifier)
	}
	return identifier
}
