# Otom-AI

<img src="./assets/mascotte.png" alt="Otom-AI Mascotte" width="200" height="200">

<i>Image générée via Gemini</i>

## Description
> Chatbot utilisable sur un serveur Discord.<br/>
Le but principal est d'impersonifié un joueur expert de Dofus mais qui reste chill, les pieds sur Terre et qui se veut blagueur et pote avec tout le monde.

> Sous le capot on utilise le modèle **gpt-5-mini** d'OpenAI avec qui on communique via l'API Chat Completions. Le modèle est rapide, peu coûteux et bien adapté à des réponses courtes de chat Discord.<br/>
Toutefois le modèle à lui seul ne suffit pas pour des sujets récents ou précis mais on peut contourner ce problème via des recherches web en temps réel pour enrichir les données. J'utilise le service Tavily qui autorise 1000 requêtes gratuites.

## 1. Télécharger dépendances
```sh
go mod tidy   # télécharge les dépendances
```

## 2. Configurer l'environnement
Dans un fichier `.env`
```yaml
# Identifiants Discord
DISCORD_TOKEN=(Portal Discord > section "Bot" → Token → "Reset Token")

# Identifiants IA (OpenAI)
OPENAI_API_KEY=
OPENAI_URL=https://api.openai.com/v1/chat/completions
OPENAI_MODEL=gpt-5-mini

# Identifiants Recherche Web (Tavily)
TAVILY_API_KEY=
```

## 3. Compiler et exécuter
```sh
go build .     # Compile l'exécutable
go run .       # Compile et lance directement le bot
```

## 🤖 Ajouter le bot à un serveur
1. Aller sur [Portail Developper Discord](https://discord.com/developers/applications)
2. Sélectionner (ou créer) l'appli, puis onglet OAuth2 > URL Generator
3. Dans la section Scopes, cocher : "bot"
4. Puis en dessous dans Bot Permissions, sélectionner :
    - General Permissions :
        - View Channels
    - Text Permissions :
        - Send Messages
        - Read Message History
5. Copier l'URL générée en bas de page et la coller dans le navigateur
6. Section bot > Privileged Gateway Intents : Cocher "Message Content Intent" sinon erreur "websocket: close 4014: Disallowed intent(s)"

## TODO
- Implémenter la recherche web via l'API Brave Search pour plus de flexibilité (travaux débutés dans le fichier search/brave.go.new)