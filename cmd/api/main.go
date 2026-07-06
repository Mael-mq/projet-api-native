package main

import (
	"log/slog"
	"os"

	"github.com/mmarquet/native-api/internal/config"
	"github.com/mmarquet/native-api/internal/docker"
	"github.com/mmarquet/native-api/internal/service"
	"github.com/mmarquet/native-api/internal/storage"
	httptransport "github.com/mmarquet/native-api/internal/transport/http"
)

func main() {
	cfg := config.Load()
	setupLogger(cfg.LogLevel)

	db, err := storage.Open(cfg.DBPath)
	if err != nil {
		slog.Error("ouverture base de données", "err", err)
		os.Exit(1)
	}

	manager := docker.NewManager(cfg.DockerHost)

	projects := service.NewProjectService(db)
	servers := service.NewServerService(db, manager)
	deployments := service.NewDeploymentService(db, manager, projects)

	if _, err := servers.EnsureLocal(); err != nil {
		slog.Error("enregistrement du serveur local", "err", err)
		os.Exit(1)
	}

	handlers := &httptransport.Handlers{
		Projects:    projects,
		Servers:     servers,
		Deployments: deployments,
	}
	app := httptransport.NewApp(handlers, cfg.APIKey)

	slog.Info("démarrage de l'API", "port", cfg.Port)
	if err := app.Listen(":" + cfg.Port); err != nil {
		slog.Error("arrêt du serveur", "err", err)
		os.Exit(1)
	}
}

func setupLogger(level string) {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})))
}
