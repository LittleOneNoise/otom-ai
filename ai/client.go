// Package ai implémente le client OpenAI avec support du tool calling.
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
	Parameters  json.RawMessage `json:"parameters"`
}

// chatRequest est le payload envoyé à l'API OpenAI.
type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
}

// chatResponse est la réponse de l'API OpenAI.
type chatResponse struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
	Usage *Usage `json:"usage,omitempty"`
	Error *struct {
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

// Usage contient les statistiques de tokens retournées par l'API OpenAI.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// SearchArgs contient les arguments parsés de l'outil search_internet.
type SearchArgs struct {
	Query string `json:"query"`
}

// ---------- Client ----------

// Client encapsule la connexion à l'API OpenAI.
type Client struct {
	apiKey     string
	baseURL    string
	model      string
	httpClient *http.Client
}

// NewClient crée un nouveau client OpenAI avec les paramètres donnés.
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
			Parameters:  params,
		},
	}
}

// Prix gpt-5-mini au 03/03/2026 (USD par token).
const (
	pricePerInputToken  = 0.00000025 // $0.25 / 1M tokens
	pricePerOutputToken = 0.00000200 // $2.00 / 1M tokens
)

// CompletionResult contient le résultat d'une complétion LLM avec métadonnées.
type CompletionResult struct {
	Reply            string  // Réponse textuelle du LLM
	WebSearchUsed    bool    // true si le LLM a déclenché une recherche web
	WebSearchError   error   // non-nil si la recherche web a échoué
	WebSearchQuery   string  // Requête de recherche utilisée (si applicable)
	PromptTokens     int     // Nombre de tokens en entrée (cumulé si tool calling)
	CompletionTokens int     // Nombre de tokens en sortie (cumulé si tool calling)
	TotalTokens      int     // Total des tokens consommés
	EstimatedCost    float64 // Coût estimé en USD
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
	accumulateUsage(result, resp.Usage)
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
			accumulateUsage(result, resp.Usage)
			msg = resp.Choices[0].Message
		}
	}

	// Calcul du coût estimé
	result.EstimatedCost = float64(result.PromptTokens)*pricePerInputToken +
		float64(result.CompletionTokens)*pricePerOutputToken

	result.Reply = msg.Content
	return result, nil
}

// accumulateUsage cumule les tokens de chaque appel API dans le résultat.
func accumulateUsage(r *CompletionResult, u *Usage) {
	if u == nil {
		return
	}
	r.PromptTokens += u.PromptTokens
	r.CompletionTokens += u.CompletionTokens
	r.TotalTokens += u.TotalTokens
}

// call effectue un appel HTTP brut à l'API OpenAI.
func (c *Client) call(ctx context.Context, messages []Message, tools []ToolDef) (*chatResponse, error) {
	reqBody := chatRequest{
		Model:       c.model,
		Messages:    messages,
		Tools:       tools,
		Temperature: 0.2, // Entre 0.0 et 1.5, plus c'est élevé, plus les réponses sont créatives (et potentiellement incohérentes)
		MaxTokens:   800, // ~2000 caractères, suffisant pour les messages Discord
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

// APIError est retournée lors d'une erreur HTTP de l'API OpenAI.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("erreur API (HTTP %d): %s", e.StatusCode, e.Body)
}

// UserMessage retourne un message utilisateur adapté au code d'erreur OpenAI.
func (e *APIError) UserMessage() string {
	switch e.StatusCode {
	case 400:
		return "💨 Un simple courant d'air ? Le grand silence de la Shukrute ?\nTon message est complètement vide ou mal formé ! Envoie-moi quelques mots, je ne maîtrise pas encore la télépathie. (Erreur 400)"
	case 401:
		return "❌🛡️❌ Oulah ça sent le porkass grillé, la milice m'a refoulé l'accès !\n Mon créateur doit corriger ma clé API pour que je puisse te répondre. (Erreur 401)"
	case 403:
		return "❌🚫❌ Accès interdit ! On dirait que mon créateur essaie de m'invoquer depuis une zone non autorisée par OpenAI. Pas de bol ! (Erreur 403)"
	case 429:
		return "❌⚡❌ Oula, soit tes Tofus messagers sont sur les rotules (trop de requêtes), soit ma bourse d'Enutrof sonne creux (quota épuisé) ! Attends un instant ou préviens mon créateur. (Erreur 429)"
	case 500:
		return "❌💥❌ Aïe... Une de mes tourelles Steamer vient de surchauffer en coulisses chez OpenAI. Mes technomages sont sur le coup, reviens dans un petit instant. (Erreur 500)"
	case 503:
		return "❌⏳❌ Embouteillage monstre au Zaap d'Astrub ! Les serveurs OpenAI sont surchargés. Prends une petite limonade et ré-essaye dans quelques minutes. (Erreur 503)"
	default:
		return "❌ Oups, on dirait que Dieu Xélor fait encore des siennes, mes signaux sont perturbés ! Ré-essaye dans quelques instants. (Erreur inconnue)"
	}
}
