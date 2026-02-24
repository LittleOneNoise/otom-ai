# Otom-AI
Bot Discord boosté à l'IA sur le thème Dofus

<img src="./assets/mascotte.png" alt="Otom-AI Mascotte" width="200" height="200">

<i>Image générée via Gemini</i>

# Run app
```sh
go mod tidy   # télécharge les dépendances
go build .    # compile l'exécutable
go run .      # compile + lance directement
```

# Ajouter le bot à un serveur
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

# Env
1. DISCORD_TOKEN : On le retrouve sur le Portal > section "Bot" → Token → "Reset Token"