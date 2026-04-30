package model

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
)

const (
	UserStatusEnabled  = 1
	UserStatusDisabled = 2
)

const (
	TokenStatusEnabled  = 1
	TokenStatusDisabled = 2
)

type User struct {
	ID           bson.ObjectID `bson:"_id,omitempty" json:"id"`
	Username     string        `bson:"username" json:"username"`
	DisplayName  string        `bson:"display_name" json:"display_name"`
	Status       int           `bson:"status" json:"status"`
	Group        string        `bson:"group" json:"group"`
	Remark       string        `bson:"remark,omitempty" json:"remark,omitempty"`
	RequestCount int64         `bson:"request_count" json:"request_count"`
	CreatedAt    time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time     `bson:"updated_at" json:"updated_at"`
}

type UserToken struct {
	ID                 bson.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID             bson.ObjectID `bson:"user_id" json:"user_id"`
	Name               string        `bson:"name" json:"name"`
	KeyHash            string        `bson:"key_hash" json:"-"`
	KeyPreview         string        `bson:"key_preview" json:"key_preview"`
	Status             int           `bson:"status" json:"status"`
	ExpiresAt          *time.Time    `bson:"expires_at,omitempty" json:"expires_at,omitempty"`
	LastUsedAt         *time.Time    `bson:"last_used_at,omitempty" json:"last_used_at,omitempty"`
	ModelLimitsEnabled bool          `bson:"model_limits_enabled" json:"model_limits_enabled"`
	ModelLimits        []string      `bson:"model_limits" json:"model_limits"`
	AllowIPs           []string      `bson:"allow_ips" json:"allow_ips"`
	RequestCount       int64         `bson:"request_count" json:"request_count"`
	CreatedAt          time.Time     `bson:"created_at" json:"created_at"`
	UpdatedAt          time.Time     `bson:"updated_at" json:"updated_at"`
}

func (u *User) Normalize() {
	u.Username = strings.TrimSpace(u.Username)
	u.DisplayName = strings.TrimSpace(u.DisplayName)
	u.Group = strings.TrimSpace(u.Group)
	u.Remark = strings.TrimSpace(u.Remark)
	if u.DisplayName == "" {
		u.DisplayName = u.Username
	}
	if u.Group == "" {
		u.Group = "default"
	}
	if u.Status == 0 {
		u.Status = UserStatusEnabled
	}
}

func (t *UserToken) Normalize() {
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		t.Name = "default"
	}
	if t.Status == 0 {
		t.Status = TokenStatusEnabled
	}
	t.ModelLimits = normalizeStringList(t.ModelLimits)
	t.AllowIPs = normalizeStringList(t.AllowIPs)
	if !t.ModelLimitsEnabled {
		t.ModelLimits = []string{}
	}
}

func (t UserToken) AllowsModel(model string) bool {
	if !t.ModelLimitsEnabled {
		return true
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	for _, item := range t.ModelLimits {
		if item == model {
			return true
		}
	}
	return false
}

func GenerateUserTokenKey() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return "sk-hermes-" + base64.RawURLEncoding.EncodeToString(bytes), nil
}

func HashUserTokenKey(key string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return hex.EncodeToString(sum[:])
}

func PreviewTokenKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return ""
	}
	if len(key) <= 12 {
		return strings.Repeat("*", len(key))
	}
	return key[:10] + "********" + key[len(key)-4:]
}

func normalizeStringList(items []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		result = append(result, item)
	}
	return result
}
