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
		b.sendMessage(s, m.ChannelID, fmt.Sprintf(
			"⏳ Hop là <@%s>, tu t'es pris pour Flasho ?! Attends encore %.1f secondes et là j'accepterai de t'écouter.",
			m.Author.ID, retryAfter.Seconds(),
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

	// Construction du contexte conversationnel
	messages := []ai.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: cleanContent},
	}

	// Définition des outils disponibles
	tools := []ai.ToolDef{ai.SearchToolDef()}

	// Appel au LLM avec support du tool calling
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	reply, err := b.aiClient.Complete(ctx, messages, tools, b.searchClient.Search)
	if err != nil {
		b.handleAIError(s, m, err)
		return
	}

	// Envoi de la réponse (tronquée à 2000 caractères, limite Discord)
	b.sendMessage(s, m.ChannelID, truncate(reply, 2000))
}

// handleAIError gère les erreurs de l'API IA avec des messages thématiques Dofus.
func (b *Bot) handleAIError(s *discordgo.Session, m *discordgo.MessageCreate, err error) {
	var quotaErr *ai.QuotaError
	if errors.As(err, &quotaErr) {
		msg := fmt.Sprintf(
			"🪙 **Aïe, panne de Kamas!**\n"+
				"Désolé <@%s>, mon créateur -- étant un gros rat -- n'a pas suffisamment donné d'argent pour payer l'API (Rate Limit API atteint).\n"+
				"Je serai de nouveau opérationnel plus tard.",
			m.Author.ID,
		)
		b.sendMessage(s, m.ChannelID, msg)
		return
	}

	b.logger.Error("Erreur API IA", slog.String("error", err.Error()))
	b.sendMessage(s, m.ChannelID,
		"Oups, on dirait que le Dieu Xélor fait encore des siennes, mes signaux sont perturbés ! Ré-essaye dans quelques instants.",
	)
}

// ---------- Utilitaires ----------

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

// sendMessage envoie un message dans un salon Discord avec gestion d'erreur.
func (b *Bot) sendMessage(s *discordgo.Session, channelID, content string) {
	if _, err := s.ChannelMessageSend(channelID, content); err != nil {
		b.logger.Error("Impossible d'envoyer un message",
			slog.String("channel", channelID),
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
