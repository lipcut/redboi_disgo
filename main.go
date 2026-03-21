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

	"github.com/disgoorg/disgolink/v3/disgolink"
	"github.com/disgoorg/snowflake/v2"
)

var (
	Token         = os.Getenv("TOKEN")
	guildID       = snowflake.GetEnv("GUILD_ID")
	NodeName      = os.Getenv("NODE_NAME")
	NodeAddress   = os.Getenv("NODE_ADDRESS")
	NodePassword  = os.Getenv("NODE_PASSWORD")
	NodeSecure, _ = strconv.ParseBool(os.Getenv("NODE_SECURE"))
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	robot, err := discordBot(Token)
	if err != nil {
		slog.Error("failed to setup discord bot", slog.Any("err", err))
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if err = robot.Client.OpenGateway(ctx); err != nil {
		slog.Error("failed to open gateway", slog.Any("err", err))
		os.Exit(1)
	}
	defer robot.Client.Close(context.TODO())

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
		slog.Error("failed to get node version", slog.Any("err", err))
		os.Exit(1)
	}
	fmt.Printf("Lavalink version: %v\n", version)

	go server(robot, guildID)

	slog.Info("Redboi is now running. Press CTRL-C to exit.", slog.String("node_version", version), slog.String("node_session_id", node.SessionID()))
	s := make(chan os.Signal, 1)
	signal.Notify(s, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	<-s
}
