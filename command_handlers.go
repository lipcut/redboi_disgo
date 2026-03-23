package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/json"

	"github.com/disgoorg/disgolink/v3/disgolink"
	"github.com/disgoorg/disgolink/v3/lavalink"
)

var (
	bassBoost = lavalink.Equalizer{
		0:  0.2,
		1:  0.15,
		2:  0.1,
		3:  0.05,
		4:  0.0,
		5:  -0.05,
		6:  -0.1,
		7:  -0.1,
		8:  -0.1,
		9:  -0.1,
		10: -0.1,
		11: -0.1,
		12: -0.1,
		13: -0.1,
		14: -0.1,
	}
)

func (b *Bot) shuffle(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) error {
	queue := b.Queues.Get(*event.GuildID())
	if queue == nil {
		return event.CreateMessage(discord.MessageCreate{
			Content: "No player found",
		})
	}

	queue.Shuffle()
	return event.CreateMessage(discord.MessageCreate{
		Content: "Queue shuffled",
	})
}

func (b *Bot) volume(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) error {
	player, ok := b.requirePlayer(*event.GuildID())
	if !ok {
		return event.CreateMessage(discord.MessageCreate{Content: "No player found"})
	}

	volume := data.Int("volume")
	if err := player.Update(context.TODO(), lavalink.WithVolume(volume)); err != nil {
		return event.CreateMessage(discord.MessageCreate{
			Content: fmt.Sprintf("Error while setting volume: `%s`", err),
		})
	}

	return event.CreateMessage(discord.MessageCreate{
		Content: fmt.Sprintf("Volume set to `%d`", volume),
	})
}

func (b *Bot) seek(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) error {
	player, ok := b.requirePlayer(*event.GuildID())
	if !ok {
		return event.CreateMessage(discord.MessageCreate{Content: "No player found"})
	}

	position := data.String("position")
	duration, err := time.ParseDuration(position)
	if err != nil {
		return event.CreateMessage(discord.MessageCreate{
			Content: fmt.Sprintf("Error when parse duration: `%s`", err),
		})
	}
	finalPosition := lavalink.Duration(duration.Milliseconds())
	if err := player.Update(context.TODO(), lavalink.WithPosition(finalPosition)); err != nil {
		return event.CreateMessage(discord.MessageCreate{
			Content: fmt.Sprintf("Error while seeking: `%s`", err),
		})
	}

	return event.CreateMessage(discord.MessageCreate{
		Content: fmt.Sprintf("Seeked to `%s`", formatPosition(finalPosition)),
	})
}

func (b *Bot) bassBoost(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) error {
	player, ok := b.requirePlayer(*event.GuildID())
	if !ok {
		return event.CreateMessage(discord.MessageCreate{Content: "No player found"})
	}

	enabled := data.Bool("enabled")
	filters := player.Filters()
	if enabled {
		filters.Equalizer = &bassBoost
	} else {
		filters.Equalizer = nil
	}

	if err := player.Update(context.TODO(), lavalink.WithFilters(filters)); err != nil {
		return event.CreateMessage(discord.MessageCreate{
			Content: fmt.Sprintf("Error while setting bass boost: `%s`", err),
		})
	}

	return event.CreateMessage(discord.MessageCreate{
		Content: fmt.Sprintf("Bass boost set to `%t`", enabled),
	})
}

func (b *Bot) skip(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) error {
	player, ok := b.requirePlayer(*event.GuildID())
	if !ok {
		return event.CreateMessage(discord.MessageCreate{Content: "No player found"})
	}

	queue := b.Queues.Get(*event.GuildID())
	track, err := queue.Skip()
	updateOption := lavalink.WithTrack(track)
	if err != nil {
		updateOption = lavalink.WithNullTrack()
	}

	if err := player.Update(context.TODO(), updateOption); err != nil {
		return event.CreateMessage(discord.MessageCreate{
			Content: fmt.Sprintf("Error while skipping track: `%s`", err),
		})
	}

	return event.CreateMessage(discord.MessageCreate{
		Content: "Skipped track",
	})
}

func (b *Bot) queueType(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) error {
	queue := b.Queues.Get(*event.GuildID())
	if queue == nil {
		return event.CreateMessage(discord.MessageCreate{
			Content: "No player found",
		})
	}

	queue.Type = QueueType(data.String("type"))
	return event.CreateMessage(discord.MessageCreate{
		Content: fmt.Sprintf("Queue type set to `%s`", queue.Type),
	})
}

func (b *Bot) clearQueue(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) error {
	queue := b.Queues.Get(*event.GuildID())
	if queue == nil {
		return event.CreateMessage(discord.MessageCreate{
			Content: "No player found",
		})
	}

	queue.Clear()
	return event.CreateMessage(discord.MessageCreate{
		Content: "Queue cleared",
	})
}

func (b *Bot) queue(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) error {
	queue := b.Queues.Get(*event.GuildID())
	if queue == nil {
		return event.CreateMessage(discord.MessageCreate{
			Content: "No player found",
		})
	}

	if len(queue.Tracks) == 0 {
		return event.CreateMessage(discord.MessageCreate{
			Content: "No tracks in queue",
		})
	}

	var tracks string
	for i, track := range queue.Tracks {
		tracks += fmt.Sprintf("%d. [`%s`](<%s>)\n", i+1, track.Info.Title, *track.Info.URI)
	}

	return event.CreateMessage(discord.MessageCreate{
		Content: fmt.Sprintf("Queue `%s`:\n%s", queue.Type, tracks),
	})
}

func (b *Bot) players(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) error {
	var description string
	b.Lavalink.ForPlayers(func(player disgolink.Player) {
		description += fmt.Sprintf("GuildID: `%s`\n", player.GuildID())
	})

	return event.CreateMessage(discord.MessageCreate{
		Content: fmt.Sprintf("Players:\n%s", description),
	})
}

func (b *Bot) togglePlay(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) error {
	player, ok := b.requirePlayer(*event.GuildID())
	if !ok {
		return event.CreateMessage(discord.MessageCreate{Content: "No player found"})
	}

	if err := player.Update(context.TODO(), lavalink.WithPaused(!player.Paused())); err != nil {
		return event.CreateMessage(discord.MessageCreate{
			Content: fmt.Sprintf("Error while pausing: `%s`", err),
		})
	}

	status := "playing"
	if player.Paused() {
		status = "paused"
	}
	return event.CreateMessage(discord.MessageCreate{
		Content: fmt.Sprintf("Player is now %s", status),
	})
}

func (b *Bot) stop(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) error {
	player, ok := b.requirePlayer(*event.GuildID())
	if !ok {
		return event.CreateMessage(discord.MessageCreate{Content: "No player found"})
	}

	if err := player.Update(context.TODO(), lavalink.WithNullTrack()); err != nil {
		return event.CreateMessage(discord.MessageCreate{
			Content: fmt.Sprintf("Error while stopping: `%s`", err),
		})
	}

	return event.CreateMessage(discord.MessageCreate{
		Content: "Player stopped",
	})
}

func (b *Bot) disconnect(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) error {
	_, ok := b.requirePlayer(*event.GuildID())
	if !ok {
		return event.CreateMessage(discord.MessageCreate{Content: "No player found"})
	}

	if err := b.Client.UpdateVoiceState(context.TODO(), *event.GuildID(), nil, false, false); err != nil {
		return event.CreateMessage(discord.MessageCreate{
			Content: fmt.Sprintf("Error while disconnecting: `%s`", err),
		})
	}

	return event.CreateMessage(discord.MessageCreate{
		Content: "Player disconnected",
	})
}

func (b *Bot) nowPlaying(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) error {
	player, ok := b.requirePlayer(*event.GuildID())
	if !ok {
		return event.CreateMessage(discord.MessageCreate{Content: "No player found"})
	}

	track := player.Track()
	if track == nil {
		return event.CreateMessage(discord.MessageCreate{
			Content: "No track found",
		})
	}

	return event.CreateMessage(discord.MessageCreate{
		Content: fmt.Sprintf("Now playing: [`%s`](<%s>)\n\n %s / %s", track.Info.Title, *track.Info.URI, formatPosition(player.Position()), formatPosition(track.Info.Length)),
	})
}

func formatPosition(position lavalink.Duration) string {
	if position == 0 {
		return "0:00"
	}
	return fmt.Sprintf("%d:%02d", position.Minutes(), position.SecondsPart())
}

func (b *Bot) voiceStateCheck(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) (*discord.VoiceState, error) {
	voiceState, ok := b.Client.Caches.VoiceState(*event.GuildID(), event.User().ID)
	if !ok {
		return nil, event.CreateMessage(discord.MessageCreate{
			Content: "You need to be in a voice channel to use this command",
		})
	}

	if err := event.DeferCreateMessage(false); err != nil {
		return nil, err
	}

	return &voiceState, nil
}

func (b *Bot) loadTrack(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) []lavalink.Track {
	identifier := data.String("identifier")
	if source, ok := data.OptString("source"); ok {
		identifier = lavalink.SearchType(source).Apply(identifier)
	} else {
		identifier = PrepareIdentifier(identifier)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result_tracks := []lavalink.Track{}
	b.Lavalink.BestNode().LoadTracksHandler(ctx, identifier, disgolink.NewResultHandler(
		func(track lavalink.Track) {
			_, _ = b.Client.Rest.UpdateInteractionResponse(event.ApplicationID(), event.Token(), discord.MessageUpdate{
				Content: json.Ptr(fmt.Sprintf("loaded track: [`%s`](<%s>)", track.Info.Title, *track.Info.URI)),
			})
			result_tracks = append(result_tracks, track)
		},
		func(playlist lavalink.Playlist) {
			_, _ = b.Client.Rest.UpdateInteractionResponse(event.ApplicationID(), event.Token(), discord.MessageUpdate{
				Content: json.Ptr(fmt.Sprintf("loaded playlist: `%s` with `%d` tracks", playlist.Info.Name, len(playlist.Tracks))),
			})
			result_tracks = append(result_tracks, playlist.Tracks...)
		},
		func(tracks []lavalink.Track) {
			_, _ = b.Client.Rest.UpdateInteractionResponse(event.ApplicationID(), event.Token(), discord.MessageUpdate{
				Content: json.Ptr(fmt.Sprintf("loaded search result: [`%s`](<%s>)", tracks[0].Info.Title, *tracks[0].Info.URI)),
			})
			result_tracks = append(result_tracks, tracks[0])
		},
		func() {
			_, _ = b.Client.Rest.UpdateInteractionResponse(event.ApplicationID(), event.Token(), discord.MessageUpdate{
				Content: json.Ptr(fmt.Sprintf("Nothing found for: `%s`", identifier)),
			})
		},
		func(err error) {
			_, _ = b.Client.Rest.UpdateInteractionResponse(event.ApplicationID(), event.Token(), discord.MessageUpdate{
				Content: json.Ptr(fmt.Sprintf("Error while looking up query: `%s`", err)),
			})
		},
	))

	return result_tracks
}

func (b *Bot) play(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) error {
	voiceState, err := b.voiceStateCheck(event, data)
	if err != nil {
		return err
	}
	tracks := b.loadTrack(event, data)
	if len(tracks) == 0 {
		return errors.New("no tracks found")
	}

	player := b.Lavalink.ExistingPlayer(*event.GuildID())
	if player == nil || player.ChannelID() != voiceState.ChannelID {
		if err := b.Client.UpdateVoiceState(context.TODO(), *event.GuildID(), voiceState.ChannelID, false, false); err != nil {
			return err
		}
	}

	b.Lavalink.Player(*event.GuildID()).Update(context.TODO(), lavalink.WithTrack(tracks[0]))

	return nil
}

func (b *Bot) enqueue(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) error {
	voiceState, err := b.voiceStateCheck(event, data)
	if err != nil {
		return err
	}
	tracks := b.loadTrack(event, data)
	if len(tracks) == 0 {
		return errors.New("no tracks found")
	}

	player := b.Lavalink.ExistingPlayer(*event.GuildID())
	if player == nil || player.ChannelID() != voiceState.ChannelID {
		err := b.Client.UpdateVoiceState(context.TODO(), *event.GuildID(), voiceState.ChannelID, false, false)
		if err != nil {
			return err
		}
	}

	queue := b.Queues.Get(*event.GuildID())
	if player.Track() != nil {
		queue.Append(tracks...)
	} else {
		b.Lavalink.Player(*event.GuildID()).Update(context.TODO(), lavalink.WithTrack(tracks[0]))
		if len(tracks[1:]) != 0 {
			queue.Append(tracks[1:]...)
		}
	}

	return nil
}

func (b *Bot) summon(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) error {
	voiceState, err := b.voiceStateCheck(event, data)
	if err != nil {
		return err
	}

	player := b.Lavalink.ExistingPlayer(*event.GuildID())
	if player == nil || player.ChannelID() != voiceState.ChannelID {
		if err := b.Client.UpdateVoiceState(context.TODO(), *event.GuildID(), voiceState.ChannelID, false, false); err != nil {
			return err
		}
	}

	_, err = b.Client.Rest.UpdateInteractionResponse(event.ApplicationID(), event.Token(), discord.MessageUpdate{
		Content: json.Ptr("I'm summoned!"),
	})
	return err
}
