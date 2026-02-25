# Otom-AI

<img src="./assets/mascotte.png" alt="Otom-AI Mascotte" width="200" height="200">

<i>Image générée via Gemini</i>

## Description
> Chatbot utilisable sur un serveur Discord.<br/>
Le but principal est d'impersonifié un joueur expert de Dofus mais qui reste chill, les pieds sur Terre et qui se veut blagueur et pote avec tout le monde.

> Sous le capot on utilise un model IA fournit par DeepSeek avec qui on communique via l'API disponible. L'avantage de DeepSeek est son prix imbattable qui de base est très peu cher par rapport à la concurrence mais également on profite de leur système de cache de contexte qui réduit par 10 le coût des tokens.<br/>
Toutefois le modèle à lui seul ne suffit pas pour des sujets récents ou précis mais on peut contourner ce problème via des recherches web en temps réel pour enrichir les données. J'utilise le service Tavily qui autorise 1000 requêtes gratuites.<br/>
PS : L'utilisation du tool call (quand on utilise des recherches web par exemple) rend le modèle DeepSeek instable (il hallucine). Pour le moment cela est contournable en utilisant la version beta du modèle et en configurant une "temperature" assez faible sur le modèle (faible = moins de créativité = moins d'hallucination)

## 1. Télécharger dépendances
```sh
go mod tidy   # télécharge les dépendances
```

## 2. Configurer l'environnement
Dans un fichier `.env`
```yaml
# Identifiants Discord
DISCORD_TOKEN=(Portal Discord > section "Bot" → Token → "Reset Token")

# Identifiants IA (DeepSeek)
DEEPSEEK_API_KEY=
DEEPSEEK_URL=https://api.deepseek.com/beta/chat/completions
DEEPSEEK_MODEL=deepseek-chat

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