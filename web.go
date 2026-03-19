package main

import (
	"fmt"
	"log"
	"log/slog"
	"net/http"

	"github.com/disgoorg/snowflake/v2"
)

const (
	serverAddress = "localhost:8080"
)

func server(robot Bot, guildID snowflake.ID) {
	bogus := Bogus{
		Bot:            robot,
		currentGuildID: guildID,
	}

	homepage := func(w http.ResponseWriter, r *http.Request) {
		if bogus.Lavalink.ExistingPlayer(guildID) == nil {
			http.ServeFile(w, r, "no_player.html")
		} else {
			http.ServeFile(w, r, "index.html")
		}
	}
	http.HandleFunc("/", homepage)
	http.HandleFunc("/api/now-playing", bogus.nowPlaying)
	http.HandleFunc("/api/queue", bogus.queue)
	http.HandleFunc("/api/check-paused", bogus.checkPaused)
	http.HandleFunc("/api/enqueue", bogus.enqueue)
	http.HandleFunc("/api/toggle-play", bogus.togglePlay)
	http.HandleFunc("/api/skip", bogus.skip)
	http.HandleFunc("/api/stop", bogus.stop)
	http.HandleFunc("/api/clear", bogus.clear)
	http.HandleFunc("/api/remove-track/{id}", bogus.removeTrack)
	http.HandleFunc("/api/sync", bogus.sync)

	slog.Info(fmt.Sprintf(
		"Open your browser to: http://%s/",
		serverAddress,
	))
	log.Fatal(http.ListenAndServe(serverAddress, nil))
}
