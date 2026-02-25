// Package ai implémente le client DeepSeek (compatible OpenAI) avec support du tool calling.
// Il gère le cycle complet : appel initial → détection d'outil → exécution → appel final.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ---------- Types de requête/réponse (format OpenAI) ----------

// Message représente un message dans le contexte conversationnel du LLM.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall représente un appel d'outil demandé par le LLM.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall contient le nom et les arguments d'une fonction appelée par le LLM.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON stringifié
}

// ToolDef définit un outil disponible pour le LLM (format OpenAI function calling).
type ToolDef struct {
	Type     string         `json:"type"`
	Function FunctionSchema `json:"function"`
}

// FunctionSchema décrit la signature d'une fonction-outil.
type FunctionSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Strict      bool            `json:"strict,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

// chatRequest est le payload envoyé à l'API DeepSeek.
type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	Temperature float64   `json:"temperature"`
}

// chatResponse est la réponse de l'API DeepSeek.
type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

// SearchArgs contient les arguments parsés de l'outil search_internet.
type SearchArgs struct {
	Query string `json:"query"`
}

// ---------- Client ----------

// Client encapsule la connexion à l'API DeepSeek.
type Client struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// NewClient crée un nouveau client DeepSeek avec les paramètres donnés.
func NewClient(apiKey, baseURL, model string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // Timeout généreux pour les réponses LLM
		},
	}
}

// SearchToolDef retourne la définition de l'outil de recherche web
// au format OpenAI function calling.
func SearchToolDef() ToolDef {
	params := json.RawMessage(`{
		"type": "object",
		"properties": {
			"query": {
				"type": "string",
				"description": "La requête de recherche web à effectuer pour trouver des informations récentes sur Dofus 3 Unity ou tout autre sujet."
			}
		},
		"required": ["query"],
		"additionalProperties": false
	}`)

	return ToolDef{
		Type: "function",
		Function: FunctionSchema{
			Name:        "search_internet",
			Description: "Recherche des informations récentes sur internet. Utilise cet outil quand tu as besoin d'informations actualisées, de news, ou de données que tu ne possèdes pas.",
			Strict:      true,
			Parameters:  params,
		},
	}
}

// CompletionResult contient le résultat d'une complétion LLM avec métadonnées.
type CompletionResult struct {
	Reply          string // Réponse textuelle du LLM
	WebSearchUsed  bool   // true si le LLM a déclenché une recherche web
	WebSearchError error  // non-nil si la recherche web a échoué
	WebSearchQuery string // Requête de recherche utilisée (si applicable)
}

// Complete envoie une requête de complétion au LLM et retourne le résultat avec métadonnées.
// Si le LLM demande un outil, la fonction searchFn est appelée et un second appel est fait.
func (c *Client) Complete(ctx context.Context, messages []Message, tools []ToolDef, searchFn func(ctx context.Context, query string) (string, error)) (*CompletionResult, error) {
	result := &CompletionResult{}

	// --- Premier appel ---
	resp, err := c.call(ctx, messages, tools)
	if err != nil {
		return nil, err
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("réponse vide du LLM")
	}
	msg := resp.Choices[0].Message

	// --- Détection du tool calling ---
	if len(msg.ToolCalls) > 0 && searchFn != nil {
		tc := msg.ToolCalls[0]
		if tc.Function.Name == "search_internet" {
			result.WebSearchUsed = true

			var args SearchArgs
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				return nil, fmt.Errorf("arguments outil invalides: %w", err)
			}
			result.WebSearchQuery = args.Query

			// Exécution de la recherche
			searchResult, searchErr := searchFn(ctx, args.Query)
			result.WebSearchError = searchErr

			// Ajout du contexte outil dans l'historique (même en cas d'erreur, le fallback est passé)
			messages = append(messages,
				Message{Role: "assistant", ToolCalls: msg.ToolCalls},
				Message{Role: "tool", ToolCallID: tc.ID, Content: searchResult},
			)

			// --- Second appel avec les résultats de recherche (sans outils) ---
			resp, err = c.call(ctx, messages, nil)
			if err != nil {
				return nil, err
			}
			if len(resp.Choices) == 0 {
				return nil, fmt.Errorf("réponse vide du LLM (second appel)")
			}
			msg = resp.Choices[0].Message
		}
	}

	result.Reply = msg.Content
	return result, nil
}

// call effectue un appel HTTP brut à l'API DeepSeek.
func (c *Client) call(ctx context.Context, messages []Message, tools []ToolDef) (*chatResponse, error) {
	reqBody := chatRequest{
		Model:       c.model,
		Messages:    messages,
		Tools:       tools,
		Temperature: 0.2, // Entre 0.0 et 1.5, plus c'est élevé, plus les réponses sont créatives (et potentiellement incohérentes)
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("erreur de sérialisation: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("erreur de création de requête: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("erreur réseau: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("erreur de lecture: %w", err)
	}

	// Détection des erreurs HTTP avec messages personnalisés par code
	if resp.StatusCode != http.StatusOK {
		bodyStr := string(respBody)
		return nil, &APIError{StatusCode: resp.StatusCode, Body: bodyStr}
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("erreur de décodage: %w", err)
	}

	return &chatResp, nil
}

// ---------- Erreurs typées ----------

// APIError est retournée lors d'une erreur HTTP de l'API DeepSeek.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("erreur API (HTTP %d): %s", e.StatusCode, e.Body)
}

// UserMessage retourne un message utilisateur adapté au code d'erreur DeepSeek.
func (e *APIError) UserMessage() string {
	switch e.StatusCode {
	case 400:
		return "💨 Un simple courant d'air ? Le grand silence de la Shukrute ?\nTon message est complètement vide ! Envoie-moi quelques mots, je ne maîtrise pas encore la télépathie. (Erreur 400)"
	case 401:
		return "❌🛡️❌ Oulah ça sent le porkass grillé, la milice m'a refoulé l'accès !\n Mon créateur doit corriger mes accès pour que je puisse te répondre. (Erreur 401)"
	case 402:
		return "❌🪙❌ Par la sainte barbe du Dieu Enutrof, on dirait bien que ma bourse sonne creux !\n Mon créateur doit ré-injecter des Kamas pour que je puisse continuer à t'aider. (Erreur 402)"
	case 422:
		return "❌⚙️❌ Oups... Le cadran de mon Xélor interne s'est emmêlé les aiguilles, ou alors l'alchimie est mauvaise. Ma configuration actuelle m'empêche de te répondre correctement.\n Mon créateur doit revoir la configuration de mon modèle ou de mes outils. (Erreur 422)"
	case 429:
		return "❌⚡❌ Oula, tes Tofus messagers sont sur les rotules ! Tu spam comme un fou. Laisse-leur le temps de picorer quelques graines et ré-essaye dans un instant. (Erreur 429)"
	case 500:
		return "❌💥❌ Aïe... Une de mes tourelles Steamer vient de surchauffer en coulisses. C'est de ma faute ! Mes technomages sont sur le coup pour réparer les rouages, reviens me voir dans un petit instant. (Erreur 500)"
	case 503:
		return "❌⏳❌ Embouteillage monstre au Zaap d'Astrub ! Il y a beaucoup trop de monde qui me parle en même temps et mes circuits débordent. Prends une petite limonade et ré-essaye dans quelques minutes. (Erreur 503)"
	default:
		return "❌ Oups, on dirait que Dieu Xélor fait encore des siennes, mes signaux sont perturbés ! Ré-essaye dans quelques instants. (Erreur inconnue)"

	}
}
