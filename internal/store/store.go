package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/982945902/hermes/internal/model"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

type Store struct {
	client   *mongo.Client
	database *mongo.Database
	channels *mongo.Collection
	users    *mongo.Collection
	tokens   *mongo.Collection
}

func Connect(ctx context.Context, uri string, database string) (*Store, error) {
	client, err := mongo.Connect(options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, err
	}
	db := client.Database(database)
	return &Store{
		client:   client,
		database: db,
		channels: db.Collection("channels"),
		users:    db.Collection("users"),
		tokens:   db.Collection("tokens"),
	}, nil
}

func (s *Store) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

func (s *Store) EnsureIndexes(ctx context.Context) error {
	if _, err := s.channels.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "enabled", Value: 1}}},
		{Keys: bson.D{{Key: "models", Value: 1}}},
		{Keys: bson.D{{Key: "provider", Value: 1}}},
	}); err != nil {
		return err
	}
	if _, err := s.users.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "username", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "group", Value: 1}}},
	}); err != nil {
		return err
	}
	_, err := s.tokens.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "key_hash", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "user_id", Value: 1}}},
		{Keys: bson.D{{Key: "status", Value: 1}}},
		{Keys: bson.D{{Key: "expires_at", Value: 1}}},
	})
	return err
}

func (s *Store) ListChannels(ctx context.Context, includeDisabled bool) ([]model.Channel, error) {
	filter := bson.M{}
	if !includeDisabled {
		filter["enabled"] = true
	}
	cursor, err := s.channels.Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "priority", Value: -1}, {Key: "updated_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var channels []model.Channel
	if err := cursor.All(ctx, &channels); err != nil {
		return nil, err
	}
	for i := range channels {
		channels[i].Normalize()
	}
	return channels, nil
}

func (s *Store) ListPublicModels(ctx context.Context) ([]string, error) {
	channels, err := s.ListChannels(ctx, false)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	models := make([]string, 0)
	for _, channel := range channels {
		for _, item := range channel.Models {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			if _, ok := seen[item]; ok {
				continue
			}
			seen[item] = struct{}{}
			models = append(models, item)
		}
	}
	return models, nil
}

func (s *Store) FindChannelsForModel(ctx context.Context, name string) ([]model.Channel, error) {
	cursor, err := s.channels.Find(ctx, bson.M{
		"enabled": true,
		"models":  name,
	}, options.Find().SetSort(bson.D{{Key: "priority", Value: -1}, {Key: "updated_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var channels []model.Channel
	if err := cursor.All(ctx, &channels); err != nil {
		return nil, err
	}
	for i := range channels {
		channels[i].Normalize()
	}
	return channels, nil
}

func (s *Store) GetChannel(ctx context.Context, id string) (*model.Channel, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var channel model.Channel
	if err := s.channels.FindOne(ctx, bson.M{"_id": objectID}).Decode(&channel); err != nil {
		return nil, err
	}
	channel.Normalize()
	return &channel, nil
}

func (s *Store) CreateChannel(ctx context.Context, channel *model.Channel) error {
	channel.Normalize()
	now := time.Now()
	channel.ID = bson.NewObjectID()
	channel.CreatedAt = now
	channel.UpdatedAt = now
	_, err := s.channels.InsertOne(ctx, channel)
	return err
}

func (s *Store) UpdateChannel(ctx context.Context, id string, input *model.Channel) error {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	existing, err := s.GetChannel(ctx, id)
	if err != nil {
		return err
	}
	preserveChannelSecrets(input, existing)
	input.Normalize()
	input.ID = objectID
	input.CreatedAt = existing.CreatedAt
	input.UpdatedAt = time.Now()
	_, err = s.channels.ReplaceOne(ctx, bson.M{"_id": objectID}, input)
	return err
}

func (s *Store) DeleteChannel(ctx context.Context, id string) error {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	result, err := s.channels.DeleteOne(ctx, bson.M{"_id": objectID})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func preserveChannelSecrets(input *model.Channel, existing *model.Channel) {
	if isBlankOrMasked(input.APIKey) {
		input.APIKey = existing.APIKey
	}
	existingByID := map[string]model.ChannelKey{}
	existingByName := map[string]model.ChannelKey{}
	for _, key := range existing.Keys {
		if key.ID != "" {
			existingByID[key.ID] = key
		}
		if key.Name != "" {
			existingByName[key.Name] = key
		}
	}
	for i := range input.Keys {
		if !isBlankOrMasked(input.Keys[i].APIKey) {
			continue
		}
		if existingKey, ok := existingByID[input.Keys[i].ID]; ok {
			input.Keys[i].APIKey = existingKey.APIKey
			continue
		}
		if existingKey, ok := existingByName[input.Keys[i].Name]; ok {
			input.Keys[i].APIKey = existingKey.APIKey
			continue
		}
		if len(input.Keys) == 1 {
			input.Keys[i].APIKey = existing.APIKey
		}
	}
}

func isBlankOrMasked(key string) bool {
	key = strings.TrimSpace(key)
	return key == "" || strings.Contains(key, "********")
}

func (s *Store) UpdateChannelTestResult(ctx context.Context, id string, lastError string) error {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	now := time.Now()
	result, err := s.channels.UpdateOne(ctx, bson.M{"_id": objectID}, bson.M{"$set": bson.M{
		"last_error":   lastError,
		"last_test_at": now,
		"updated_at":   now,
	}})
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return errors.New("channel not found")
	}
	return nil
}
