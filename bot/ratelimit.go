package bot

import (
	"sync"
	"time"
)

// RateLimiter implémente un rate limiter applicatif par utilisateur
// avec une fenêtre glissante (sliding window).
// Équivalent Go du CooldownMapping de discord.py.
type RateLimiter struct {
	mu       sync.Mutex
	limit    int           // Nombre max de requêtes par fenêtre
	window   time.Duration // Durée de la fenêtre
	requests map[string][]time.Time
}

// NewRateLimiter crée un rate limiter avec les paramètres donnés.
// Exemple : NewRateLimiter(5, 60*time.Second) → 5 requêtes/minute/utilisateur.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		limit:    limit,
		window:   window,
		requests: make(map[string][]time.Time),
	}
}

// Allow vérifie si l'utilisateur peut effectuer une requête.
// Retourne (true, 0) si autorisé, ou (false, retryAfter) si limité.
func (rl *RateLimiter) Allow(userID string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Nettoyage des entrées expirées (hors fenêtre)
	timestamps := rl.requests[userID]
	valid := timestamps[:0] // Réutilisation du slice sous-jacent
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}

	// Vérification de la limite
	if len(valid) >= rl.limit {
		retryAfter := valid[0].Add(rl.window).Sub(now)
		rl.requests[userID] = valid
		return false, retryAfter
	}

	// Autorisation et enregistrement du timestamp
	rl.requests[userID] = append(valid, now)
	return true, 0
}
