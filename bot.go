package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgo/voice"
	"github.com/disgoorg/disgolink/v3/disgolink"
	"github.com/disgoorg/disgolink/v3/lavalink"
	"github.com/disgoorg/godave"
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

	nextTrack := q.Tracks[0]
	q.Tracks = q.Tracks[1:]
	return nextTrack, nil
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
	newTracks := slices.Concat(q.Tracks[:index], q.Tracks[index+1:])
	if newTracks == nil {
		q.Clear()
	} else {
		q.Tracks = newTracks
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
	PublishClient   *http.Client // for publishing updates to the websocket
}

func (b *Bot) onApplicationCommand(event *events.ApplicationCommandInteractionCreate) {
	interactionData := event.SlashCommandInteractionData()

	handler, ok := b.CommandHandlers[interactionData.CommandName()]
	if !ok {
		slog.Info("unknown command", slog.String("command", interactionData.CommandName()))
		return
	}
	if err := handler(event, interactionData); err != nil {
		slog.Error("error handling command", slog.Any("err", err))
	}

	if b.PublishClient != nil {
		res, err := b.PublishClient.Post("http://localhost:8080/api/publish", "text/plain", strings.NewReader("update!"))
		if err != nil {
			slog.Error("error posting update", slog.Any("err", err))
		} else {
			slog.Info("update posted", slog.Any("res", res))
		}
		res.Body.Close()
	}
}

// onVoiceStateUpdate: forward request to Lavalink
func (b *Bot) onVoiceStateUpdate(event *events.GuildVoiceStateUpdate) {
	if event.VoiceState.UserID != b.Client.ApplicationID {
		for voiceState := range event.Client().Caches.VoiceStates(event.VoiceState.GuildID) {
			if voiceState.SessionID != event.VoiceState.SessionID {
				continue
			}
			if voiceState.UserID != b.Client.ApplicationID {
				slog.Info("found user in voice", slog.String("guild", voiceState.UserID.String()))
				return
			}
		}
		b.Client.UpdateVoiceState(context.TODO(), event.VoiceState.GuildID, nil, false, false)
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

func discordBot(token string) (Bot, error) {
	robot := Bot{
		Queues:        make(map[snowflake.ID]*Queue),
		PublishClient: &http.Client{},
	}

	client, err := disgo.New(token,
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(gateway.IntentsGuild),
		),
		bot.WithCacheConfigOpts(
			cache.WithCaches(cache.FlagVoiceStates),
		),
		bot.WithEventListenerFunc(robot.onApplicationCommand),
		bot.WithEventListenerFunc(robot.onVoiceStateUpdate),
		bot.WithEventListenerFunc(robot.onVoiceServerUpdate),
		bot.WithVoiceManagerConfigOpts(
			voice.WithDaveSessionCreateFunc(godave.NewNoopSession),
		),
	)
	if err != nil {
		return Bot{}, fmt.Errorf("error while building disgo client: %w", err)
	}

	robot.Client = *client

	registerCommands(client)

	robot.Lavalink = disgolink.New(client.ApplicationID,
		disgolink.WithListenerFunc(robot.onPlayerPause),
		disgolink.WithListenerFunc(robot.onPlayerResume),
		disgolink.WithListenerFunc(robot.onTrackStart),
		disgolink.WithListenerFunc(robot.onTrackEnd),
		disgolink.WithListenerFunc(robot.onTrackException),
		disgolink.WithListenerFunc(robot.onTrackStuck),
		disgolink.WithListenerFunc(robot.onWebSocketClosed),
		disgolink.WithListenerFunc(robot.onUnknownEvent),
	)

	robot.CommandHandlers = map[string]func(event *events.ApplicationCommandInteractionCreate, data discord.SlashCommandInteractionData) error{
		"play":        robot.play,
		"enqueue":     robot.enqueue,
		"pause":       robot.pause,
		"now-playing": robot.nowPlaying,
		"stop":        robot.stop,
		"disconnect":  robot.disconnect,
		"players":     robot.players,
		"queue":       robot.queue,
		"clear-queue": robot.clearQueue,
		"queue-type":  robot.queueType,
		"shuffle":     robot.shuffle,
		"seek":        robot.seek,
		"volume":      robot.volume,
		"skip":        robot.skip,
		"bass-boost":  robot.bassBoost,
		"summon":      robot.summon,
	}

	return robot, nil
}
