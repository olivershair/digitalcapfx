# Docker

Le fichier `docker-compose.yml` à la racine construit et lance deux images de
production distinctes, ainsi qu'une image ponctuelle qui applique les migrations
de la base de données :

- `digitalcapfx-frontend:latest` — frontend Next.js, accessible à l'adresse http://localhost:3000.
- `digitalcapfx-backend:latest` — API Go, accessible à l'adresse http://localhost:8080.

Les données de PostgreSQL et Redis sont stockées dans les volumes nommés
`pgdata` et `redisdata`. Les documents téléversés restent stockés dans le
compartiment Google Cloud Storage configuré ; ils ne sont pas enregistrés dans
le système de fichiers d'un conteneur.

## Configuration et démarrage

1. Copiez le fichier `.env.example` à la racine vers `.env`, puis définissez un
   `POSTGRES_PASSWORD` alphanumérique robuste.
2. Copiez `digitalcapfx-backend/.env.example` vers
   `digitalcapfx-backend/.env`. Définissez au minimum une valeur unique pour
   `JWT_SECRET`. Ajoutez les identifiants des fournisseurs et les autres
   paramètres du backend nécessaires. Ne commitez aucun des deux fichiers
   `.env`.
3. Depuis la racine du dépôt, construisez et démarrez tous les services :

   ```sh
   docker compose up --build -d
   ```

Compose configure les URL de la base de données et de Redis du backend avec
leurs adresses sur le réseau Docker, et permet au proxy du frontend d'appeler le
service backend. Le backend démarre une fois que PostgreSQL et Redis sont prêts
et que les migrations sont terminées.

Pour reconstruire et démarrer séparément l'une des images de l'application :

```sh
docker compose build frontend
docker compose up -d frontend

docker compose build backend
docker compose up -d backend
```

Consultez les journaux avec `docker compose logs -f frontend backend`. Arrêtez
les services avec `docker compose down` ; les volumes nommés de la base de
données et de Redis sont conservés. Pour supprimer également les données
persistées, exécutez `docker compose down --volumes`.
