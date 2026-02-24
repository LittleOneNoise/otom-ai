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
	"strings"
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
		"required": ["query"]
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

// Complete envoie une requête de complétion au LLM et retourne la réponse textuelle.
// Si le LLM demande un outil, la fonction searchFn est appelée et un second appel est fait.
func (c *Client) Complete(ctx context.Context, messages []Message, tools []ToolDef, searchFn func(ctx context.Context, query string) string) (string, error) {
	// --- Premier appel ---
	resp, err := c.call(ctx, messages, tools)
	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("réponse vide du LLM")
	}
	msg := resp.Choices[0].Message

	// --- Détection du tool calling ---
	if len(msg.ToolCalls) > 0 && searchFn != nil {
		tc := msg.ToolCalls[0]
		if tc.Function.Name == "search_internet" {
			var args SearchArgs
			if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
				return "", fmt.Errorf("arguments outil invalides: %w", err)
			}

			// Exécution de la recherche
			result := searchFn(ctx, args.Query)

			// Ajout du contexte outil dans l'historique
			messages = append(messages,
				Message{Role: "assistant", ToolCalls: msg.ToolCalls},
				Message{Role: "tool", ToolCallID: tc.ID, Content: result},
			)

			// --- Second appel avec les résultats de recherche (sans outils) ---
			resp, err = c.call(ctx, messages, nil)
			if err != nil {
				return "", err
			}
			if len(resp.Choices) == 0 {
				return "", fmt.Errorf("réponse vide du LLM (second appel)")
			}
			msg = resp.Choices[0].Message
		}
	}

	return msg.Content, nil
}

// call effectue un appel HTTP brut à l'API DeepSeek.
func (c *Client) call(ctx context.Context, messages []Message, tools []ToolDef) (*chatResponse, error) {
	reqBody := chatRequest{
		Model:       c.model,
		Messages:    messages,
		Tools:       tools,
		Temperature: 1.3, // Entre 0.0 et 1.5, plus c'est élevé, plus les réponses sont créatives (et potentiellement incohérentes)
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

	// Détection des erreurs HTTP spécifiques (402 = quota, 429 = rate limit)
	if resp.StatusCode != http.StatusOK {
		code := fmt.Sprintf("%d", resp.StatusCode)
		bodyStr := string(respBody)
		if strings.Contains(code, "402") || strings.Contains(code, "429") ||
			strings.Contains(strings.ToLower(bodyStr), "insufficient_quota") {
			return nil, &QuotaError{StatusCode: resp.StatusCode, Body: bodyStr}
		}
		return nil, fmt.Errorf("erreur API (HTTP %d): %s", resp.StatusCode, bodyStr)
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("erreur de décodage: %w", err)
	}

	return &chatResp, nil
}

// ---------- Erreurs typées ----------

// QuotaError est retournée quand le quota API ou le rate limit est atteint.
type QuotaError struct {
	StatusCode int
	Body       string
}

func (e *QuotaError) Error() string {
	return fmt.Sprintf("quota/rate limit atteint (HTTP %d): %s", e.StatusCode, e.Body)
}
