package main

import (
	"context"
	"log/slog"

	"github.com/disgoorg/disgolink/v3/disgolink"
	"github.com/disgoorg/disgolink/v3/lavalink"
)

func (b *Bot) onPlayerPause(player disgolink.Player, event lavalink.PlayerPauseEvent) {
	slog.Info("player paused", slog.Any("event", event))
	b.publish()
}

func (b *Bot) onPlayerResume(player disgolink.Player, event lavalink.PlayerResumeEvent) {
	slog.Info("player resumed", slog.Any("event", event))
	b.publish()
}

func (b *Bot) onTrackStart(player disgolink.Player, event lavalink.TrackStartEvent) {
	slog.Info("track started", slog.Any("event", event))
	b.publish()
}

func (b *Bot) onTrackEnd(player disgolink.Player, event lavalink.TrackEndEvent) {
	if !event.Reason.MayStartNext() {
		return
	}

	queue := b.Queues.Get(event.GuildID())
	var (
		nextTrack lavalink.Track
		err       error
	)
	updateOption := lavalink.WithNullTrack()

	switch queue.Type {
	case QueueTypeNormal:
		nextTrack, err = queue.Next()

	case QueueTypeRepeatTrack:
		nextTrack = event.Track

	case QueueTypeRepeatQueue:
		queue.Append(event.Track)
		nextTrack, err = queue.Next()
	}

	if err != nil {
		slog.Info("no next track available, stopping player", slog.Any("err", err))
	} else {
		updateOption = lavalink.WithTrack(nextTrack)
	}

	if err := player.Update(context.TODO(), updateOption); err != nil {
		slog.Error("failed to update player on track end", slog.Any("err", err))
	}
	b.publish()
}

func (b *Bot) onTrackException(player disgolink.Player, event lavalink.TrackExceptionEvent) {
	slog.Info("track exception", slog.Any("event", event))
}

func (b *Bot) onTrackStuck(player disgolink.Player, event lavalink.TrackStuckEvent) {
	slog.Info("track stuck", slog.Any("event", event))
}

func (b *Bot) onWebSocketClosed(player disgolink.Player, event lavalink.WebSocketClosedEvent) {
	slog.Info("websocket closed", slog.Any("event", event))
}

func (b *Bot) onUnknownEvent(p disgolink.Player, e lavalink.UnknownEvent) {
	slog.Info("unknown event", slog.Any("event", e.Type()), slog.String("data", string(e.Data)))
}
