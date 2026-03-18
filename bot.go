package main

import (
	"context"
	"log/slog"

	"errors"
	"math/rand"
	"slices"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgolink/v3/disgolink"
	"github.com/disgoorg/disgolink/v3/lavalink"
	"github.com/disgoorg/snowflake/v2"
)

func init() {
	rand.New(rand.NewSource(time.Now().UnixNano()))
}

type QueueType string

const (
	QueueTypeNormal      QueueType = "normal"
	QueueTypeRepeatTrack QueueType = "repeat_track"
	QueueTypeRepeatQueue QueueType = "repeat_queue"
)

func (q QueueType) String() string {
	switch q {
	case QueueTypeNormal:
		return "Normal"
	case QueueTypeRepeatTrack:
		return "Repeat Track"
	case QueueTypeRepeatQueue:
		return "Repeat Queue"
	default:
		return "???"
	}
}

type Queue struct {
	Type   QueueType
	Tracks []lavalink.Track
}

func (q *Queue) Shuffle() {
	rand.Shuffle(len(q.Tracks), func(i, j int) {
		q.Tracks[i], q.Tracks[j] = q.Tracks[j], q.Tracks[i]
	})
}

func (q *Queue) Append(track ...lavalink.Track) {
	q.Tracks = append(q.Tracks, track...)
}

func (q *Queue) Next() (lavalink.Track, error) {
	if len(q.Tracks) == 0 {
		return lavalink.Track{}, errors.New("Queue is empty...")
	}

	next_track := q.Tracks[0]
	q.Tracks = q.Tracks[1:]
	return next_track, nil
}

func (q *Queue) Skip() (lavalink.Track, error) {
	if len(q.Tracks) == 0 {
		return lavalink.Track{}, errors.New("Queue is empty...")
	}

	result := q.Tracks[0]
	q.Tracks = q.Tracks[1:]
	return result, nil
}

func (q *Queue) Clear() {
	q.Tracks = make([]lavalink.Track, 0)
}

func (q *Queue) Remove(index int) error {
	if len(q.Tracks) == 0 {
		return errors.New("Queue is empty...")
	}
	if len(q.Tracks) <= index {
		return errors.New("index out of range")
	}
	new_tracks := slices.Concat(q.Tracks[:index], q.Tracks[index+1:])
	if new_tracks == nil {
		q.Clear()
	} else {
		q.Tracks = new_tracks
	}
	return nil
}

type Guild2Queue map[snowflake.ID]*Queue

func (q Guild2Queue) Get(guildID snowflake.ID) *Queue {
	queue, ok := q[guildID]
	if !ok {
		queue = &Queue{
			Tracks: make([]lavalink.Track, 0),
			Type:   QueueTypeNormal,
		}
		q[guildID] = queue
	}
	return queue
}

func (q Guild2Queue) Delete(guildID snowflake.ID) {
	delete(q, guildID)
}

type Bot struct {
	Client          bot.Client
	Lavalink        disgolink.Client
	CommandHandlers map[string]func(*events.ApplicationCommandInteractionCreate, discord.SlashCommandInteractionData) error
	Queues          Guild2Queue
}

func (b *Bot) onApplicationCommand(event *events.ApplicationCommandInteractionCreate) {
	interaction_data := event.SlashCommandInteractionData()

	handler, ok := b.CommandHandlers[interaction_data.CommandName()]
	if !ok {
		slog.Info("unknown command", slog.String("command", interaction_data.CommandName()))
		return
	}
	if err := handler(event, interaction_data); err != nil {
		slog.Error("error handling command", slog.Any("err", err))
	}
}

// onVoiceStateUpdate: forward request to Lavalink
func (b *Bot) onVoiceStateUpdate(event *events.GuildVoiceStateUpdate) {
	if event.VoiceState.UserID != b.Client.ApplicationID {
		return
	}
	b.Lavalink.OnVoiceStateUpdate(context.TODO(), event.VoiceState.GuildID, event.VoiceState.ChannelID, event.VoiceState.SessionID)
}

// onVoiceServerUpdate: forward request to Lavalink
func (b *Bot) onVoiceServerUpdate(event *events.VoiceServerUpdate) {
	if event.Endpoint == nil {
		return
	}
	b.Lavalink.OnVoiceServerUpdate(context.TODO(), event.GuildID, event.Token, *event.Endpoint)
}
