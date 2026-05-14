package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"

	"github.com/evsamsonov/raftogram/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "raftogram: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	var (
		configPath = flag.String("config", envOr("RAFTOGRAM_CONFIG", "config.yaml"), "path to YAML config file")
		dev        = flag.Bool("dev", envBool("RAFTOGRAM_DEV"), "enable development logger (human-readable output)")
	)
	flag.Parse()

	logger, err := buildLogger(*dev)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}
	defer logger.Sync() //nolint:errcheck

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger.Info("Starting raftogram",
		zap.String("node_id", cfg.Cluster.NodeID),
		zap.String("raft_bind", cfg.Cluster.RaftBindAddr),
		zap.String("grpc_bind", cfg.Cluster.GRPCBindAddr),
		zap.Int("peers", len(cfg.Cluster.Peers)),
	)

	<-ctx.Done()
	logger.Info("Shutting down")
	return nil
}

func buildLogger(dev bool) (*zap.Logger, error) {
	if dev {
		return zap.NewDevelopment()
	}
	return zap.NewProduction()
}

// envOr returns the value of the environment variable named by key,
// or fallback if the variable is not set or empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envBool returns true if the environment variable named by key is set to
// a non-empty value.
func envBool(key string) bool {
	return os.Getenv(key) != ""
}
