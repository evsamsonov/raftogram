package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"

	"github.com/evsamsonov/raftogram/internal/config"
	"github.com/evsamsonov/raftogram/internal/gen/raftogrampb"
	"github.com/evsamsonov/raftogram/internal/grpcapi"
	"github.com/evsamsonov/raftogram/internal/healthhttp"
	"github.com/evsamsonov/raftogram/internal/messenger"
	"github.com/evsamsonov/raftogram/internal/raftcluster"
	"github.com/hashicorp/raft"
)

func serveUntilShutdown(
	ctx context.Context,
	logger *zap.Logger,
	cl config.Cluster,
	r *raft.Raft,
	fsm *messenger.FSM,
	node *raftcluster.Node,
) error {
	grpcListener, err := net.Listen("tcp", cl.GRPCBindAddr)
	if err != nil {
		return fmt.Errorf("listen grpc: %w", err)
	}

	grpcServer := grpc.NewServer()
	raftogrampb.RegisterMessengerServer(grpcServer, grpcapi.NewServer(r, fsm, cl.Peers))

	healthListener, err := net.Listen("tcp", cl.HealthBindAddr)
	if err != nil {
		return fmt.Errorf("listen health http: %w", err)
	}

	healthServer := &http.Server{Handler: healthhttp.NewMux(&healthhttp.Handler{
		NodeID: cl.NodeID,
		Peers:  cl.Peers,
		Source: node,
	})}

	grpcDone := make(chan struct{})
	go func() {
		defer close(grpcDone)
		if err := grpcServer.Serve(grpcListener); err != nil {
			logger.Error("gRPC server exited", zap.Error(err))
		}
	}()

	healthDone := make(chan struct{})
	go func() {
		defer close(healthDone)
		if err := healthServer.Serve(healthListener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("Health HTTP server exited", zap.Error(err))
		}
	}()

	<-ctx.Done()
	logger.Info("Shutting down")
	grpcServer.GracefulStop()
	<-grpcDone

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("Shutdown health HTTP server", zap.Error(err))
	}
	<-healthDone

	return nil
}
