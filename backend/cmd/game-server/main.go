package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/ecscard/game/internal/game"
	pb "github.com/ecscard/game/internal/proto"
	"github.com/ecscard/game/internal/server"
	_ "github.com/ecscard/game/internal/game"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	redisPassword := getEnv("REDIS_PASSWORD", "")
	redisDB, _ := strconv.Atoi(getEnv("REDIS_DB", "0"))
	mongoURI := getEnv("MONGO_URI", "mongodb://admin:password@localhost:27017")
	serverAddr := getEnv("SERVER_ADDR", ":50051")
	useClusterStr := getEnv("REDIS_USE_CLUSTER", "false")
	useCluster := strings.EqualFold(useClusterStr, "true")
	clusterAddrsStr := getEnv("REDIS_CLUSTER_ADDRS", "")
	var clusterAddrs []string
	if clusterAddrsStr != "" {
		clusterAddrs = strings.Split(clusterAddrsStr, ",")
	}

	lis, err := net.Listen("tcp", serverAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	gs, err := server.NewGameServer(redisAddr, redisPassword, redisDB, mongoURI, serverAddr, useCluster, clusterAddrs)
	if err != nil {
		log.Fatalf("failed to create game server: %v", err)
	}
	defer gs.Close()

	var gm *game.GameManager
	if gs != nil {
		gm = gs.GameManager()
	}

	var replaySrv *server.ReplayServer
	var balanceSrv *server.BalanceServer
	if gm != nil {
		replayStore := gm.GetReplayStore()
		specMgr := gm.GetSpecMgr()
		statsCollector := gm.GetStatsCollector()
		hotUpdateMgr := gm.GetHotUpdateMgr()
		changeLogStore := gm.GetChangeLogStore()

		if replayStore != nil && specMgr != nil {
			replaySrv = server.NewReplayServer(replayStore, specMgr)
		}
		if statsCollector != nil && hotUpdateMgr != nil && changeLogStore != nil {
			balanceSrv = server.NewBalanceServer(statsCollector, hotUpdateMgr, changeLogStore)
		}
	}

	grpcServer := grpc.NewServer()
	pb.RegisterGameServiceServer(grpcServer, gs)
	if replaySrv != nil {
		pb.RegisterReplayServiceServer(grpcServer, replaySrv)
		log.Println("ReplayService registered")
	}
	if balanceSrv != nil {
		pb.RegisterBalanceServiceServer(grpcServer, balanceSrv)
		log.Println("BalanceService registered")
	}
	reflection.Register(grpcServer)

	go func() {
		log.Printf("Game server listening on %s", serverAddr)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down game server...")
	grpcServer.GracefulStop()
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
