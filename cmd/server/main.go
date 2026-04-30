package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/982945902/hermes/internal/api"
	"github.com/982945902/hermes/internal/auth"
	"github.com/982945902/hermes/internal/cache"
	"github.com/982945902/hermes/internal/config"
	"github.com/982945902/hermes/internal/health"
	"github.com/982945902/hermes/internal/identity"
	"github.com/982945902/hermes/internal/provider"
	"github.com/982945902/hermes/internal/relay"
	"github.com/982945902/hermes/internal/server"
	"github.com/982945902/hermes/internal/store"
)

func main() {
	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := store.Connect(ctx, cfg.MongoURI, cfg.MongoDatabase)
	if err != nil {
		log.Fatalf("connect mongo: %v", err)
	}
	defer db.Close(context.Background())
	if err := db.EnsureIndexes(context.Background()); err != nil {
		log.Fatalf("ensure indexes: %v", err)
	}
	channelCache := cache.NewChannelCache(db, cfg.ChannelSyncEvery)
	if err := channelCache.Load(context.Background()); err != nil {
		log.Fatalf("load channel cache: %v", err)
	}
	channelCache.Start(context.Background())

	httpClient := &http.Client{Timeout: cfg.UpstreamTimeout}
	provider := provider.NewOpenAICompatible(httpClient)
	meter := health.NewMeter(cfg.RoutingStrategy)
	guard := identity.NewGuard(cfg.IdentityName, cfg.IdentityEnabled)
	authService := auth.New(cfg)
	adminHandler := api.NewAdminHandler(db, channelCache, authService, provider, meter)
	relayHandler := relay.NewHandler(channelCache, provider, meter, guard)
	router := server.NewRouter(cfg, authService, adminHandler, relayHandler, db)

	log.Printf("hermes listening on :%s", cfg.Port)
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}
