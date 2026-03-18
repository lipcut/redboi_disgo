package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/dotenv-org/godotenvvault"

	"github.com/disgoorg/disgo"
	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/cache"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/gateway"
	"github.com/disgoorg/disgolink/v3/disgolink"
	"github.com/disgoorg/snowflake/v2"
)

var robot = Bot{
	Queues: make(map[snowflake.ID]*Queue),
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	if err := godotenvvault.Load(); err != nil {
		log.Fatal(err)
	}

	var (
		Token = os.Getenv("TOKEN")

		guildID = snowflake.GetEnv("GUILD_ID")

		NodeName      = os.Getenv("NODE_NAME")
		NodeAddress   = os.Getenv("NODE_ADDRESS")
		NodePassword  = os.Getenv("NODE_PASSWORD")
		NodeSecure, _ = strconv.ParseBool(os.Getenv("NODE_SECURE"))
	)

	client, err := disgo.New(Token,
		bot.WithGatewayConfigOpts(
			gateway.WithIntents(gateway.IntentsGuild),
		),
		bot.WithCacheConfigOpts(
			cache.WithCaches(cache.FlagVoiceStates),
		),
		bot.WithEventListenerFunc(robot.onApplicationCommand),
		bot.WithEventListenerFunc(robot.onVoiceStateUpdate),
		bot.WithEventListenerFunc(robot.onVoiceServerUpdate),
	)
	if err != nil {
		slog.Error("error while building disgo client", slog.Any("err", err))
		os.Exit(1)
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = client.OpenGateway(ctx); err != nil {
		slog.Error("failed to open gateway", slog.Any("err", err))
		os.Exit(1)
	}
	defer client.Close(context.TODO())

	node, err := robot.Lavalink.AddNode(ctx, disgolink.NodeConfig{
		Name:     NodeName,
		Address:  NodeAddress,
		Password: NodePassword,
		Secure:   NodeSecure,
	})
	if err != nil {
		log.Fatal(err)
	}
	version, err := node.Version(ctx)
	if err != nil {
		slog.Error("failed to add node", slog.Any("err", err))
		os.Exit(1)
	}
	fmt.Printf("Lavalink version: %v\n", version)
	if err != nil {
		slog.Error("failed to get node version", slog.Any("err", err))
		os.Exit(1)
	}

	server(&robot, guildID)

	slog.Info("DisGo example is now running. Press CTRL-C to exit.", slog.String("node_version", version), slog.String("node_session_id", node.SessionID()))
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-s
}
