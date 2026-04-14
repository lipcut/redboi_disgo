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

type TrackResultKind = int

const (
	TrackResultSingle TrackResultKind = iota
	TrackResultMultiple
	TrackResultPlaylist
)

type ResultTrack struct {
	Kind   TrackResultKind
	Tracks []lavalink.Track
}

func (b *Bogus) loadTracks(identifier string) (ResultTrack, error) {
	if !IsURLIdentifier(identifier) && !searchPattern.MatchString(identifier) {
		identifier = lavalink.SearchTypeYouTubeMusic.Apply(identifier)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var resultError error
	result := ResultTrack{
		Tracks: []lavalink.Track{},
	}
	b.Lavalink.BestNode().LoadTracksHandler(ctx, identifier, disgolink.NewResultHandler(
		func(track lavalink.Track) {
			result.Kind = TrackResultSingle
			result.Tracks = append(result.Tracks, track)
		},
		func(playlist lavalink.Playlist) {
			result.Kind = TrackResultPlaylist
			result.Tracks = slices.Concat(result.Tracks, playlist.Tracks)
		},
		func(tracks []lavalink.Track) {
			result.Kind = TrackResultMultiple
			result.Tracks = slices.Concat(result.Tracks, tracks)
		},
		func() {
			resultError = errors.New(fmt.Sprintf("Nothing found for: `%s`", identifier))
		},
		func(err error) {
			resultError = err
		},
	))

	return result, resultError
}

func (b *Bogus) nowPlaying(w http.ResponseWriter, r *http.Request) {
	player, ok := b.requirePlayer(b.currentGuildID)
	if !ok {
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
		artwork := `<div class="size-14 rounded-box bg-base-300 shrink-0"></div>`
		if track.Info.ArtworkURL != nil {
			artwork = fmt.Sprintf(`<img class="mask mask-squircle size-14 object-cover object-center shrink-0" src="%v"/>`, *track.Info.ArtworkURL)
		}

		err := sse.PatchElements(fmt.Sprintf(`
				<h2
				   class="text-lg card-title opacity-90 flex items-center gap-3"
				   id="nowPlayingSong"
				>
					%s
					<div class="min-w-0">
						<div class="truncate">%v</div>
						<div class="uppercase font-semibold opacity-60 truncate">%v</div>
					</div>
				</h2>
				`,
			artwork, track.Info.Author, track.Info.Title))
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
		    <div><img class="mask mask-squircle size-10 object-cover object-center" src="%v"/></div>
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
	player, ok := b.requirePlayer(b.currentGuildID)
	if !ok {
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
	player, ok := b.requirePlayer(b.currentGuildID)
	if !ok {
		return
	}

	store := &Store{}
	if err := datastar.ReadSignals(r, store); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tracks, err := b.loadTracks(store.Identifier)
	if err != nil {
		slog.Error("failed to enqueue", slog.Any("err", err))
		return
	}

	queue := b.Queues.Get(b.currentGuildID)

	switch tracks.Kind {
	case TrackResultPlaylist:
		playlist := tracks.Tracks
		if player.Track() != nil {
			queue.Append(playlist...)
		} else {
			player.Update(context.TODO(), lavalink.WithTrack(playlist[0]))
			if len(playlist[1:]) != 0 {
				queue.Append(playlist[1:]...)
			}
		}
	case TrackResultSingle, TrackResultMultiple:
		track := tracks.Tracks[0]
		if player.Track() != nil {
			queue.Append(track)
		} else {
			player.Update(context.TODO(), lavalink.WithTrack(track))
		}
	}

	b.nowPlaying(w, r)
	b.queue(w, r)
	b.publish()
}

func (b *Bogus) togglePlay(w http.ResponseWriter, r *http.Request) {
	player, ok := b.requirePlayer(b.currentGuildID)
	if !ok {
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
	player, ok := b.requirePlayer(b.currentGuildID)
	if !ok {
		return
	}

	track, err := b.Queues.Get(player.GuildID()).Next()
	updateOption := lavalink.WithNullTrack()
	if err == nil {
		updateOption = lavalink.WithTrack(track)
	}

	err = player.Update(context.TODO(), updateOption, lavalink.WithPaused(false))
	if err != nil {
		slog.Error("failed to skip the song", slog.Any("err", err))
		return
	}

	b.checkPaused(w, r)
	b.nowPlaying(w, r)
	b.queue(w, r)
	b.publish()
}

func (b *Bogus) stop(w http.ResponseWriter, r *http.Request) {
	player, ok := b.requirePlayer(b.currentGuildID)
	if !ok {
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

func (b *Bogus) search(w http.ResponseWriter, r *http.Request) {
	_, ok := b.requirePlayer(b.currentGuildID)
	if !ok {
		return
	}

	store := &Store{}
	if err := datastar.ReadSignals(r, store); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	identifier := store.Identifier
	tracks, err := b.loadTracks(identifier)
	if err != nil {
		slog.Error("failed to enqueue", slog.Any("err", err))
		return
	}

	if !IsURLIdentifier(identifier) && !searchPattern.MatchString(identifier) {
		switch tracks.Kind {
		case TrackResultPlaylist:
		case TrackResultSingle, TrackResultMultiple:
			sse := datastar.NewSSE(w, r)
			var resultHTML string
			for idx, track := range tracks.Tracks {
				if idx >= 8 {
					break
				}
				info := track.Info
				resultHTML += fmt.Sprintf(`<li
					class="list-row py-1"
					data-search-index="%d"
					data-identifier="%v"
					data-class:bg-neutral="$searchIndex === %d"
					data-on:click="$searchIndex = %d; $identifier = el.dataset.identifier; @post('/api/enqueue'); $identifier = ''; $searchIndex = -1"
				>
					<div><img class="mask mask-squircle size-6 object-cover object-center" src="%v"/></div>
					<div class="text-sm" data-class:text-neutral-content="$searchIndex === %d">
						<div>%v</div>
						<div class="text-xs uppercase font-semibold opacity-60 truncate">%v</div>
					</div>
				</li>`,
					idx, *track.Info.URI, idx, idx, *track.Info.ArtworkURL, idx, info.Author, info.Title)
			}
			sse.PatchElements(resultHTML, datastar.WithSelectorID("search-results"), datastar.WithModeInner())
			sse.MarshalAndPatchSignals(map[string]any{
				"searchResultCount":  min(len(tracks.Tracks), 8),
				"searchIndex":        0,
				"selectedIdentifier": *tracks.Tracks[0].Info.URI,
			})
		}

	}
}

func (b *Bogus) play(w http.ResponseWriter, r *http.Request) {
	player, ok := b.requirePlayer(b.currentGuildID)
	if !ok {
		return
	}

	store := &Store{}
	if err := datastar.ReadSignals(r, store); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	tracks, err := b.loadTracks(store.Identifier)
	if err != nil {
		slog.Error("failed to play", slog.Any("err", err))
		return
	}

	queue := b.Queues.Get(b.currentGuildID)

	switch tracks.Kind {
	case TrackResultPlaylist:
		playlist := tracks.Tracks
		player.Update(context.TODO(), lavalink.WithTrack(playlist[0]))
		queue.Prepend(playlist[1:]...)
	case TrackResultSingle, TrackResultMultiple:
		track := tracks.Tracks[0]
		player.Update(context.TODO(), lavalink.WithTrack(track))
	}

	b.nowPlaying(w, r)
	b.queue(w, r)
	b.publish()

}
