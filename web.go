package main

import (
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/disgoorg/snowflake/v2"
	"github.com/starfederation/datastar-go/datastar"
)

type Store struct {
	Identifier string `json:"identifier"`
}

const (
	serverAddress = "localhost:8080"
)

func server(robot *Bot, guildID snowflake.ID) {
	var bogus = Bogus(*robot)

	nowPlaying := func(sse *datastar.ServerSentEventGenerator, w http.ResponseWriter, r *http.Request) {
		player := bogus.Lavalink.ExistingPlayer(guildID)
		if player == nil {
			slog.Error("No player found")
			return
		}
		track := player.Track()
		if track == nil {
			err := sse.PatchElements(`
				<h2
                    class="text-lg card-title opacity-90"
                    id="nowPlayingSong"
                >Nothing Playing...</h2>
				`)
			if err != nil {
				slog.Error("fail to patch nowPlaying State", slog.Any("err", err))
				return
			}
		} else {
			err := sse.PatchElements(fmt.Sprintf(`
				<h2
                    class="text-lg card-title opacity-90"
                    id="nowPlayingSong"
                >
					<div class="text-nowrap">%v</div>
					<div class="uppercase font-semibold opacity-60 truncate">%v</div>
				</h2>
				`,
				track.Info.Author, track.Info.Title))
			if err != nil {
				slog.Error("fail to patch nowPlaying State", slog.Any("err", err))
				return
			}
		}
	}

	queue := func(sse *datastar.ServerSentEventGenerator, w http.ResponseWriter, r *http.Request) {
		tracks, err := bogus.queue(guildID)
		if err != nil {
			slog.Error("fail to aquire queue", slog.Any("err", err))
			return
		}
		var elements string
		for idx, track := range tracks {
			trackID := idx + 1
			element := fmt.Sprintf(`
				<li class="list-row">
				    <div><img class="mask mask-squircle size-10" src="%v"/></div>
				    <div>
						<div>%v</div>
						<div class="text-xs uppercase font-semibold opacity-60">%v</div>
					</div>
					<button class="btn btn-ghost btn-error" data-on:click="@delete('/api/remove-track/%d')">
						Remove
					</button>
				</li>
				`, *track.Info.ArtworkURL, track.Info.Author, track.Info.Title, trackID)
			elements += element
		}

		err = sse.PatchElements(
			elements,
			datastar.WithModeInner(),
			datastar.WithSelectorID("queue"),
		)
		if err != nil {
			slog.Error("fail to patch queue status", slog.Any("err", err))
			return
		}
	}

	checkPaused := func(sse *datastar.ServerSentEventGenerator, w http.ResponseWriter, r *http.Request) {
		isPaused, err := bogus.checkPaused(guildID)
		if err != nil {
			slog.Error("failed to check whether song is paused", slog.Any("err", err))
			return
		}
		message := "Pause"
		if isPaused {
			message = "Play"
		}
		err = sse.PatchElementf(
			`<button
				class="btn btn-outline join-item"
				data-on:click="@post('/api/toggle-play')"
				id="play-pause-btn"
			>%v</button>`, message)
		if err != nil {
			slog.Error("fail to patch pause State", slog.Any("err", err))
			return
		}
	}

	sync := func(w http.ResponseWriter, r *http.Request) {
		player := bogus.Lavalink.ExistingPlayer(guildID)
		if player == nil {
			return
		}

		sse := datastar.NewSSE(w, r)

		checkPaused(sse, w, r)
		nowPlaying(sse, w, r)
		queue(sse, w, r)
	}

	enqueue := func(w http.ResponseWriter, r *http.Request) {
		store := &Store{}
		if err := datastar.ReadSignals(r, store); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		err := bogus.enqueue(store.Identifier, guildID)
		if err != nil {
			slog.Error("failed to enqueue", slog.Any("err", err))
		}

		sse := datastar.NewSSE(w, r)
		nowPlaying(sse, w, r)
		queue(sse, w, r)
	}

	togglePlay := func(w http.ResponseWriter, r *http.Request) {
		err := bogus.togglePlay(guildID)
		if err != nil {
			slog.Error("failed to pause/play", slog.Any("err", err))
			return
		}

		sse := datastar.NewSSE(w, r)
		checkPaused(sse, w, r)
	}

	skip := func(w http.ResponseWriter, r *http.Request) {
		err := bogus.skip(guildID)
		if err != nil {
			slog.Error("failed to skip the song", slog.Any("err", err))
			return
		}

		sse := datastar.NewSSE(w, r)
		nowPlaying(sse, w, r)
		queue(sse, w, r)
	}

	stop := func(w http.ResponseWriter, r *http.Request) {
		err := bogus.stop(guildID)
		if err != nil {
			slog.Error("failed to stop the song", slog.Any("err", err))
		}

		sse := datastar.NewSSE(w, r)
		nowPlaying(sse, w, r)
		queue(sse, w, r)
	}

	removeTrack := func(w http.ResponseWriter, r *http.Request) {
		track_id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			slog.Error("failed to parse id for remove track", slog.Any("err", err))
			return
		}

		err = bogus.removeTrack(guildID, track_id-1)
		if err != nil {
			slog.Error("failed to remove track", slog.Any("err", err))
			return
		}

		sse := datastar.NewSSE(w, r)
		queue(sse, w, r)
	}

	clear := func(w http.ResponseWriter, r *http.Request) {
		bogus.clear(guildID)

		sse := datastar.NewSSE(w, r)
		queue(sse, w, r)
	}

	homepage := func(w http.ResponseWriter, r *http.Request) {
		if bogus.Lavalink.ExistingPlayer(guildID) == nil {
			http.ServeFile(w, r, "no_player.html")
		} else {
			http.ServeFile(w, r, "index.html")
		}
	}
	http.HandleFunc("/", homepage)
	http.HandleFunc("/api/now-playing", func(w http.ResponseWriter, r *http.Request) {
		sse := datastar.NewSSE(w, r)
		nowPlaying(sse, w, r)
	})
	http.HandleFunc("/api/queue", func(w http.ResponseWriter, r *http.Request) {
		sse := datastar.NewSSE(w, r)
		queue(sse, w, r)
	})
	http.HandleFunc("/api/check-paused", func(w http.ResponseWriter, r *http.Request) {
		sse := datastar.NewSSE(w, r)
		checkPaused(sse, w, r)
	})
	http.HandleFunc("/api/enqueue", enqueue)
	http.HandleFunc("/api/toggle-play", togglePlay)
	http.HandleFunc("/api/skip", skip)
	http.HandleFunc("/api/stop", stop)
	http.HandleFunc("/api/clear", clear)
	http.HandleFunc("/api/remove-track/{id}", removeTrack)
	http.HandleFunc("/api/sync", sync)

	slog.Info(fmt.Sprintf(
		"Open your browser to: http://%s/",
		serverAddress,
	))
	log.Fatal(http.ListenAndServe(serverAddress, nil))
}
