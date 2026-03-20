package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/disgoorg/disgolink/v3/disgolink"
	"github.com/disgoorg/disgolink/v3/lavalink"
	"github.com/disgoorg/snowflake/v2"
	"github.com/starfederation/datastar-go/datastar"
)

// Proxy the bot to hijack the discord
type Bogus struct {
	*Bot
	currentGuildID snowflake.ID
}

type Store struct {
	Identifier string `json:"identifier"`
}

func (b *Bogus) loadTrack(identifier string) ([]lavalink.Track, error) {
	if !urlPattern.MatchString(identifier) && !searchPattern.MatchString(identifier) {
		identifier = lavalink.SearchTypeYouTubeMusic.Apply(identifier)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var result_error error
	result_tracks := []lavalink.Track{}
	b.Lavalink.BestNode().LoadTracksHandler(ctx, identifier, disgolink.NewResultHandler(
		func(track lavalink.Track) {
			result_tracks = append(result_tracks, track)
		},
		func(playlist lavalink.Playlist) {
			result_tracks = slices.Concat(result_tracks, playlist.Tracks)
		},
		func(tracks []lavalink.Track) {
			result_tracks = append(result_tracks, tracks[0])
		},
		func() {
			result_error = errors.New(fmt.Sprintf("Nothing found for: `%s`", identifier))
		},
		func(err error) {
			result_error = err
		},
	))

	return result_tracks, result_error
}

func (b *Bogus) nowPlaying(w http.ResponseWriter, r *http.Request) {
	player := b.Lavalink.ExistingPlayer(b.currentGuildID)
	if player == nil {
		slog.Error("No player found")
		return
	}
	track := player.Track()
	sse := datastar.NewSSE(w, r)
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

func (b *Bogus) queue(w http.ResponseWriter, r *http.Request) {
	queue := b.Queues.Get(b.currentGuildID)
	var elements string
	for idx, track := range queue.Tracks {
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

	sse := datastar.NewSSE(w, r)
	err := sse.PatchElements(
		elements,
		datastar.WithModeInner(),
		datastar.WithSelectorID("queue"),
	)
	if err != nil {
		slog.Error("fail to patch queue status", slog.Any("err", err))
		return
	}
}

func (b *Bogus) checkPaused(w http.ResponseWriter, r *http.Request) {
	player := b.Lavalink.ExistingPlayer(b.currentGuildID)
	if player == nil {
		slog.Error("No player found")
		return
	}
	message := "Pause"
	if player.Paused() {
		message = "Play"
	}
	sse := datastar.NewSSE(w, r)
	err := sse.PatchElementf(
		`<button
				class="btn btn-outline join-item"
				data-on:click="@get('/api/toggle-play')"
				id="play-pause-btn"
			>%v</button>`, message)
	if err != nil {
		slog.Error("fail to patch pause State", slog.Any("err", err))
		return
	}
}

func (b *Bogus) sync(w http.ResponseWriter, r *http.Request) {
	b.checkPaused(w, r)
	b.nowPlaying(w, r)
	b.queue(w, r)
}

func (b *Bogus) enqueue(w http.ResponseWriter, r *http.Request) {
	player := b.Lavalink.ExistingPlayer(b.currentGuildID)
	if player == nil {
		slog.Error("No player found")
		return
	}

	store := &Store{}
	if err := datastar.ReadSignals(r, store); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tracks, err := b.loadTrack(store.Identifier)
	if err != nil {
		slog.Error("failed to enqueue", slog.Any("err", err))
		return
	}

	queue := b.Queues.Get(b.currentGuildID)
	if player.Track() != nil {
		queue.Append(tracks...)
	} else {
		player.Update(context.TODO(), lavalink.WithTrack(tracks[0]))
		if len(tracks[1:]) != 0 {
			queue.Append(tracks[1:]...)
		}
	}

	b.nowPlaying(w, r)
	b.queue(w, r)
	b.publish()
}

func (b *Bogus) togglePlay(w http.ResponseWriter, r *http.Request) {
	player := b.Lavalink.ExistingPlayer(b.currentGuildID)
	if player == nil {
		slog.Error("No player found")
		return
	}

	err := player.Update(context.TODO(), lavalink.WithPaused(!player.Paused()))
	if err != nil {
		slog.Error("failed to pause/play", slog.Any("err", err))
		return
	}

	b.checkPaused(w, r)
	b.publish()
}

func (b *Bogus) skip(w http.ResponseWriter, r *http.Request) {
	player := b.Lavalink.ExistingPlayer(b.currentGuildID)
	if player == nil {
		slog.Error("No player found")
		return
	}

	track, err := b.Queues.Get(player.GuildID()).Next()
	updateOption := lavalink.WithNullTrack()
	if err == nil {
		updateOption = lavalink.WithTrack(track)
	}

	err = player.Update(context.TODO(), updateOption)
	if err != nil {
		slog.Error("failed to skip the song", slog.Any("err", err))
		return
	}

	b.nowPlaying(w, r)
	b.queue(w, r)
	b.publish()
}

func (b *Bogus) stop(w http.ResponseWriter, r *http.Request) {
	player := b.Lavalink.ExistingPlayer(b.currentGuildID)
	if player == nil {
		slog.Error("No player found")
		return
	}

	err := player.Update(context.TODO(), lavalink.WithNullTrack())
	if err != nil {
		slog.Error("failed to stop the song", slog.Any("err", err))
		return
	}

	b.nowPlaying(w, r)
	b.queue(w, r)
	b.publish()
}

func (b *Bogus) removeTrack(w http.ResponseWriter, r *http.Request) {
	track_id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		slog.Error("failed to parse id for remove track", slog.Any("err", err))
		return
	}

	err = b.Queues.Get(b.currentGuildID).Remove(int(track_id - 1))
	if err != nil {
		slog.Error("failed to remove track", slog.Any("err", err))
		return
	}

	b.queue(w, r)
	b.publish()
}

func (b *Bogus) clear(w http.ResponseWriter, r *http.Request) {
	b.Queues.Get(b.currentGuildID).Clear()
	b.queue(w, r)
	b.publish()
}
