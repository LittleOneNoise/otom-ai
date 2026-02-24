package config

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	DiscordToken  string // Token d'authentification Discord
	DeepSeekKey   string // Clé API DeepSeek (compatible OpenAI)
	TavilyKey     string // Clé API Tavily pour la recherche web
	DeepSeekURL   string // URL de base de l'API DeepSeek
	DeepSeekModel string // Modèle DeepSeek à utiliser
}

func Load() (*Config, error) {
	_ = godotenv.Load()

	cfg := &Config{
		DiscordToken:  os.Getenv("DISCORD_TOKEN"),
		DeepSeekKey:   os.Getenv("DEEPSEEK_API_KEY"),
		TavilyKey:     os.Getenv("TAVILY_API_KEY"),
		DeepSeekURL:   os.Getenv("DEEPSEEK_URL"),
		DeepSeekModel: os.Getenv("DEEPSEEK_MODEL"),
	}

	// Validation stricte des clés obligatoires
	if cfg.DiscordToken == "" {
		return nil, fmt.Errorf("DISCORD_TOKEN manquant dans l'environnement")
	}
	if cfg.DeepSeekKey == "" {
		return nil, fmt.Errorf("DEEPSEEK_API_KEY manquant dans l'environnement")
	}
	if cfg.TavilyKey == "" {
		return nil, fmt.Errorf("TAVILY_API_KEY manquant dans l'environnement")
	}
	if cfg.DeepSeekURL == "" {
		return nil, fmt.Errorf("DEEPSEEK_URL manquant dans l'environnement")
	}
	if cfg.DeepSeekModel == "" {
		return nil, fmt.Errorf("DEEPSEEK_MODEL manquant dans l'environnement")
	}

	return cfg, nil
}
