package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/arisone/redcapital/api/pb"
	"github.com/arisone/redcapital/internal/config"
	"github.com/arisone/redcapital/internal/registry"
	"github.com/arisone/redcapital/internal/service"
	"github.com/arisone/redcapital/internal/worker"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	if err := config.Validate(cfg); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	reg, err := registry.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer reg.Close()

	grpcLis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("listen gRPC on %s: %w", cfg.GRPCAddr, err)
	}
	grpcServer := grpc.NewServer()
	pb.RegisterNotificationServiceServer(grpcServer, service.New(reg))

	conn, err := grpc.NewClient(grpcLis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("dial gRPC server: %w", err)
	}
	defer conn.Close()

	gatewayMux := runtime.NewServeMux(runtime.WithMarshalerOption(runtime.MIMEWildcard, &service.LenientJSONPb{}))
	if err := pb.RegisterNotificationServiceHandler(ctx, gatewayMux, conn); err != nil {
		return fmt.Errorf("register HTTP gateway: %w", err)
	}
	httpServer := &http.Server{Addr: cfg.HTTPAddr, Handler: gatewayMux}

	workerCtx, cancelWorker := context.WithCancel(ctx)
	defer cancelWorker()
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		if err := worker.New(reg, cfg.WorkerPollInterval, cfg.LeaseDuration, logger).Run(workerCtx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("worker stopped", "error", err)
		}
	}()

	errCh := make(chan error, 2)
	go func() {
		logger.Info("gRPC server listening", "addr", grpcLis.Addr().String())
		if err := grpcServer.Serve(grpcLis); err != nil {
			errCh <- fmt.Errorf("gRPC server: %w", err)
		}
	}()
	go func() {
		logger.Info("HTTP gateway listening", "addr", cfg.HTTPAddr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("HTTP server: %w", err)
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		return err
	}

	grpcServer.GracefulStop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP gateway shutdown", "error", err)
	}
	cancelWorker()
	<-workerDone
	return nil
}
