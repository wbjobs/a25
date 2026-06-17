package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/ecscard/game/internal/server"
	pb "github.com/ecscard/game/proto/v1"
	_ "github.com/ecscard/game/internal/game"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	mongoURI := getEnv("MONGO_URI", "mongodb://admin:password@localhost:27017")
	serverAddr := getEnv("SERVER_ADDR", ":50051")

	lis, err := net.Listen("tcp", serverAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	gs, err := server.NewGameServer(redisAddr, mongoURI, serverAddr)
	if err != nil {
		log.Fatalf("failed to create game server: %v", err)
	}
	defer gs.Close()

	grpcServer := grpc.NewServer()
	pb.RegisterGameServiceServer(grpcServer, gs)
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
