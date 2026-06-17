package main

import (
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/ecscard/game/internal/server"
	pb "github.com/ecscard/game/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	redisAddr := getEnv("REDIS_ADDR", "localhost:6379")
	mongoURI := getEnv("MONGO_URI", "mongodb://admin:password@localhost:27017")
	gameServerAddr := getEnv("GAME_SERVER_ADDR", "localhost:50051")
	serverAddr := getEnv("SERVER_ADDR", ":50052")

	conn, err := grpc.Dial(gameServerAddr, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("failed to connect to game server: %v", err)
	}
	defer conn.Close()

	gameClient := pb.NewGameServiceClient(conn)

	ms, err := server.NewMatchServer(redisAddr, mongoURI, gameServerAddr, gameClient)
	if err != nil {
		log.Fatalf("failed to create match server: %v", err)
	}
	defer ms.Close()

	lis, err := net.Listen("tcp", serverAddr)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterMatchServiceServer(grpcServer, ms)
	reflection.Register(grpcServer)

	go func() {
		log.Printf("Match server listening on %s", serverAddr)
		if err := grpcServer.Serve(lis); err != nil {
			log.Fatalf("failed to serve: %v", err)
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	log.Println("Shutting down match server...")
	grpcServer.GracefulStop()
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
