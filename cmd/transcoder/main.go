package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/distributed-transcoder/internal/config"
	"github.com/distributed-transcoder/internal/coordinator"
	"github.com/distributed-transcoder/internal/gateway"
	"github.com/distributed-transcoder/internal/infra"
	"github.com/distributed-transcoder/internal/tracing"
	"github.com/distributed-transcoder/internal/worker"
	"github.com/spf13/cobra"
)

var (
	configPath string
	region     string
)

var rootCmd = &cobra.Command{
	Use:   "video-engine",
	Short: "Distributed Video Transcoding Engine",
	Long:  `A high-performance, multi-region distributed video transcoder.`,
}

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Run a server component",
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "config.yaml", "Path to YAML config")
	rootCmd.PersistentFlags().StringVar(&region, "region", "us-east", "Region to run this node in")

	serverCmd.AddCommand(gatewayCmd)
	serverCmd.AddCommand(coordinatorCmd)
	serverCmd.AddCommand(workerCmd)

	rootCmd.AddCommand(serverCmd)
}

func initInfra(cfg *config.Config, role string, needsNATS, needsEtcd bool) (context.Context, context.CancelFunc, *infra.RedisStore, *infra.NATSBus, *infra.EtcdClient, *infra.S3Client) {
	if cfg.NodeID == "" {
		cfg.NodeID = fmt.Sprintf("%s-node-%d", role, time.Now().UnixNano())
	}
	// override region from flag
	if region != "" {
		cfg.Region = region
	}

	if cfg.Tracing.Endpoint != "" {
		tp, err := tracing.InitTracer(context.Background(), fmt.Sprintf("transcoder-%s", role), cfg.Tracing.Endpoint, cfg.Tracing.SampleRate)
		if err != nil {
			log.Printf("failed to initialize tracer: %v", err)
		} else {
			// fire and forget shutdown for simplicity in this daemon
			_ = tp
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)

	stateStore, err := infra.NewRedisStore(cfg.Redis)
	if err != nil {
		log.Fatalf("failed to init redis: %v", err)
	}
	
	var messageBus *infra.NATSBus
	if needsNATS {
		messageBus, err = infra.NewNATSBus(cfg.NATS)
		if err != nil {
			log.Fatalf("failed to init NATS: %v", err)
		}
		if err := messageBus.InitEcosystem(cfg.Coordinator.NATSShardCount); err != nil {
			log.Fatalf("failed to init NATS JetStream ecosystem: %v", err)
		}
	}
	
	var coord *infra.EtcdClient
	if needsEtcd {
		coord, err = infra.NewEtcdClient(cfg.Etcd)
		if err != nil {
			log.Fatalf("failed to init etcd: %v", err)
		}
	}

	objectStore, err := infra.NewS3Client(cfg.ObjectStore)
	if err != nil {
		log.Fatalf("failed to init s3: %v", err)
	}

	return ctx, cancel, stateStore, messageBus, coord, objectStore
}

var gatewayCmd = &cobra.Command{
	Use:   "gateway",
	Short: "Run the API Gateway",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			log.Fatalf("failed to load config: %v", err)
		}
		ctx, cancel, stateStore, messageBus, _, objectStore := initInfra(&cfg, "gateway", true, false)
		defer cancel()
		if stateStore != nil { defer stateStore.Close() }
		if messageBus != nil { defer messageBus.Close() }

		daemon := gateway.NewGatewayDaemon(cfg, stateStore, objectStore, messageBus)
		if err := daemon.Run(ctx); err != nil {
			log.Fatalf("gateway error: %v", err)
		}
	},
}

var coordinatorCmd = &cobra.Command{
	Use:   "coordinator",
	Short: "Run the Etcd Coordinator",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			log.Fatalf("failed to load config: %v", err)
		}
		ctx, cancel, stateStore, messageBus, coord, objectStore := initInfra(&cfg, "coordinator", true, true)
		defer cancel()
		if stateStore != nil { defer stateStore.Close() }
		if messageBus != nil { defer messageBus.Close() }
		if coord != nil { defer coord.Close() }

		daemon := coordinator.NewCoordinatorDaemon(cfg, cfg.NodeID, stateStore, messageBus, coord, objectStore)
		daemon.Run(ctx)
	},
}

var workerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Run a Transcode Worker",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, err := config.LoadConfig(configPath)
		if err != nil {
			log.Fatalf("failed to load config: %v", err)
		}
		ctx, cancel, stateStore, messageBus, _, objectStore := initInfra(&cfg, "worker", true, false)
		defer cancel()
		if stateStore != nil { defer stateStore.Close() }
		if messageBus != nil { defer messageBus.Close() }

		daemon := worker.NewWorkerDaemon(cfg, stateStore, objectStore, messageBus)
		if err := daemon.Run(ctx); err != nil {
			log.Fatalf("worker error: %v", err)
		}
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
