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

func server(robot *Bot, guildID snowflake.ID) {
	bogus := Bogus{
		Bot:            robot,
		currentGuildID: guildID,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", bogus.homepage)
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
	mux.HandleFunc("/api/search", bogus.search)
	mux.HandleFunc("/api/play", bogus.play)
	mux.HandleFunc("/api/history", bogus.history)
	WsHubSetup(mux)

	slog.Info(fmt.Sprintf(
		"Open your browser to: http://%v/",
		serverAddress,
	))

	log.Fatal(http.ListenAndServe(serverAddress, mux))
}
