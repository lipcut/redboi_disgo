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

	mux := http.NewServeMux()
	mux.HandleFunc("/", homepage)
	mux.HandleFunc("/api/now-playing", bogus.nowPlaying)
	mux.HandleFunc("/api/queue", bogus.queue)
	mux.HandleFunc("/api/check-paused", bogus.checkPaused)
	mux.HandleFunc("/api/enqueue", bogus.enqueue)
	mux.HandleFunc("/api/toggle-play", bogus.togglePlay)
	mux.HandleFunc("/api/skip", bogus.skip)
	mux.HandleFunc("/api/stop", bogus.stop)
	mux.HandleFunc("/api/clear", bogus.clear)
	mux.HandleFunc("/api/remove-track/{id}", bogus.removeTrack)
	mux.HandleFunc("/api/sync", bogus.sync)
	WsHubSetup(mux)

	slog.Info(fmt.Sprintf(
		"Open your browser to: http://%s/",
		serverAddress,
	))
	log.Fatal(http.ListenAndServe(serverAddress, mux))
}
