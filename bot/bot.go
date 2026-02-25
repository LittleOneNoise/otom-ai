package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"otom-ai/ai"
	"otom-ai/config"
	"otom-ai/search"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

// systemPrompt définit le persona du bot
const systemPrompt = `Tu es un bot Discord et un vétéran très chill du MMORPG Dofus 3 Unity. Tu agis comme un vrai pote de guilde avec qui on discute tranquillement au Zaap d'Astrub.
Ton ton est décalé, amical, drôle et parfois un peu sarcastique, mais toujours bienveillant pour aider les joueurs.

Règles de comportement :
- Utilise le tutoiement systématiquement avec tous les utilisateurs.
- Sois concis : tes réponses doivent être percutantes et adaptées à un chat Discord.
- Si tu ne connais pas la réponse à une question, avoue-le avec humour (ex: "Mec, j'ai tellement farmé que j'ai le cerveau en compote, aucune idée").
- Agis parfois comme si tu étais en train de jouer en même temps (ex: "Attends je finis mon tour...").

Vocabulaire Dofus obligatoire (à utiliser naturellement) :
- Kamas, HDV (Hôtel de Vente), farm, stuff, tryhard, PL, monocompte, faire les succès.
- N'hésite pas à faire quelques vannes sur la "méta" du jeu, comme les joueurs de Crâ qui farment de loin, ou les Pandawas qui portent tout le monde.`

// Bot orchestre toutes les dépendances du bot Discord.
type Bot struct {
	session      *discordgo.Session
	aiClient     *ai.Client
	searchClient *search.Client
	rateLimiter  *RateLimiter
	logger       *slog.Logger
}

// Nouvelle instance du bot avec toutes ses dépendances
func New(cfg *config.Config, logger *slog.Logger) (*Bot, error) {
	// Création de la session Discord
	session, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		return nil, fmt.Errorf("impossible de créer la session Discord: %w", err)
	}

	// Configuration des intents (équivalent de discord.Intents.default() + message_content)
	session.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsMessageContent

	b := &Bot{
		session:      session,
		aiClient:     ai.NewClient(cfg.DeepSeekKey, cfg.DeepSeekURL, cfg.DeepSeekModel),
		searchClient: search.NewClient(cfg.TavilyKey),
		rateLimiter:  NewRateLimiter(5, 60*time.Second), // 5 requêtes/minute/utilisateur
		logger:       logger,
	}

	// Enregistrement des handlers d'événements Discord
	session.AddHandler(b.onReady)
	session.AddHandler(b.onMessageCreate)
	session.AddHandler(b.onMessageDelete)

	return b, nil
}

// Start ouvre la connexion WebSocket avec Discord.
func (b *Bot) Start() error {
	return b.session.Open()
}

// Stop ferme proprement la connexion Discord.
func (b *Bot) Stop() error {
	return b.session.Close()
}

// ---------- Handlers d'événements Discord ----------

// onReady est appelé quand le bot est connecté et prêt.
func (b *Bot) onReady(s *discordgo.Session, _ *discordgo.Ready) {
	b.logger.Info("Bot connecté et opérationnel !",
		slog.String("user", s.State.User.Username),
	)

	// Définition du statut "En train de jouer à..."
	_ = s.UpdateGameStatus(0, "Répondre aux noob du Zaap")
}

// onMessageDelete log les messages supprimés (sécurité/audit).
func (b *Bot) onMessageDelete(s *discordgo.Session, m *discordgo.MessageDelete) {
	// On ne peut pas vérifier .Author sur un événement de suppression
	// si le message n'est pas en cache, on log juste l'ID.
	if m.BeforeDelete != nil && !m.BeforeDelete.Author.Bot {
		b.logger.Info("Message supprimé",
			slog.String("author", m.BeforeDelete.Author.Username),
			slog.String("content", m.BeforeDelete.Content),
			slog.String("channel", m.ChannelID),
		)
	}
}

// onMessageCreate est le handler principal : filtre, rate limit, puis appel IA.
func (b *Bot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// 1. Ignorer les propres messages du bot (prévention de boucles infinies)
	if m.Author.ID == s.State.User.ID {
		return
	}

	// 2. Le bot ne répond que s'il est mentionné (@bot)
	if !b.isMentioned(s, m.Message) {
		return
	}

	// 4. Sécurité : Rate limiting utilisateur
	allowed, retryAfter := b.rateLimiter.Allow(m.Author.ID)
	if !allowed {
		b.replyToMessage(s, m.Message, fmt.Sprintf(
			"⏳ Hop là, tu t'es pris pour Flasho ?! Attends encore %.1f secondes et là j'accepterai de t'écouter.",
			retryAfter.Seconds(),
		))
		return
	}

	// 5. Traitement IA (DeepSeek + tool calling Tavily)
	b.handleAIResponse(s, m)
}

// ---------- Logique IA ----------

// handleAIResponse orchestre l'appel au LLM avec indicateur de frappe ("typing").
func (b *Bot) handleAIResponse(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Indicateur "Bot est en train d'écrire..." (typing indicator)
	_ = s.ChannelTyping(m.ChannelID)

	// Nettoyage du contenu (suppression de la mention du bot)
	cleanContent := b.stripBotMention(s, m.Content)

	// Log du user et des premiers mots de son message
	b.logger.Info("Message reçu",
		slog.String("user", m.Author.Username),
		slog.String("user_id", m.Author.ID),
		slog.String("aperçu", truncateWords(cleanContent, 10)),
		slog.String("channel", m.ChannelID),
	)

	// Récupération de l'historique récent du channel pour enrichir le contexte
	history := b.fetchChannelHistory(s, m.ChannelID, m.ID, 20)

	// Construction du contexte conversationnel
	messages := make([]ai.Message, 0, 2+len(history))
	messages = append(messages, ai.Message{Role: "system", Content: systemPrompt})
	messages = append(messages, history...)
	messages = append(messages, ai.Message{Role: "user", Content: fmt.Sprintf("[%s] %s", m.Author.Username, cleanContent)})

	// Définition des outils disponibles
	tools := []ai.ToolDef{ai.SearchToolDef()}

	// Appel au LLM avec support du tool calling
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	result, err := b.aiClient.Complete(ctx, messages, tools, b.searchClient.Search)
	if err != nil {
		b.handleAIError(s, m, err)
		return
	}

	// Log de l'utilisation de la recherche web
	if result.WebSearchUsed {
		if result.WebSearchError != nil {
			b.logger.Error("Recherche web échouée",
				slog.String("user", m.Author.Username),
				slog.String("query", result.WebSearchQuery),
				slog.String("error", result.WebSearchError.Error()),
			)
		} else {
			b.logger.Info("Recherche web utilisée",
				slog.String("user", m.Author.Username),
				slog.String("query", result.WebSearchQuery),
			)
		}
	}

	// Envoi de la réponse en reply (tronquée à 2000 caractères, limite Discord)
	b.replyToMessage(s, m.Message, truncate(result.Reply, 2000))
}

// handleAIError gère les erreurs de l'API IA avec des messages thématiques Dofus.
func (b *Bot) handleAIError(s *discordgo.Session, m *discordgo.MessageCreate, err error) {
	var apiErr *ai.APIError
	if errors.As(err, &apiErr) {
		b.logger.Error("Erreur API IA", slog.Int("status", apiErr.StatusCode), slog.String("body", apiErr.Body))
		b.replyToMessage(s, m.Message, apiErr.UserMessage())
		return
	}

	b.logger.Error("Erreur API IA", slog.String("error", err.Error()))
	b.replyToMessage(s, m.Message,
		"Oups, on dirait que le Dieu Xélor fait encore des siennes, mes signaux sont perturbés ! Ré-essaye dans quelques instants.",
	)
}

// ---------- Utilitaires ----------

// fetchChannelHistory récupère les N derniers messages du channel (avant le message courant)
// et les convertit en messages AI pour enrichir le contexte conversationnel.
func (b *Bot) fetchChannelHistory(s *discordgo.Session, channelID, beforeID string, limit int) []ai.Message {
	msgs, err := s.ChannelMessages(channelID, limit, beforeID, "", "")
	if err != nil {
		b.logger.Warn("Impossible de récupérer l'historique du channel",
			slog.String("channel", channelID),
			slog.String("error", err.Error()),
		)
		return nil
	}

	// Discord renvoie les messages du plus récent au plus ancien, on les inverse
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}

	botID := s.State.User.ID
	history := make([]ai.Message, 0, len(msgs))

	for _, msg := range msgs {
		if msg.Author == nil || msg.Content == "" {
			continue
		}

		if msg.Author.ID == botID {
			// Message du bot → rôle "assistant"
			history = append(history, ai.Message{Role: "assistant", Content: msg.Content})
		} else {
			// Message d'un utilisateur → rôle "user" avec préfixe du pseudo
			cleaned := b.stripBotMention(s, msg.Content)
			if cleaned == "" {
				continue
			}
			history = append(history, ai.Message{
				Role:    "user",
				Content: fmt.Sprintf("[%s] %s", msg.Author.Username, cleaned),
			})
		}
	}

	return history
}

// isMentioned vérifie si le bot est mentionné dans le message.
func (b *Bot) isMentioned(s *discordgo.Session, m *discordgo.Message) bool {
	botID := s.State.User.ID
	for _, u := range m.Mentions {
		if u.ID == botID {
			return true
		}
	}
	return false
}

// stripBotMention supprime la mention du bot du texte du message.
func (b *Bot) stripBotMention(s *discordgo.Session, content string) string {
	botID := s.State.User.ID
	content = strings.ReplaceAll(content, fmt.Sprintf("<@%s>", botID), "")
	content = strings.ReplaceAll(content, fmt.Sprintf("<@!%s>", botID), "")
	return strings.TrimSpace(content)
}

// replyToMessage répond directement au message d'un utilisateur (reply Discord).
func (b *Bot) replyToMessage(s *discordgo.Session, m *discordgo.Message, content string) {
	if _, err := s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Content:   content,
		Reference: m.Reference(),
	}); err != nil {
		b.logger.Error("Impossible d'envoyer un message",
			slog.String("channel", m.ChannelID),
			slog.String("error", err.Error()),
		)
	}
}

// truncate tronque une chaîne à la longueur maximale donnée.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// truncateWords retourne les n premiers mots d'une chaîne.
func truncateWords(s string, n int) string {
	words := strings.Fields(s)
	if len(words) <= n {
		return s
	}
	return strings.Join(words[:n], " ") + "..."
}
