// Package search implémente le client de recherche web via l'API Tavily.
// Il est utilisé comme outil (tool calling) par le LLM pour obtenir des infos récentes.
package search

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Client encapsule la connexion à l'API Tavily.
type Client struct {
	apiKey     string
	httpClient *http.Client
}

// tavilyRequest représente le payload envoyé à l'API Tavily.
type tavilyRequest struct {
	APIKey        string `json:"api_key"`
	Query         string `json:"query"`
	SearchDepth   string `json:"search_depth"`
	IncludeAnswer bool   `json:"include_answer"`
}

// tavilyResult représente un résultat individuel de recherche.
type tavilyResult struct {
	Title   string `json:"title"`
	Content string `json:"content"`
	URL     string `json:"url"`
}

// tavilyResponse représente la réponse complète de l'API Tavily.
type tavilyResponse struct {
	Results []tavilyResult `json:"results"`
}

// NewClient crée un nouveau client Tavily avec un timeout HTTP de 5 secondes.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Search effectue une recherche web et retourne les 3 premiers résultats concaténés.
// En cas d'erreur, retourne un message de fallback pour que le LLM réponde sans données web.
func (c *Client) Search(ctx context.Context, query string) string {
	reqBody := tavilyRequest{
		APIKey:        c.apiKey,
		Query:         query,
		SearchDepth:   "basic",
		IncludeAnswer: false,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return fallbackMessage()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.tavily.com/search", bytes.NewReader(body))
	if err != nil {
		return fallbackMessage()
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fallbackMessage()
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fallbackMessage()
	}

	var tavilyResp tavilyResponse
	if err := json.NewDecoder(resp.Body).Decode(&tavilyResp); err != nil {
		return fallbackMessage()
	}

	// Concaténation des 3 premiers snippets
	var snippets []string
	limit := min(3, len(tavilyResp.Results))
	for i := range limit {
		r := tavilyResp.Results[i]
		snippets = append(snippets, fmt.Sprintf("- %s: %s", r.Title, r.Content))
	}

	if len(snippets) == 0 {
		return "Aucune information récente trouvée sur le web."
	}
	return strings.Join(snippets, "\n")
}

// fallbackMessage retourne l'instruction de secours quand la recherche échoue.
func fallbackMessage() string {
	return "ERREUR_OUTIL: La recherche internet a échoué et est indisponible. " +
		"Tu dois absolument répondre à la question de l'utilisateur en utilisant uniquement " +
		"tes connaissances internes. Précise-lui, en faisant un clin d'oeil à l'instabilité des serveurs Ankama, que ton accès internet " +
		"bug un peu mais donne quand même ta meilleure réponse."
}
