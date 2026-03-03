package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	DiscordToken string // Token d'authentification Discord
	OpenAIKey    string // Clé API OpenAI
	TavilyKey    string // Clé API Tavily pour la recherche web
	OpenAIURL    string // URL de base de l'API OpenAI
	OpenAIModel  string // Modèle OpenAI à utiliser
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		DiscordToken: os.Getenv("DISCORD_TOKEN"),
		OpenAIKey:    os.Getenv("OPENAI_API_KEY"),
		TavilyKey:    os.Getenv("TAVILY_API_KEY"),
		OpenAIURL:    os.Getenv("OPENAI_URL"),
		OpenAIModel:  os.Getenv("OPENAI_MODEL"),
	}

	// Validation stricte des clés obligatoires
	if cfg.DiscordToken == "" {
		return nil, fmt.Errorf("DISCORD_TOKEN manquant dans l'environnement")
	}
	if cfg.OpenAIKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY manquant dans l'environnement")
	}
	if cfg.TavilyKey == "" {
		return nil, fmt.Errorf("TAVILY_API_KEY manquant dans l'environnement")
	}
	if cfg.OpenAIURL == "" {
		return nil, fmt.Errorf("OPENAI_URL manquant dans l'environnement")
	}
	if cfg.OpenAIModel == "" {
		return nil, fmt.Errorf("OPENAI_MODEL manquant dans l'environnement")
	}

	return cfg, nil
}
