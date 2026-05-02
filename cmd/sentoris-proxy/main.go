package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	httpHandler "github.com/sentoris-ai/sentoris-proxy/internal/transport/http"
	"github.com/sentoris-ai/sentoris-proxy/internal/transport/http/middleware"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/audit"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/deprecation"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/governance"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/security"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/hooks"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/extensions"
	"github.com/sentoris-ai/sentoris-proxy/internal/config"
	"github.com/sentoris-ai/sentoris-proxy/internal/adapter/storage"
	"github.com/sentoris-ai/sentoris-proxy/internal/service/router"
	"github.com/sentoris-ai/sentoris-proxy/pkg/otel"
	"github.com/sentoris-ai/sentoris-proxy/pkg/logger"
	"github.com/sentoris-ai/sentoris-proxy/pkg/metrics"
	"github.com/sentoris-ai/sentoris-proxy/pkg/migrate"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	Version    string = "development"
	BuildTime  string
	CommitHash string
)

func init() {
	if Version == "development" {
		Version = "v0.0.0-dev"
	}
}

func main() {
	port := flag.Int("port", 8080, "Port to listen on")
	configPath := flag.String("config", config.GetConfigPath(), "Path to config file")
	storageMode := flag.String("storage", "", "Storage mode: postgres_redis, sqlite, or memory")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Printf("Warning: Failed to load config file: %v, using defaults", err)
		cfg = config.DefaultConfig()
	}

	if *storageMode != "" {
		cfg.StorageMode = *storageMode
	}

	logger.InitLogger("info")
	logger.Info("Sentoris Proxy starting", "version", Version, "commit", CommitHash, "build_time", BuildTime)

	deprecationManager := deprecation.GetDeprecationManager()
	deprecationManager.InitializeDefaultNotices()

	metricsRegistry := metrics.GetRegistry()

	shutdownOtel, err := otel.InitOpenTelemetry("sentoris-proxy", "")
	if err != nil {
		logger.Warn("Failed to initialize OpenTelemetry", "error", err)
	}
	defer func() {
		if shutdownOtel != nil {
			shutdownOtel()
		}
	}()

	go func() {
		http.Handle("/metrics", promhttp.HandlerFor(metricsRegistry, promhttp.HandlerOpts{}))
		logger.Info("Metrics server starting on :9090")
		if err := http.ListenAndServe(":9090", nil); err != nil {
			logger.Error("Metrics server error", "error", err)
		}
	}()

	var budgetStore storage.BudgetStore
	var traceStore storage.TraceStore
	var riskReportStore storage.RiskReportStore
	var apiKeyStore storage.APIKeyStore

	keyManager := security.NewKeyManager(cfg.JWT.Secret)

	modelRouter := router.NewModelRouter()

	for name, providerConfig := range cfg.Upstreams {
		modelRouter.AddProvider(name, providerConfig)
		logger.Info("Loaded provider", "name", name)
	}
	if cfg.DefaultProvider != "" {
		modelRouter.SetDefaultProvider(cfg.DefaultProvider)
		logger.Info("Set default provider", "provider", cfg.DefaultProvider)
	}

	switch cfg.StorageMode {
	case "postgres_redis":
		logger.Info("Using PostgreSQL + Redis storage mode")
		budgetStore = storage.NewRedisBudgetStore(
			cfg.Redis.Addr,
			cfg.Redis.Password,
			cfg.Redis.DB,
			cfg.Redis.Mode,
			cfg.Redis.Master,
			cfg.Redis.Nodes,
		)
		if _, err := budgetStore.GetRemaining(context.Background(), "test"); err != nil {
			logger.Warn("Failed to connect to Redis, falling back to memory storage", "error", err)
			budgetStore = storage.NewMemoryBudgetStore()
		} else {
			logger.Info("Connected to Redis", "mode", cfg.Redis.Mode)
		}

		if err := migrate.RunMigrations(cfg.PostgreSQL.DSN, "migrations"); err != nil {
			logger.Warn("Failed to run database migrations", "error", err)
		} else {
			logger.Info("Database migrations completed successfully")
		}

		traceStore, err = storage.NewPostgresTraceStore(cfg.PostgreSQL.DSN)
		if err != nil {
			logger.Warn("Failed to connect to PostgreSQL, falling back to memory storage", "error", err)
			traceStore = storage.NewMemoryTraceStore()
		} else {
			logger.Info("Connected to PostgreSQL")
		}

		riskReportStore, err = storage.NewPostgresRiskReportStore(cfg.PostgreSQL.DSN)
		if err != nil {
			logger.Warn("Failed to connect to PostgreSQL for risk report store, falling back to memory", "error", err)
			riskReportStore = storage.NewMemoryRiskReportStore()
		}

		apiKeyStore, err = storage.NewPostgresAPIKeyStore(cfg.PostgreSQL.DSN)
		if err != nil {
			logger.Warn("Failed to connect to PostgreSQL for API key store, falling back to memory", "error", err)
			apiKeyStore = storage.NewMemoryAPIKeyStore()
		}

	case "sqlite":
		logger.Info("Using SQLite storage mode")
		sqlitePath := cfg.SQLite.DatabasePath
		if sqlitePath == "" {
			sqlitePath = "./data/sentoris.db"
		}

		sqliteStore, err := storage.NewSQLiteStore(sqlitePath)
		if err != nil {
			logger.Error("Failed to initialize SQLite store", "error", err)
			panic(err)
		}
		logger.Info("Connected to SQLite database", "path", sqlitePath)

		budgetStore = &storage.SQLiteBudgetStore{SQLiteStore: sqliteStore}
		traceStore = &storage.SQLiteTraceStore{SQLiteStore: sqliteStore}
		riskReportStore = &storage.SQLiteRiskReportStore{SQLiteStore: sqliteStore}
		apiKeyStore = &storage.SQLiteAPIKeyStore{SQLiteStore: sqliteStore}

	case "memory":
		logger.Info("Using memory storage mode")
		budgetStore = storage.NewMemoryBudgetStore()
		traceStore = storage.NewMemoryTraceStore()
		riskReportStore = storage.NewMemoryRiskReportStore()
		apiKeyStore = storage.NewMemoryAPIKeyStore()

	default:
		logger.Info("Using default storage mode (PostgreSQL+Redis with fallback)")
		budgetStore = storage.NewRedisBudgetStore(
			cfg.Redis.Addr,
			cfg.Redis.Password,
			cfg.Redis.DB,
			cfg.Redis.Mode,
			cfg.Redis.Master,
			cfg.Redis.Nodes,
		)
		if _, err := budgetStore.GetRemaining(context.Background(), "test"); err != nil {
			logger.Warn("Failed to connect to Redis, using memory storage", "error", err)
			budgetStore = storage.NewMemoryBudgetStore()
		} else {
			logger.Info("Connected to Redis", "mode", cfg.Redis.Mode)
		}

		if err := migrate.RunMigrations(cfg.PostgreSQL.DSN, "migrations"); err != nil {
			logger.Warn("Failed to run database migrations", "error", err)
		} else {
			logger.Info("Database migrations completed successfully")
		}

		traceStore, err = storage.NewPostgresTraceStore(cfg.PostgreSQL.DSN)
		if err != nil {
			logger.Warn("Failed to connect to PostgreSQL, using memory storage", "error", err)
			traceStore = storage.NewMemoryTraceStore()
		} else {
			logger.Info("Connected to PostgreSQL")
		}

		riskReportStore, err = storage.NewPostgresRiskReportStore(cfg.PostgreSQL.DSN)
		if err != nil {
			logger.Warn("Failed to connect to PostgreSQL for risk report store, using memory", "error", err)
			riskReportStore = storage.NewMemoryRiskReportStore()
		}

		apiKeyStore, err = storage.NewPostgresAPIKeyStore(cfg.PostgreSQL.DSN)
		if err != nil {
			logger.Warn("Failed to connect to PostgreSQL for API key store, using memory", "error", err)
			apiKeyStore = storage.NewMemoryAPIKeyStore()
		}
	}

	apiKeys, _ := apiKeyStore.List(context.Background())
	if len(apiKeys) == 0 {
		for _, apiKey := range cfg.JWT.APIKeys {
			keyManager.AddAPIKey(apiKey)
			keyHash := storage.HashAPIKey(apiKey)
			keyPrefix := storage.GetKeyPrefix(apiKey)
			apiKeyStore.Create(context.Background(), keyHash, keyPrefix, "Default Key", "Default API key from config", nil, []string{"read", "write"})
		}
	} else {
		logger.Info("Loaded API keys from database")
	}

	signer := audit.NewSigner()
	constraintEvaluator := governance.NewConstraintEvaluator(budgetStore)

	hookRegistry := hooks.NewHookRegistry()
	hookRegistry.Register(hooks.NewNoopHook())
	hookRegistry.Register(hooks.NewPIIDetectorHook())
	hookRegistry.Register(hooks.NewRateLimiterHook())

	hookChain := hooks.NewHookChain(cfg.Hooks.Strategy)
	for _, hookConfig := range cfg.Hooks.Enabled {
		hook := hookRegistry.Get(hookConfig.Name)
		if hook != nil {
			hookChain.AddHook(hook)
			logger.Info("Added hook", "name", hook.Name(), "priority", hook.Priority())
		}
	}

	extensionRegistry := extensions.NewExtensionRegistry()

	memoryFirewallEntry := &extensions.ExtensionRegistryEntry{
		Namespace:          "sentoris.ai/v1/memory_firewall",
		Version:            "1.0.0",
		Title:              "Memory Firewall Extension",
		Status:             "active",
		Maintainer:         extensions.MaintainerInfo{Name: "Sentoris AI", ContactURI: "https://sentoris.ai"},
		SpecificationURI:   "https://sentoris.ai/spec/v1/extensions/memory-firewall",
		MinProtocolVersion: "1.0.0",
		Tags:               []string{"security"},
		HandlerClass:       "MemoryFirewallExtension",
		Handler:            extensions.NewMemoryFirewallExtension(),
	}
	if err := extensionRegistry.Register(memoryFirewallEntry); err != nil {
		logger.Warn("Failed to register memory firewall extension", "error", err)
	}

	customRuleEntry := &extensions.ExtensionRegistryEntry{
		Namespace:          "sentoris.ai/v1/custom_rule",
		Version:            "1.0.0",
		Title:              "Custom Rule Extension",
		Status:             "active",
		Maintainer:         extensions.MaintainerInfo{Name: "Sentoris AI", ContactURI: "https://sentoris.ai"},
		SpecificationURI:   "https://sentoris.ai/spec/v1/extensions/custom-rule",
		MinProtocolVersion: "1.0.0",
		Tags:               []string{"customization"},
		HandlerClass:       "CustomRuleExtension",
		Handler:            extensions.NewCustomRuleExtension(),
	}
	if err := extensionRegistry.Register(customRuleEntry); err != nil {
		logger.Warn("Failed to register custom rule extension", "error", err)
	}

	for _, extConfig := range cfg.Extensions.Registered {
		logger.Info("Registered extension", "namespace", extConfig.Namespace, "version", extConfig.Version)
	}

	h := httpHandler.NewHandler(modelRouter, signer, constraintEvaluator, traceStore, budgetStore, apiKeyStore, riskReportStore)
	h.SetHookChain(hookChain)
	h.SetExtensionRegistry(extensionRegistry)

	authMiddleware := middleware.AuthMiddleware(keyManager, apiKeyStore)

	wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		publicPaths := map[string]bool{
			"/v1/chat/completions":  true,
			"/v1/models":            true,
			"/health":               true,
			"/dashboard":            true,
			"/dashboard/index.html": true,
			"/":                     true,
		}

		if strings.HasPrefix(path, "/v1/monitor") ||
			strings.HasPrefix(path, "/v1/admin") ||
			strings.HasPrefix(path, "/v1/replay-eval") ||
			strings.HasPrefix(path, "/v1/verify") ||
			publicPaths[path] {
			h.ServeHTTP(w, r)
		} else {
			authMiddleware(h).ServeHTTP(w, r)
		}
	})

	listenPort := *port
	if cfg != nil && cfg.Port > 0 {
		listenPort = cfg.Port
	}

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", listenPort),
		Handler: wrappedHandler,
	}

	ttlCleaner := storage.NewTTLCleaner(traceStore, 1*time.Hour)
	ttlCleaner.Start(context.Background())
	defer ttlCleaner.Stop()

	go func() {
		logger.Info("Sentoris Proxy starting", "port", *port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("Failed to start server", "error", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("Shutting down server...")

	if err := extensionRegistry.Close(); err != nil {
		logger.Error("Failed to close extensions", "error", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		logger.Error("Server forced to shutdown", "error", err)
	}

	logger.Info("Server exited gracefully")
}