package main

import (
	"log/slog"
	"os"
	"os/signal"
	"otom-ai/bot"
	"otom-ai/config"
	"syscall"
	"time"
)

func main() {
	// Logger structuré avec format lisible et timestamps courts
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Format de timestamp plus court : HH:MM:SS
			if a.Key == slog.TimeKey {
				if t, ok := a.Value.Any().(time.Time); ok {
					a.Value = slog.StringValue(t.Format("15:04:05"))
				}
			}
			return a
		},
	}))
	slog.SetDefault(logger)

	// Chargement de la configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Error("Échec du chargement de la configuration", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("Configuration chargée avec succès")

	// Initialisation du bot avec toutes ses dépendances
	b, err := bot.New(cfg, logger)
	if err != nil {
		logger.Error("Échec de l'initialisation du bot", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("Bot initialisé avec succès")

	// Démarrage de la connexion Discord
	if err := b.Start(); err != nil {
		logger.Error("Échec de la connexion à Discord", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("✅ Bot démarré — en attente des messages...")

	// Arrêt gracieux : attente d'un signal SIGINT (Ctrl+C) ou SIGTERM
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	logger.Info("⏹️ Signal d'arrêt reçu, déconnexion en cours...")
	if err := b.Stop(); err != nil {
		logger.Error("Erreur lors de la fermeture", slog.String("error", err.Error()))
	}
	logger.Info("👋 Bot déconnecté proprement. À la prochaine au Zaap!")
}
