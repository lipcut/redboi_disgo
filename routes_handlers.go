package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/disgoorg/disgolink/v3/disgolink"
	"github.com/disgoorg/disgolink/v3/lavalink"
	"github.com/disgoorg/snowflake/v2"
	"github.com/lipcut/redboi_disgo/templ_componets"
	"github.com/starfederation/datastar-go/datastar"
)

// Proxy the bot to hijack the discord
type Bogus struct {
	*Bot
	currentGuildID snowflake.ID
}

type Store struct {
	Identifier string `json:"identifier"`
	TrackCount int    `json:"trackCount"`
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
			resultError = fmt.Errorf("Nothing found for: `%s`", identifier)
		},
		func(err error) {
			resultError = err
		},
	))

	return result, resultError
}

func (b *Bogus) homepage(w http.ResponseWriter, r *http.Request) {
	if b.Lavalink.ExistingPlayer(b.currentGuildID) == nil {
		http.ServeFile(w, r, "no_player.html")
	} else {
		http.ServeFile(w, r, "index.html")
	}
}

func (b *Bogus) nowPlaying(w http.ResponseWriter, r *http.Request) {
	player, ok := b.requirePlayer(b.currentGuildID)
	if !ok {
		slog.Error("cannot find player in %v:%v")
		return
	}
	track := player.Track()
	sse := datastar.NewSSE(w, r)

	playStatus := templ_componets.PlayingStatus(track)
	err := sse.PatchElementTempl(playStatus)
	if err != nil {
		slog.Error("fail to patch nowPlaying State", slog.Any("err", err))
	}
}

func (b *Bogus) queue(w http.ResponseWriter, r *http.Request) {
	queue := b.Queues.Get(b.currentGuildID)
	queueStatus := templ_componets.QueueStatus(queue.Tracks)
	sse := datastar.NewSSE(w, r)
	err := sse.PatchElementTempl(
		queueStatus,
		datastar.WithModeInner(),
		datastar.WithSelectorID("queue"),
	)
	if err != nil {
		slog.Error("fail to patch queue status", slog.Any("err", err))
		return
	}

	sse = datastar.NewSSE(w, r)
	err = sse.PatchSignals(fmt.Appendf([]byte{}, `{trackCount: %d}`, len(queue.Tracks)))

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
	playPause := templ_componets.PlayPause(player.Paused())
	sse := datastar.NewSSE(w, r)
	err := sse.PatchElementTempl(playPause)
	if err != nil {
		slog.Error("fail to patch pause State", slog.Any("err", err))
	}
}

func (b *Bogus) sync(w http.ResponseWriter, r *http.Request) {
	b.checkPaused(w, r)
	b.nowPlaying(w, r)
	b.history(w, r)
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
			searchResults := templ_componets.SearchResults(tracks.Tracks)
			sse.PatchElementTempl(searchResults, datastar.WithSelectorID("search-results"), datastar.WithModeInner())
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

func (b *Bogus) history(w http.ResponseWriter, r *http.Request) {
	store := &Store{}
	if err := datastar.ReadSignals(r, store); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	history := b.HistoryTracks.Get(b.currentGuildID)

	// resultHTML := strings.Builder{}
	// for _, track := range history.Iter() {
	// 	fmt.Fprintf(&resultHTML, `
	// 		<li class="list-row">
	// 	    <div><img class="mask mask-squircle size-10 object-cover object-center" src="%v"/></div>
	// 	    <div>
	// 			<div>%v</div>
	// 			<div class="text-xs uppercase font-semibold opacity-60">%v</div>
	// 		</div>
	// 		<button
	// 	 		class="btn btn-ghost btn-warning"
	// 			data-identifier="%v"
	// 			data-on:click="$identifier = el.dataset.identifier; @post('/api/enqueue'); $identifier = ''"
	// 		>
	// 			Enqueue
	// 		</button>
	// 		</li>
	// 		`, *track.Info.ArtworkURL, track.Info.Author, track.Info.Title, *track.Info.URI)
	// }

	historyStatus := templ_componets.HistoryStatus(history.Iter())
	sse := datastar.NewSSE(w, r)
	err := sse.PatchElementTempl(
		historyStatus,
		datastar.WithModeInner(),
		datastar.WithSelectorID("history"),
	)

	if err != nil {
		slog.Error("fail to patch queue status", slog.Any("err", err))
		return
	}
}
