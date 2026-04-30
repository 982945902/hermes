package store

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/982945902/hermes/internal/model"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

var (
	ErrGatewayTokenRequired = errors.New("gateway token required")
	ErrGatewayTokenInvalid  = errors.New("gateway token invalid")
	ErrGatewayTokenDisabled = errors.New("gateway token disabled")
	ErrGatewayTokenExpired  = errors.New("gateway token expired")
	ErrGatewayUserDisabled  = errors.New("gateway user disabled")
	ErrGatewayIPForbidden   = errors.New("gateway token ip forbidden")
)

type GatewayIdentity struct {
	User  model.User
	Token model.UserToken
}

func (s *Store) ListUsers(ctx context.Context) ([]model.User, error) {
	cursor, err := s.users.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var users []model.User
	if err := cursor.All(ctx, &users); err != nil {
		return nil, err
	}
	for i := range users {
		users[i].Normalize()
	}
	return users, nil
}

func (s *Store) GetUser(ctx context.Context, id string) (*model.User, error) {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	var user model.User
	if err := s.users.FindOne(ctx, bson.M{"_id": objectID}).Decode(&user); err != nil {
		return nil, err
	}
	user.Normalize()
	return &user, nil
}

func (s *Store) CreateUser(ctx context.Context, user *model.User) error {
	user.Normalize()
	if user.Username == "" {
		return errors.New("username is required")
	}
	now := time.Now()
	user.ID = bson.NewObjectID()
	user.CreatedAt = now
	user.UpdatedAt = now
	_, err := s.users.InsertOne(ctx, user)
	return err
}

func (s *Store) UpdateUser(ctx context.Context, id string, input *model.User) error {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	existing, err := s.GetUser(ctx, id)
	if err != nil {
		return err
	}
	input.Normalize()
	if input.Username == "" {
		return errors.New("username is required")
	}
	input.ID = objectID
	input.CreatedAt = existing.CreatedAt
	input.UpdatedAt = time.Now()
	input.RequestCount = existing.RequestCount
	_, err = s.users.ReplaceOne(ctx, bson.M{"_id": objectID}, input)
	return err
}

func (s *Store) DeleteUser(ctx context.Context, id string) error {
	objectID, err := bson.ObjectIDFromHex(id)
	if err != nil {
		return err
	}
	result, err := s.users.DeleteOne(ctx, bson.M{"_id": objectID})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}
	_, err = s.tokens.DeleteMany(ctx, bson.M{"user_id": objectID})
	return err
}

func (s *Store) ListUserTokens(ctx context.Context, userID string) ([]model.UserToken, error) {
	objectID, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return nil, err
	}
	if _, err := s.GetUser(ctx, userID); err != nil {
		return nil, err
	}
	cursor, err := s.tokens.Find(ctx, bson.M{"user_id": objectID}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var tokens []model.UserToken
	if err := cursor.All(ctx, &tokens); err != nil {
		return nil, err
	}
	for i := range tokens {
		tokens[i].Normalize()
	}
	return tokens, nil
}

func (s *Store) GetUserToken(ctx context.Context, userID string, tokenID string) (*model.UserToken, error) {
	userObjectID, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return nil, err
	}
	tokenObjectID, err := bson.ObjectIDFromHex(tokenID)
	if err != nil {
		return nil, err
	}
	var token model.UserToken
	if err := s.tokens.FindOne(ctx, bson.M{"_id": tokenObjectID, "user_id": userObjectID}).Decode(&token); err != nil {
		return nil, err
	}
	token.Normalize()
	return &token, nil
}

func (s *Store) CreateUserToken(ctx context.Context, userID string, token *model.UserToken) (string, error) {
	user, err := s.GetUser(ctx, userID)
	if err != nil {
		return "", err
	}
	rawKey, err := model.GenerateUserTokenKey()
	if err != nil {
		return "", err
	}
	token.Normalize()
	now := time.Now()
	token.ID = bson.NewObjectID()
	token.UserID = user.ID
	token.KeyHash = model.HashUserTokenKey(rawKey)
	token.KeyPreview = model.PreviewTokenKey(rawKey)
	token.CreatedAt = now
	token.UpdatedAt = now
	_, err = s.tokens.InsertOne(ctx, token)
	if err != nil {
		return "", err
	}
	return rawKey, nil
}

func (s *Store) UpdateUserToken(ctx context.Context, userID string, tokenID string, input *model.UserToken) error {
	existing, err := s.GetUserToken(ctx, userID, tokenID)
	if err != nil {
		return err
	}
	input.Normalize()
	input.ID = existing.ID
	input.UserID = existing.UserID
	input.KeyHash = existing.KeyHash
	input.KeyPreview = existing.KeyPreview
	input.CreatedAt = existing.CreatedAt
	input.UpdatedAt = time.Now()
	input.LastUsedAt = existing.LastUsedAt
	input.RequestCount = existing.RequestCount
	_, err = s.tokens.ReplaceOne(ctx, bson.M{"_id": existing.ID, "user_id": existing.UserID}, input)
	return err
}

func (s *Store) DeleteUserToken(ctx context.Context, userID string, tokenID string) error {
	userObjectID, err := bson.ObjectIDFromHex(userID)
	if err != nil {
		return err
	}
	tokenObjectID, err := bson.ObjectIDFromHex(tokenID)
	if err != nil {
		return err
	}
	result, err := s.tokens.DeleteOne(ctx, bson.M{"_id": tokenObjectID, "user_id": userObjectID})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (s *Store) ValidateGatewayToken(ctx context.Context, rawKey string, clientIP string) (*GatewayIdentity, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return nil, ErrGatewayTokenRequired
	}
	var token model.UserToken
	if err := s.tokens.FindOne(ctx, bson.M{"key_hash": model.HashUserTokenKey(rawKey)}).Decode(&token); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrGatewayTokenInvalid
		}
		return nil, err
	}
	token.Normalize()
	now := time.Now()
	if token.Status != model.TokenStatusEnabled {
		return nil, ErrGatewayTokenDisabled
	}
	if token.ExpiresAt != nil && token.ExpiresAt.Before(now) {
		return nil, ErrGatewayTokenExpired
	}
	if !ipAllowed(clientIP, token.AllowIPs) {
		return nil, ErrGatewayIPForbidden
	}
	var user model.User
	if err := s.users.FindOne(ctx, bson.M{"_id": token.UserID}).Decode(&user); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrGatewayTokenInvalid
		}
		return nil, err
	}
	user.Normalize()
	if user.Status != model.UserStatusEnabled {
		return nil, ErrGatewayUserDisabled
	}
	_, _ = s.tokens.UpdateOne(ctx, bson.M{"_id": token.ID}, bson.M{
		"$set": bson.M{"last_used_at": now, "updated_at": now},
		"$inc": bson.M{"request_count": 1},
	})
	_, _ = s.users.UpdateOne(ctx, bson.M{"_id": user.ID}, bson.M{
		"$set": bson.M{"updated_at": now},
		"$inc": bson.M{"request_count": 1},
	})
	return &GatewayIdentity{User: user, Token: token}, nil
}

func ipAllowed(clientIP string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	parsed := net.ParseIP(strings.TrimSpace(clientIP))
	if parsed == nil {
		return false
	}
	for _, item := range allowed {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if strings.Contains(item, "/") {
			_, network, err := net.ParseCIDR(item)
			if err == nil && network.Contains(parsed) {
				return true
			}
			continue
		}
		exact := net.ParseIP(item)
		if exact != nil && exact.Equal(parsed) {
			return true
		}
	}
	return false
}
