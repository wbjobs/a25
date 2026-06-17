package mongodb

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Store struct {
	client     *mongo.Client
	database   *mongo.Database
	cards      *mongo.Collection
	players    *mongo.Collection
	games      *mongo.Collection
	decks      *mongo.Collection
}

type CardDocument struct {
	ID          string    `bson:"_id"`
	TemplateID  string    `bson:"template_id"`
	Name        string    `bson:"name"`
	Type        string    `bson:"type"`
	Cost        int       `bson:"cost"`
	Attack      int       `bson:"attack,omitempty"`
	Health      int       `bson:"health,omitempty"`
	Description string    `bson:"description"`
	Rarity      string    `bson:"rarity"`
	CreatedAt   time.Time `bson:"created_at"`
}

type PlayerDocument struct {
	ID         string    `bson:"_id"`
	Username   string    `bson:"username"`
	Email      string    `bson:"email,omitempty"`
	Rating     int       `bson:"rating"`
	Wins       int       `bson:"wins"`
	Losses     int       `bson:"losses"`
	WinStreak  int       `bson:"win_streak"`
	TotalGames int       `bson:"total_games"`
	Cards      []string  `bson:"cards"`
	Decks      []string  `bson:"decks"`
	CreatedAt  time.Time `bson:"created_at"`
	UpdatedAt  time.Time `bson:"updated_at"`
}

type GameDocument struct {
	ID            string    `bson:"_id"`
	MatchID       string    `bson:"match_id"`
	Player1ID     string    `bson:"player1_id"`
	Player2ID     string    `bson:"player2_id"`
	Player1Name   string    `bson:"player1_name"`
	Player2Name   string    `bson:"player2_name"`
	WinnerID      string    `bson:"winner_id,omitempty"`
	LoserID       string    `bson:"loser_id,omitempty"`
	Turns         int       `bson:"turns"`
	DurationMs    int64     `bson:"duration_ms"`
	Conceded      bool      `bson:"conceded"`
	GameType      string    `bson:"game_type"`
	Player1Deck   []string  `bson:"player1_deck"`
	Player2Deck   []string  `bson:"player2_deck"`
	CreatedAt     time.Time `bson:"created_at"`
	EndedAt       time.Time `bson:"ended_at,omitempty"`
}

type DeckDocument struct {
	ID        string    `bson:"_id"`
	PlayerID  string    `bson:"player_id"`
	Name      string    `bson:"name"`
	Cards     []string  `bson:"cards"`
	IsDefault bool      `bson:"is_default"`
	CreatedAt time.Time `bson:"created_at"`
	UpdatedAt time.Time `bson:"updated_at"`
}

func NewStore(uri, dbName string) (*Store, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}

	db := client.Database(dbName)

	return &Store{
		client:   client,
		database: db,
		cards:    db.Collection("cards"),
		players:  db.Collection("players"),
		games:    db.Collection("games"),
		decks:    db.Collection("decks"),
	}, nil
}

func (s *Store) CreatePlayer(ctx context.Context, player *PlayerDocument) error {
	player.CreatedAt = time.Now()
	player.UpdatedAt = time.Now()
	if player.ID == "" {
		player.ID = primitive.NewObjectID().Hex()
	}
	_, err := s.players.InsertOne(ctx, player)
	return err
}

func (s *Store) GetPlayer(ctx context.Context, playerID string) (*PlayerDocument, error) {
	var player PlayerDocument
	err := s.players.FindOne(ctx, bson.M{"_id": playerID}).Decode(&player)
	if err != nil {
		return nil, err
	}
	return &player, nil
}

func (s *Store) UpdatePlayerStats(ctx context.Context, playerID string, won bool) error {
	update := bson.M{
		"$inc": bson.M{
			"total_games": 1,
		},
		"$set": bson.M{
			"updated_at": time.Now(),
		},
	}

	if won {
		update["$inc"].(bson.M)["wins"] = 1
		update["$inc"].(bson.M)["win_streak"] = 1
	} else {
		update["$inc"].(bson.M)["losses"] = 1
		update["$set"].(bson.M)["win_streak"] = 0
	}

	_, err := s.players.UpdateOne(ctx, bson.M{"_id": playerID}, update)
	return err
}

func (s *Store) UpdatePlayerRating(ctx context.Context, playerID string, delta int) error {
	_, err := s.players.UpdateOne(ctx, bson.M{"_id": playerID}, bson.M{
		"$inc": bson.M{"rating": delta},
		"$set": bson.M{"updated_at": time.Now()},
	})
	return err
}

func (s *Store) CreateGame(ctx context.Context, game *GameDocument) error {
	game.CreatedAt = time.Now()
	if game.ID == "" {
		game.ID = primitive.NewObjectID().Hex()
	}
	_, err := s.games.InsertOne(ctx, game)
	return err
}

func (s *Store) UpdateGameResult(ctx context.Context, gameID, winnerID, loserID string, turns int, durationMs int64, conceded bool) error {
	_, err := s.games.UpdateOne(ctx, bson.M{"_id": gameID}, bson.M{
		"$set": bson.M{
			"winner_id":   winnerID,
			"loser_id":    loserID,
			"turns":       turns,
			"duration_ms": durationMs,
			"conceded":    conceded,
			"ended_at":    time.Now(),
		},
	})
	return err
}

func (s *Store) GetGame(ctx context.Context, gameID string) (*GameDocument, error) {
	var game GameDocument
	err := s.games.FindOne(ctx, bson.M{"_id": gameID}).Decode(&game)
	if err != nil {
		return nil, err
	}
	return &game, nil
}

func (s *Store) GetPlayerGames(ctx context.Context, playerID string, limit int64) ([]*GameDocument, error) {
	opts := options.Find().SetSort(bson.M{"created_at": -1}).SetLimit(limit)
	cursor, err := s.games.Find(ctx, bson.M{
		"$or": []bson.M{
			{"player1_id": playerID},
			{"player2_id": playerID},
		},
	}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var games []*GameDocument
	for cursor.Next(ctx) {
		var game GameDocument
		if err := cursor.Decode(&game); err != nil {
			return nil, err
		}
		games = append(games, &game)
	}
	return games, nil
}

func (s *Store) CreateDeck(ctx context.Context, deck *DeckDocument) error {
	deck.CreatedAt = time.Now()
	deck.UpdatedAt = time.Now()
	if deck.ID == "" {
		deck.ID = primitive.NewObjectID().Hex()
	}
	_, err := s.decks.InsertOne(ctx, deck)
	return err
}

func (s *Store) GetDeck(ctx context.Context, deckID string) (*DeckDocument, error) {
	var deck DeckDocument
	err := s.decks.FindOne(ctx, bson.M{"_id": deckID}).Decode(&deck)
	if err != nil {
		return nil, err
	}
	return &deck, nil
}

func (s *Store) GetPlayerDecks(ctx context.Context, playerID string) ([]*DeckDocument, error) {
	cursor, err := s.decks.Find(ctx, bson.M{"player_id": playerID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var decks []*DeckDocument
	for cursor.Next(ctx) {
		var deck DeckDocument
		if err := cursor.Decode(&deck); err != nil {
			return nil, err
		}
		decks = append(decks, &deck)
	}
	return decks, nil
}

func (s *Store) UpdateDeck(ctx context.Context, deckID string, cards []string, name string) error {
	_, err := s.decks.UpdateOne(ctx, bson.M{"_id": deckID}, bson.M{
		"$set": bson.M{
			"cards":      cards,
			"name":       name,
			"updated_at": time.Now(),
		},
	})
	return err
}

func (s *Store) AddCardToPlayer(ctx context.Context, playerID, cardTemplateID string) error {
	_, err := s.players.UpdateOne(ctx, bson.M{"_id": playerID}, bson.M{
		"$addToSet": bson.M{"cards": cardTemplateID},
		"$set":      bson.M{"updated_at": time.Now()},
	})
	return err
}

func (s *Store) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx, nil)
}
