package api

import (
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/982945902/hermes/internal/model"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
)

func (h *AdminHandler) ListUsers(c *gin.Context) {
	users, err := h.store.ListUsers(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list users"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": users})
}

func (h *AdminHandler) GetUser(c *gin.Context) {
	user, err := h.store.GetUser(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondAdminNotFound(c, err, "user not found")
		return
	}
	c.JSON(http.StatusOK, user)
}

func (h *AdminHandler) CreateUser(c *gin.Context) {
	var user model.User
	if err := c.ShouldBindJSON(&user); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user"})
		return
	}
	if err := validateUser(&user); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.CreateUser(c.Request.Context(), &user); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create user"})
		return
	}
	c.JSON(http.StatusCreated, user)
}

func (h *AdminHandler) UpdateUser(c *gin.Context) {
	var user model.User
	if err := c.ShouldBindJSON(&user); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid user"})
		return
	}
	if err := validateUser(&user); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.UpdateUser(c.Request.Context(), c.Param("id"), &user); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			c.JSON(http.StatusConflict, gin.H{"error": "username already exists"})
			return
		}
		if errors.Is(err, bson.ErrInvalidHex) || errors.Is(err, mongo.ErrNoDocuments) {
			respondAdminNotFound(c, err, "user not found")
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update user"})
		return
	}
	updated, err := h.store.GetUser(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Status(http.StatusNoContent)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *AdminHandler) DeleteUser(c *gin.Context) {
	if err := h.store.DeleteUser(c.Request.Context(), c.Param("id")); err != nil {
		if errors.Is(err, bson.ErrInvalidHex) || errors.Is(err, mongo.ErrNoDocuments) {
			respondAdminNotFound(c, err, "user not found")
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete user"})
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *AdminHandler) ListUserTokens(c *gin.Context) {
	tokens, err := h.store.ListUserTokens(c.Request.Context(), c.Param("id"))
	if err != nil {
		respondAdminNotFound(c, err, "user not found")
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": tokens})
}

func (h *AdminHandler) GetUserToken(c *gin.Context) {
	token, err := h.store.GetUserToken(c.Request.Context(), c.Param("id"), c.Param("token_id"))
	if err != nil {
		respondAdminNotFound(c, err, "token not found")
		return
	}
	c.JSON(http.StatusOK, token)
}

func (h *AdminHandler) CreateUserToken(c *gin.Context) {
	var token model.UserToken
	if err := c.ShouldBindJSON(&token); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token"})
		return
	}
	if err := validateUserToken(&token); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	key, err := h.store.CreateUserToken(c.Request.Context(), c.Param("id"), &token)
	if err != nil {
		respondAdminNotFound(c, err, "user not found")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": token, "key": key})
}

func (h *AdminHandler) UpdateUserToken(c *gin.Context) {
	var token model.UserToken
	if err := c.ShouldBindJSON(&token); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token"})
		return
	}
	if err := validateUserToken(&token); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := h.store.UpdateUserToken(c.Request.Context(), c.Param("id"), c.Param("token_id"), &token); err != nil {
		respondAdminNotFound(c, err, "token not found")
		return
	}
	updated, err := h.store.GetUserToken(c.Request.Context(), c.Param("id"), c.Param("token_id"))
	if err != nil {
		c.Status(http.StatusNoContent)
		return
	}
	c.JSON(http.StatusOK, updated)
}

func (h *AdminHandler) DeleteUserToken(c *gin.Context) {
	if err := h.store.DeleteUserToken(c.Request.Context(), c.Param("id"), c.Param("token_id")); err != nil {
		respondAdminNotFound(c, err, "token not found")
		return
	}
	c.Status(http.StatusNoContent)
}

func validateUser(user *model.User) error {
	user.Normalize()
	if user.Username == "" {
		return errString("username is required")
	}
	if len(user.Username) > 64 {
		return errString("username is too long")
	}
	if len(user.DisplayName) > 80 {
		return errString("display_name is too long")
	}
	if user.Status != model.UserStatusEnabled && user.Status != model.UserStatusDisabled {
		return errString("invalid user status")
	}
	return nil
}

func validateUserToken(token *model.UserToken) error {
	token.Normalize()
	if len(token.Name) > 80 {
		return errString("token name is too long")
	}
	if token.Status != model.TokenStatusEnabled && token.Status != model.TokenStatusDisabled {
		return errString("invalid token status")
	}
	for _, item := range token.AllowIPs {
		if !validIPRule(item) {
			return errString("invalid allow_ips entry: " + item)
		}
	}
	return nil
}

func validIPRule(item string) bool {
	item = strings.TrimSpace(item)
	if item == "" {
		return true
	}
	if strings.Contains(item, "/") {
		_, _, err := net.ParseCIDR(item)
		return err == nil
	}
	return net.ParseIP(item) != nil
}

func respondAdminNotFound(c *gin.Context, err error, message string) {
	status := http.StatusInternalServerError
	if errors.Is(err, bson.ErrInvalidHex) {
		status = http.StatusBadRequest
	} else if errors.Is(err, mongo.ErrNoDocuments) {
		status = http.StatusNotFound
	}
	c.JSON(status, gin.H{"error": message})
}
