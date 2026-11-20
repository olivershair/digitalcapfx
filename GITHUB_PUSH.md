# Publication sur GitHub

Le dépôt regroupe le frontend (`digitalcapfx-frontend/`) et le backend
(`digitalcapfx-backend/`).

## Fichiers sensibles

Les fichiers `.env`, `.env.local` et les autres variantes `.env.*` sont exclus
du dépôt. Seuls les fichiers `.env.example` sont destinés à être partagés.
Les clés privées, certificats, fichiers de credentials et dossiers locaux
`secrets/` et `credentials/` sont également ignorés.

Ne copiez jamais une vraie clé ou un mot de passe dans un fichier suivi par Git.
Après clonage, créez les fichiers `.env` nécessaires à partir des exemples et
renseignez-y localement vos propres valeurs. Les identifiants qui ont déjà été
partagés doivent être révoqués et remplacés.

## Authentification GitHub

GitHub n'accepte plus le mot de passe du compte pour les opérations Git en HTTPS.
Utilisez GitHub CLI (`gh auth login`) ou un Personal Access Token (PAT) saisi
uniquement dans l'invite sécurisée de Git. Ne mettez jamais le token dans l'URL
du dépôt ni dans un fichier du projet.

Pour cloner puis publier les changements suivants :

```bash
git clone https://github.com/olivershair/digitalcapfx.git
cd digitalcapfx
# Copiez/modifiez les fichiers du projet, sans copier les fichiers .env.
git add .
git commit -m "Describe the project changes"
git push origin main
```

Le dépôt contient les sources du frontend et du backend, pas leurs dépendances
installées ni leurs fichiers de configuration secrets.
