package main

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/disgoorg/disgolink/v3/disgolink"
	"github.com/disgoorg/disgolink/v3/lavalink"
	"github.com/disgoorg/snowflake/v2"
)

// Proxy the bot to hijack the discord
type Bogus Bot

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

func (b *Bogus) play(identifier string, guildID snowflake.ID) (*lavalink.Track, error) {
	player := b.Lavalink.ExistingPlayer(guildID)
	if player == nil {
		return nil, errors.New("No player found")
	}

	tracks, err := b.loadTrack(identifier)
	if err != nil {
		return nil, err
	}

	b.Lavalink.Player(guildID).Update(context.TODO(), lavalink.WithTrack(tracks[0]))

	return &tracks[0], err
}

func (b *Bogus) enqueue(identifier string, guildID snowflake.ID) error {
	player := b.Lavalink.ExistingPlayer(guildID)
	if player == nil {
		return errors.New("No player found")
	}

	tracks, err := b.loadTrack(identifier)
	if err != nil {
		return err
	}

	if player.Track() != nil {
		b.Queues.Get(guildID).Append(tracks...)
	} else {
		b.Lavalink.Player(guildID).Update(context.TODO(), lavalink.WithTrack(tracks[0]))
		if len(tracks[1:]) != 0 {
			b.Queues.Get(guildID).Append(tracks[1:]...)
		}
	}

	return err
}

func (b *Bogus) togglePlay(guildID snowflake.ID) error {
	player := b.Lavalink.ExistingPlayer(guildID)
	if player == nil {
		return errors.New("No player found")
	}

	err := player.Update(context.TODO(), lavalink.WithPaused(!player.Paused()))
	if err != nil {
		return err
	}

	return nil
}

func (b *Bogus) queue(guildID snowflake.ID) ([]lavalink.Track, error) {
	queue := b.Queues.Get(guildID)
	if queue == nil {
		return nil, errors.New("No player found")
	}
	return queue.Tracks, nil
}

func (b *Bogus) skip(guildID snowflake.ID) error {
	player := b.Lavalink.ExistingPlayer(guildID)
	queue := b.Queues.Get(guildID)
	if player == nil || queue == nil {
		return errors.New("No player found")
	}

	track, err := queue.Skip()
	if err != nil {
		if err := player.Update(context.TODO(), lavalink.WithNullTrack()); err != nil {
			return err
		}
	} else {
		if err := player.Update(context.TODO(), lavalink.WithTrack(track)); err != nil {
			return err
		}
	}

	return nil
}

func (b *Bogus) stop(guildID snowflake.ID) error {
	player := b.Lavalink.ExistingPlayer(guildID)
	if player == nil {
		return errors.New("No player found")
	}

	if err := player.Update(context.TODO(), lavalink.WithNullTrack()); err != nil {
		return err
	}

	return nil
}

func (b *Bogus) nowPlaying(guildID snowflake.ID) (*lavalink.Track, error) {
	player := b.Lavalink.ExistingPlayer(guildID)
	if player == nil {
		return nil, errors.New("No player found")
	}

	track := player.Track()
	if track == nil {
		return nil, errors.New("No track playing")
	}

	return track, nil
}

func (b *Bogus) removeTrack(guildID snowflake.ID, index int64) error {
	return b.Queues.Get(guildID).Remove(int(index))
}

func (b *Bogus) checkPaused(guildID snowflake.ID) (bool, error) {
	player := b.Lavalink.ExistingPlayer(guildID)
	if player == nil {
		return false, errors.New("No player found")
	}
	return player.Paused(), nil
}

func (b *Bogus) clear(guildID snowflake.ID) {
	b.Queues.Get(guildID).Clear()
}
