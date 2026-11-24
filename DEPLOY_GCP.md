# Déployer DigitalFX sur Google Cloud

Ce guide décrit un déploiement des conteneurs du dépôt sur **Cloud Run**, avec
**Cloud SQL for PostgreSQL**. Le frontend Next.js appelle l'API Go côté serveur
via `BACKEND_API_URL` ; les deux services n'ont donc pas besoin d'être sur un
réseau Docker commun.

Un script de déploiement applicatif est disponible dans
[scripts/deploy-gcp.sh](./scripts/deploy-gcp.sh). Il construit les images,
exécute les migrations puis déploie les deux services Cloud Run. Il ne crée pas
l'instance Cloud SQL, Redis, le bucket GCS, le compte de service ni les secrets :
ces prérequis doivent d'abord être configurés en suivant ce guide.

> **À prévoir :** Cloud SQL, Cloud Run, Artifact Registry, Cloud Build et
> Secret Manager, Cloud NAT et son adresse IP réservée sont facturés selon leur
> utilisation. Pour une base de production, activez la haute disponibilité et
> configurez des sauvegardes.

## 1. Prérequis

- Un projet Google Cloud avec la facturation activée et `gcloud` installé.
- Les identifiants de production des services externes utilisés par le backend.
- Une région cohérente pour les ressources ; les exemples utilisent
  `europe-west2`, comme [cloudbuild.deploy.yaml](./digitalcapfx-backend/cloudbuild.deploy.yaml).
- Un nom de domaine, si vous souhaitez publier le frontend sur votre propre URL.

Connectez-vous et sélectionnez votre projet GCP :

```sh
gcloud auth login
gcloud config set project digitalcap-fx-501314
```

Activez les API nécessaires :

```sh
gcloud services enable \
  run.googleapis.com \
  sqladmin.googleapis.com \
  artifactregistry.googleapis.com \
  cloudbuild.googleapis.com \
  secretmanager.googleapis.com \
  iam.googleapis.com \
  redis.googleapis.com \
  compute.googleapis.com \
  storage.googleapis.com
```

## 2. Créer PostgreSQL avec Cloud SQL

Créez une instance PostgreSQL 16. L'exemple est zonal pour limiter les coûts de
démarrage ; pour une base de production, choisissez une configuration HA et une
capacité adaptée à la charge.

```sh
gcloud sql instances create "$INSTANCE" \
  --database-version=POSTGRES_16 \
  --region="$REGION" \
  --tier=db-custom-1-3840 \
  --storage-type=SSD \
  --storage-size=20 \
  --storage-auto-increase
```

Créez la base et son utilisateur. Saisissez un mot de passe fort quand la
commande le demande ; ne l'ajoutez pas au dépôt ni à un script versionné.

```sh
gcloud sql databases create "$DB_NAME" --instance="$INSTANCE"
gcloud sql users create "$DB_USER" \
  --instance="$INSTANCE" \
  --prompt-for-password
```

### Configuration actuellement en production

Dans le projet `digitalcap-fx-501314`, les données PostgreSQL actives sont
hébergées par l'instance Cloud SQL `digitalcapfx-db` en `europe-west2`
(Londres). Elle utilise PostgreSQL 15, un disque SSD de 10 Go et le niveau
`db-f1-micro`, en configuration zonale sans haute disponibilité multi-zone.
La base applicative est `digitalfx`.

Le backend Cloud Run utilise le VPC `default` et son sous-réseau
`europe-west2/default`. Son trafic sortant passe par le Cloud NAT
`digitalcapfx-smtp-nat` du routeur `digitalcapfx-nat-router`. L'adresse IPv4
statique réservée est `34.147.253.29` (`digitalcapfx-smtp-nat-ip`). Ajoutez
cette IP à la liste autorisée de Brevo. Les journaux du NAT ne retiennent que
les erreurs.

Le bucket d'upload actuellement configuré pour le backend est
`digitalcapfx-uploads`, également en `europe-west2`. Les documents KYC/KYB
envoyés par l'application (notamment CNI, passeports et justificatifs), ainsi
que les selfies, photos de profil et autres fichiers envoyés via l'API
`/uploads`, sont stockés comme objets dans ce bucket. Le code les classe sous
des préfixes tels que `kyc/`, `document/`, `business/` et `avatar/`, par
identifiant utilisateur ; il ne s'agit pas de dossiers locaux Cloud Run.

Le bucket a la prévention d'accès public activée et l'accès uniforme au niveau
du bucket activé. Toutefois, sa politique IAM accorde aussi des rôles de
lecture d'objets aux `projectViewer` du projet. Les personnes disposant du rôle
Viewer GCP sur ce projet peuvent donc avoir accès aux objets : n'accordez pas
ce rôle sans nécessité et examinez une politique IAM limitée au compte
d'exécution et au personnel autorisé. Les liens de lecture signés par
l'application sont temporaires (jusqu'à environ 7 jours). La suppression
réversible du bucket est configurée pour 7 jours ; ce mécanisme n'est pas une
sauvegarde indépendante.

Les données actives de la base et ces fichiers sont donc stockés à Londres ;
les sauvegardes PostgreSQL décrites ci-dessous sont une copie distincte en
Afrique du Sud. Les prestataires de vérification d'identité configurés (Didit,
Sumsub ou MetaMap) peuvent également traiter ou conserver les pièces envoyées
dans leur propre service. Leur région de stockage et leur durée de conservation
dépendent du prestataire et de sa configuration contractuelle ; elles ne sont
pas déterminées par la région du bucket GCS DigitalCapFX.

Récupérez le nom de connexion Cloud SQL pour le réutiliser dans les étapes
suivantes :

```sh
export INSTANCE_CONNECTION_NAME="$(gcloud sql instances describe "$INSTANCE" \
  --format='value(connectionName)')"
echo "$INSTANCE_CONNECTION_NAME"
```

### Sauvegardes de la base de production

L'instance de production utilise un plan Backup and DR quotidien, associé à
Cloud SQL. Le coffre de sauvegarde est situé à Johannesburg (`africa-south1`),
séparé de la région des données actives (`europe-west2`). Les sauvegardes sont
conservées 30 jours et les journaux de restauration 7 jours. Le coffre impose
une rétention minimale de 30 jours. Les premières sauvegardes ont été créées ;
vérifiez leur état et le plan après tout changement de configuration.

Cette sauvegarde inter-région peut générer des frais de stockage Backup and DR
et de transfert réseau. La sauvegarde seule ne déplace pas l'instance active :
pour héberger les données primaires en Afrique du Sud, il faudrait migrer ou
restaurer l'instance dans cette région et reconfigurer les services qui en
dépendent.

Contrôlez le plan et les sauvegardes avec :

```sh
gcloud backup-dr backup-plan-associations list \
  --project=digitalcap-fx-501314 \
  --location=europe-west2

gcloud backup-dr backups list \
  --project=digitalcap-fx-501314 \
  --location=africa-south1
```

Le plan actuellement associé est `digitalcapfx-db-backups` et son coffre est
`digitalcapfx-db-vault-south-africa`. Ne désactivez pas ce plan ni ne réduisez
la rétention sans valider au préalable les exigences de restauration.

### Sauvegarde des documents et photos GCS

Les objets du bucket de production `digitalcapfx-uploads` sont copiés par
Storage Transfer Service vers `digitalcapfx-uploads-backup-sa`, bucket régional
à Johannesburg (`africa-south1`). Le job
`digitalcapfx-uploads-south-africa-daily` est activé pour une copie quotidienne
à 02:00, heure d'Afrique du Sud (UTC+01:00 ; 01:00 UTC), à partir du
22 novembre 2026. La première exécution manuelle du 21 novembre 2026 a réussi :
36 objets et 8 963 196 octets copiés, sans échec signalé.

Le bucket de destination impose la prévention d'accès public et l'accès
uniforme, avec le versionnage activé et une règle qui supprime les versions
non courantes après 90 jours. Le transfert remplace les objets de destination
lorsque leur contenu diffère et retire de la destination les objets qui ne sont
plus présents à la source. Grâce au versionnage, les anciennes versions ainsi
remplacées ou supprimées sont conservées pendant la période configurée ; cette
configuration n'est pas un verrouillage immuable contre un administrateur.
La suppression réversible du bucket est également de 7 jours. Des frais de
stockage régional et de transfert inter-région peuvent s'appliquer.

Contrôlez le job et les paramètres du bucket sans lister les noms des documents :

```sh
gcloud transfer jobs describe \
  transferJobs/digitalcapfx-uploads-south-africa-daily \
  --project=digitalcap-fx-501314

gcloud storage buckets describe \
  gs://digitalcapfx-uploads-backup-sa
```

Le bilan de chaque exécution est disponible dans la console Google Cloud
(Storage Transfer > Jobs > Operations). Pour restaurer, choisissez les objets
nécessaires et copiez-les d'abord vers un bucket de restauration séparé et
privé, puis validez leur intégrité et leurs autorisations avant toute remise
en production. Ne synchronisez pas directement le bucket de sauvegarde vers le
bucket actif : la configuration de transfert inclut une suppression à la
destination et pourrait propager des suppressions.

### Accès à la base de production

Le compte PostgreSQL dédié est `digitalcapfx_admin`, pour la base `digitalfx`.
Il dispose du rôle géré Cloud SQL `cloudsqlsuperuser` et est membre du rôle
applicatif `digitalfx`. Cloud SQL ne donne pas le privilège système PostgreSQL
`SUPERUSER` aux utilisateurs clients.

Son mot de passe n'est pas stocké dans ce dépôt. Il est conservé dans Secret
Manager sous `digitalcapfx-db-admin-password`, avec réplication gérée à
Johannesburg (`africa-south1`). Le compte Google
`digitalcapfx@gmail.com` a le droit de lire ce secret. Récupérez-le uniquement
dans votre terminal authentifié ; ne le copiez pas dans Git, un ticket ou une
conversation :

```sh
gcloud auth login
gcloud config set project digitalcap-fx-501314
gcloud secrets versions access latest \
  --secret=digitalcapfx-db-admin-password \
  --project=digitalcap-fx-501314
```

Pour se connecter depuis un poste local, utilisez Cloud SQL Auth Proxy. Il
fournit une connexion chiffrée et authentifiée sans ouvrir l'instance aux
adresses IP publiques. Laissez le proxy actif dans un premier terminal :

```sh
"$HOME/go/bin/cloud-sql-proxy" --gcloud-auth \
  --address 127.0.0.1 \
  --port 5433 \
  digitalcap-fx-501314:europe-west2:digitalcapfx-db
```

Dans un second terminal, connectez `psql` au proxy et saisissez le mot de passe
du secret à l'invite :

```sh
psql -h 127.0.0.1 -p 5433 \
  -U digitalcapfx_admin \
  -d digitalfx \
  -W
```

Le proxy doit rester lancé pendant la session. Si `psql` affiche
« connexion refusée », vérifiez d'abord qu'il indique
`Listening on 127.0.0.1:5433` dans le premier terminal. N'exposez pas le port
5433 sur Internet. Pour utiliser le compte depuis un autre compte Google,
accordez explicitement à cette identité l'accès au secret et les permissions
Cloud SQL requises ; ne partagez pas le mot de passe existant.

## 3. Créer le dépôt d'images et les secrets

Créez le dépôt Artifact Registry :

```sh
gcloud artifacts repositories create "$REPOSITORY" \
  --repository-format=docker \
  --location="$REGION" \
  --description="Images DigitalFX"
```

Dans **Secret Manager** (Console Google Cloud → Sécurité → Secret Manager),
créez une nouvelle version pour chaque secret ci-dessous. Le nom du secret doit
être celui indiqué ; la valeur réelle vient de votre configuration de
production. Ne mettez pas les secrets dans les commandes, les images ou Git.

| Secret Secret Manager | Contenu |
|---|---|
| `DATABASE_URL` | URL PostgreSQL décrite ci-dessous |
| `JWT_SECRET` | Valeur aléatoire longue et propre à la production |
| `PAYMENTS_API_KEY` | Clé API Payments |
| `CAAS_API_KEY` | Clé API CaaS |
| `HUB2_API_KEY` | Clé API HUB2 |
| `HUB2_SECRET_KEY` | Secret HUB2 |
| `REDIS_URL` | URL du Redis managé, voir la section 4 |

La valeur de `DATABASE_URL` doit utiliser le socket Cloud SQL monté par Cloud Run :

```text
postgres://digitalfx:VOTRE_MOT_DE_PASSE@/digitalfx?host=/cloudsql/PROJECT_ID:REGION:INSTANCE&sslmode=disable
```

Remplacez les trois éléments entre crochets/variables par les valeurs réelles.
Encodez les caractères spéciaux du nom d'utilisateur ou du mot de passe selon
la syntaxe d'une URL. La base et le compte doivent correspondre à ceux créés à
l'étape 2.

Le backend exige aussi les variables `PAYMENTS_API_URL`, `CAAS_API_URL`,
`HUB2_API_KEY` et `HUB2_SECRET_KEY` au démarrage. Ajoutez leurs clés sensibles
à Secret Manager comme ci-dessus. Les clés externes optionnelles (par exemple
Brevo, Nomba, Nilos, Didit ou les fournisseurs KYC) sont à créer et configurer
si ces fonctions sont activées. Consultez
[.env.example](./digitalcapfx-backend/.env.example) pour la liste complète.

## 4. Configurer Redis

L'API utilise Redis en plus de PostgreSQL. **Ne gardez pas la valeur par défaut
`redis://localhost:6379/0` sur Cloud Run** : elle désigne le conteneur du backend,
pas un service Redis.

Créez une instance Redis dans Memorystore, dans la même région, et configurez
un accès réseau VPC entre Cloud Run et Memorystore (connectivité VPC directe ou
connecteur d'accès VPC selon votre configuration). Placez l'adresse privée et,
si nécessaire, le mot de passe dans le secret `REDIS_URL`, au format accepté
par `redis.ParseURL`, par exemple `redis://:MOT_DE_PASSE@IP_PRIVEE:6379/0`.
Configurez le service Cloud Run backend pour utiliser le même réseau/sous-réseau.

Les étapes réseau exactes dépendent du VPC de votre projet ; vérifiez que le
service Cloud Run peut joindre l'adresse privée de Memorystore avant la mise
en production.

Le script fourni suppose le réseau et le sous-réseau `default`. S'ils ont
d'autres noms, définissez `VPC_NETWORK` et `VPC_SUBNET` lors de son lancement.
Le backend route tout son trafic sortant dans le VPC (`all-traffic`) ; un Cloud
NAT avec une adresse IP statique doit donc être configuré pour le sous-réseau
afin que les appels Internet (SMTP, paiements et autres fournisseurs) puissent
sortir. Autorisez l'IP NAT dans les consoles des fournisseurs qui filtrent les
adresses sources.

## 5. Créer un compte de service d'exécution

Le projet `digitalcap-fx-501314` possède déjà le compte de service
`digitalcapfx-dev@digitalcap-fx-501314.iam.gserviceaccount.com`. Le script
l'utilise par défaut pour le backend, le frontend et le job de migration :

```sh
export RUNTIME_SA="digitalcapfx-dev@${PROJECT_ID}.iam.gserviceaccount.com"
```

Le script accorde automatiquement au compte d'exécution l'accès Cloud SQL.
Pour chaque secret requis (`DATABASE_URL`, `REDIS_URL`, `JWT_SECRET`,
`PAYMENTS_API_KEY`, `CAAS_API_KEY`, `HUB2_API_KEY`, `HUB2_SECRET_KEY` et
`BREVO_API_KEY` — **sans cette clé, les codes SMS ne partent pas et personne ne
peut s'inscrire**), il vérifie son existence. Si un secret manque, le script demande sa valeur dans
une entrée masquée, crée le secret et ajoute sa première version sans afficher
la valeur. Il donne ensuite au compte d'exécution l'accès **au niveau de ce
secret**.

**La configuration existante est conservée.** Le script utilise
`--update-env-vars` / `--update-secrets` : il ajoute ou met à jour des
variables, mais n'efface jamais celles déjà configurées sur le service (console
ou déploiements précédents). Une variable déjà définie en clair sur le service
(ex. `BREVO_API_KEY` saisie dans la console) est conservée et signalée.

**Secrets facultatifs** — montés automatiquement s'ils existent dans Secret
Manager (même nom) : `BREVO_SMTP_USER`, `BREVO_SMTP_KEY` (e-mails), les secrets
de webhooks (`PAYMENTS_`, `CAAS_`, `NOMBA_`, `NILOS_WEBHOOK_SECRET`), les
identifiants Nomba / Nilos, les clés du fournisseur KYC (`DIDIT_*`, `SUMSUB_*`,
`METAMAP_*`) et `FIREBASE_CREDENTIALS_JSON`.

**Variables non secrètes** — transmises si elles sont définies dans le shell au
lancement, par exemple :

```sh
KYC_PROVIDER=manual ADMIN_NOTIFY_EMAIL=conformite@exemple.com ./scripts/deploy-gcp.sh
```

(liste complète : `PASSTHROUGH_ENV` dans le script.)

**Rapport final.** Après le déploiement, le script relit la configuration
réellement déployée et affiche : les avertissements, les fonctionnalités
dégradées (e-mails, webhooks…) et, le cas échéant, une **configuration
bloquante** — `BREVO_API_KEY` absent (inscription impossible) ou clé du
fournisseur KYC choisi absente. Dans ce cas il se termine avec le code 2.

Préparez les valeurs avant de relancer le script : URL PostgreSQL avec le socket
Cloud SQL, URL Redis privée joignable via le VPC, secret JWT long et aléatoire,
et clés API des fournisseurs. Le compte `gcloud` actif doit pouvoir créer des
secrets et modifier leurs politiques IAM. Si une permission manque, le script
s'arrête et affiche la commande ou le droit administratif nécessaire. Les
valeurs saisies ne sont pas ajoutées au dépôt.

Pour attribuer ces rôles manuellement, au besoin :

```sh
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:${RUNTIME_SA}" \
  --role="roles/cloudsql.client"

gcloud secrets add-iam-policy-binding DATABASE_URL \
  --project="$PROJECT_ID" \
  --member="serviceAccount:${RUNTIME_SA}" \
  --role="roles/secretmanager.secretAccessor"
```

Répétez la dernière commande pour chaque secret consommé par le backend :
`REDIS_URL`, `JWT_SECRET`, `PAYMENTS_API_KEY`, `CAAS_API_KEY`,
`HUB2_API_KEY` et `HUB2_SECRET_KEY`. L'attribution sur chaque secret limite
l'accès par rapport à un rôle accordé sur tout le projet.

Pour autoriser ce compte à lire tous les secrets du projet (moins restrictif),
vous pouvez plutôt attribuer `roles/secretmanager.secretAccessor` au niveau du
projet :

```sh
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:${RUNTIME_SA}" \
  --role="roles/secretmanager.secretAccessor"
```

Le compte qui lance le déploiement doit également pouvoir utiliser ce compte
de service (`iam.serviceaccounts.actAs`). Le script tente d'ajouter ce droit
directement sur le compte de service pour l'utilisateur `gcloud` actif. Si cet
utilisateur n'a pas le droit de modifier la politique IAM, un administrateur
doit exécuter la commande affichée par le script. Pour le compte
`digitalcapfx@gmail.com`, cela correspond à :

```sh
gcloud iam service-accounts add-iam-policy-binding \
  digitalcapfx-dev@digitalcap-fx-501314.iam.gserviceaccount.com \
  --project=digitalcap-fx-501314 \
  --member=user:digitalcapfx@gmail.com \
  --role=roles/iam.serviceAccountUser
```

Cette attribution est limitée au compte de service d'exécution, plutôt qu'à
tout le projet. La commande peut être relancée sans risque si le droit est
déjà accordé.

Si les téléversements GCS sont utilisés, créez un bucket puis accordez au
compte de service les droits de stockage requis sur ce bucket. Pour la
génération d'URL signées, le compte doit également disposer de
`roles/iam.serviceAccountTokenCreator` sur lui-même.

## 6. Construire et publier les images

Depuis la racine du dépôt, construisez et publiez le backend et l'image des
migrations avec le Cloud Build déjà présent :

```sh
gcloud builds submit \
  --config=digitalcapfx-backend/cloudbuild.deploy.yaml \
  digitalcapfx-backend
```

Construisez et publiez le frontend :

```sh
gcloud builds submit \
  --tag="${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPOSITORY}/frontend:latest" \
  digitalcapfx-frontend
```

Les images backend et migration produites par la configuration existante sont
`.../digitalcapfx/server:latest` et `.../digitalcapfx/migrate:latest`. Assurez-vous
que ces tags sont dans le dépôt Artifact Registry créé précédemment. Si Cloud
Build signale une erreur d'autorisation, accordez au compte de service Cloud
Build le rôle `roles/artifactregistry.writer`.

## 7. Exécuter les migrations PostgreSQL

Les migrations doivent être appliquées **avant** de servir du trafic avec une
nouvelle version du backend. Créez un Cloud Run Job depuis la Console Google
Cloud (Cloud Run → Jobs) avec les paramètres suivants :

- Image :
  `${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPOSITORY}/migrate:latest`
- Compte de service : `digitalcapfx-dev`
- Connexion Cloud SQL : sélectionnez l'instance `digitalcapfx-db`.
- Variable d'environnement `DATABASE_URL` : référence au secret
  `DATABASE_URL` (`latest`).
- Commande : `/bin/sh`
- Arguments (deux éléments) :
  1. `-c`
  2. `migrate -path=/migrations -database="$DATABASE_URL" up`

Enregistrez le job sous `digitalcapfx-migrate`, puis exécutez-le depuis la
Console et attendez son statut **Réussi**. Cette commande utilise l'image et le
répertoire de migrations de [Dockerfile.migrate](./digitalcapfx-backend/Dockerfile.migrate).
N'exécutez pas la commande `down -all` en production : elle supprime le schéma.

Pour les déploiements suivants, reconstruisez l'image de migration si les
migrations ont changé, puis exécutez à nouveau ce job avant de déployer le
backend mis à jour.

## 8. Déployer l'API backend sur Cloud Run

Dans Cloud Run → Services → Créer un service, indiquez :

- Image :
  `${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPOSITORY}/server:latest`
- Région : `europe-west2`
- Compte de service : `digitalcapfx-dev`
- Connexion Cloud SQL : sélectionnez l'instance créée à l'étape 2.
- Port du conteneur : `8080` (Cloud Run fournit la variable `PORT`).
- Réseau : configurez l'accès VPC à Memorystore comme décrit à l'étape 4.
- Variables d'environnement :
  - `ENV=production`
  - `GCP_PROJECT_ID=${PROJECT_ID}`
  - `UPLOADS_BUCKET=VOTRE_BUCKET` si les fichiers sont stockés dans GCS
  - `GCP_SIGNER_SA=${RUNTIME_SA}` si les URLs signées GCS sont activées
  - `PAYMENTS_API_URL`, `CAAS_API_URL` et les autres paramètres non secrets
    nécessaires à vos intégrations
- Secrets : associez `DATABASE_URL`, `JWT_SECRET`, `PAYMENTS_API_KEY`,
  `CAAS_API_KEY`, `HUB2_API_KEY`, `HUB2_SECRET_KEY` et `REDIS_URL` aux variables
  d'environnement de même nom.

Pour ce premier déploiement, autorisez les requêtes non authentifiées au niveau
Cloud Run **si** le frontend public doit appeler l'API sans identité IAM. Le
backend doit alors appliquer l'authentification applicative pour les routes
protégées. Ne rendez pas les secrets ou les routes administratives accessibles
sans les protections attendues. Une politique IAM privée nécessite d'adapter
le proxy Next.js pour qu'il transmette une identité de service.

Après création, copiez l'URL HTTPS du service, par exemple :

```sh
export BACKEND_URL="https://URL_DU_BACKEND"
```

Vérifiez que l'API répond et consultez les journaux Cloud Run avant de poursuivre.

## 9. Déployer le frontend

Dans Cloud Run → Services → Créer un service, indiquez :

- Image :
  `${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPOSITORY}/frontend:latest`
- Région : la même que le backend.
- Port du conteneur : `3000`.
- Compte de service : `digitalcapfx-dev`.
- Variable d'environnement `BACKEND_API_URL` : URL HTTPS du backend avec le
  préfixe API `/api/v1`, par exemple
  `https://digitalcapfx-backend-....run.app/api/v1` ou
  `https://api.digitalcapfx.com/api/v1`. Le frontend utilise les routes API
  sans préfixe dans son code ; le proxy les transmet sous `/api/v1`.
- Pour les fonctions OAuth, domaines/callbacks ou URLs publiques, configurez
  également les variables et URLs correspondantes dans l'application et chez
  les fournisseurs concernés.

Autorisez les requêtes non authentifiées pour le frontend public. Le navigateur
appelle les routes proxy Next.js ; le serveur frontend contacte ensuite le
backend via `BACKEND_API_URL`. Il ne faut donc pas utiliser l'adresse Docker
locale `http://backend:8080` en production.

## 10. Domaine personnalisé pour le frontend et l’API

Les mappings de domaines Cloud Run ne sont pas disponibles dans la région
`europe-west2`. Pour servir les deux services sous le même domaine, utilisez un
répartiteur HTTPS global avec deux serverless NEGs :

- `digitalcapfx.com` → frontend Cloud Run.
- `api.digitalcapfx.com` → backend Cloud Run.
- Le port HTTP redirige vers HTTPS.

Le navigateur ouvre le frontend à `https://digitalcapfx.com`. Le frontend
transmet les appels API côté serveur à
`https://api.digitalcapfx.com/api/v1` ; les requêtes du navigateur restent
donc sur le domaine du frontend et n'ont pas besoin d'appeler directement le
backend.

Cette configuration a été créée dans le projet `digitalcap-fx-501314`. Son
adresse IP globale réservée est actuellement `34.8.199.224`. Comme aucune zone
Cloud DNS n’existe dans le projet, les enregistrements se gèrent chez le
fournisseur DNS du domaine. Ajoutez ou remplacez ces enregistrements :

| Type | Nom / hôte | Valeur |
|---|---|---|
| A | `@` (ou `digitalcapfx.com`) | `34.8.199.224` |
| A | `api` (ou `api.digitalcapfx.com`) | `34.8.199.224` |

Au fournisseur DNS, supprimez ou remplacez les anciens enregistrements A/AAAA
ou CNAME en conflit pour ces noms. Ne modifiez pas les enregistrements MX ni
les autres noms utilisés pour le courrier ou d'autres services. Vérifiez
l'adresse IP actuellement réservée dans GCP avant de la saisir. Après
propagation DNS, attendez que le certificat géré
`digitalcapfx-managed-cert` passe à l’état `ACTIVE` et vérifiez que ses noms
couvrent **les deux** hôtes (`digitalcapfx.com` et `api.digitalcapfx.com`) :

```sh
gcloud compute ssl-certificates describe digitalcapfx-managed-cert \
 --project=digitalcap-fx-501314 \
 --global \
 --format='yaml(managed.status,managed.domainStatus)'
```

Le fichier fourni `digitalcapfx_com.crt` est un certificat public, pas une clé
privée. Il couvre `digitalcapfx.com`, `olivershairwig.com` et
`infiniagbcollect.com`, mais **pas** `api.digitalcapfx.com`. Il ne peut donc
pas, à lui seul, servir au répartiteur pour sécuriser les deux hôtes. La
configuration recommandée ici utilise le certificat TLS géré par Google ; il
n'est pas nécessaire de téléverser le certificat fourni. Si vous choisissez
plutôt un certificat fourni par votre émetteur, il faut aussi sa clé privée
correspondante et un certificat couvrant `api.digitalcapfx.com` (ou un
certificat wildcard adapté). Ne placez jamais la clé privée dans Git.

Une fois le DNS propagé, le certificat actif et les règles d'hôte du
répartiteur vérifiées, faites pointer le proxy du frontend vers le sous-domaine
API :

```sh
gcloud run services update digitalcapfx-frontend \
 --project=digitalcap-fx-501314 \
 --region=europe-west2 \
 --update-env-vars=BACKEND_API_URL=https://api.digitalcapfx.com/api/v1
```

Vérifiez `https://api.digitalcapfx.com/health`, puis ouvrez
`https://digitalcapfx.com`. Les certificats gérés ne deviennent actifs qu’une
fois que leurs noms résolvent vers l’IP du répartiteur.

## 11. Vérifications et mises à jour

1. Confirmez que le job de migration s'est terminé sans erreur.
2. Ouvrez l'URL du frontend et vérifiez les parcours d'authentification et les
   appels API.
3. Vérifiez dans les journaux Cloud Run que backend et frontend démarrent sans
   erreur de configuration, PostgreSQL ou Redis.
4. Testez séparément les téléversements GCS et les intégrations externes activées.
5. Vérifiez le plan quotidien Backup and DR, la présence de sauvegardes
   `ACTIVE` dans le coffre `africa-south1`, et testez périodiquement une
   restauration vers une instance distincte avant d'en dépendre en production.
6. Vérifiez que le job Storage Transfer
   `digitalcapfx-uploads-south-africa-daily` est `ENABLED`, que ses exécutions
   quotidiennes réussissent et que le bucket de destination reste privé avec
   versionnage et rétention des versions non courantes de 90 jours.

Pour une nouvelle version : publiez les images, exécutez les migrations si
nécessaire, déployez le backend puis le frontend. Utilisez des tags versionnés
ou des digests d'image plutôt que `latest` pour les déploiements de production
reproductibles.

La configuration Docker locale reste décrite dans [DOCKER.md](./DOCKER.md) ;
elle utilise PostgreSQL et Redis sur le réseau Docker Compose et n'est pas
l'adresse à employer entre services Cloud Run.

## Déployer les mises à jour avec le script

Après avoir terminé le provisionnement des ressources GCP, connectez-vous au
bon compte et projet puis lancez le script depuis la racine du dépôt. Il créera
les secrets applicatifs manquants en demandant leur valeur de façon masquée :

```sh
gcloud auth login
gcloud config set project digitalcap-fx-501314
bash scripts/deploy-gcp.sh
```

Le script demande de saisir l'ID exact du projet avant de lancer les commandes
qui peuvent être facturées. Il exécute ensuite le build Cloud Build, le job de
migrations, le déploiement backend et le frontend configuré avec l'URL effective
du backend. Pour que le frontend continue d'utiliser l'API sur votre domaine
après chaque redéploiement, indiquez l'URL HTTPS publique du backend :

```sh
BACKEND_API_URL=https://api.digitalcapfx.com bash scripts/deploy-gcp.sh
```

Le script ajoute automatiquement `/api/v1` si le chemin n'est pas déjà présent.
Sans cette variable, il utilise l'URL HTTPS `run.app` du backend, avec ce
préfixe API.
Par défaut, HUB2 reste en mode `sandbox` ; pour activer le mode production,
lancez le script avec `HUB2_MODE=production`.

Préparez les valeurs pour `DATABASE_URL`, `REDIS_URL`, `JWT_SECRET`,
`PAYMENTS_API_KEY`, `CAAS_API_KEY`, `HUB2_API_KEY` et `HUB2_SECRET_KEY`.
`DATABASE_URL` doit utiliser le socket Cloud SQL
`/cloudsql/PROJECT_ID:REGION:INSTANCE` et `REDIS_URL` doit joindre l'IP privée
de Memorystore depuis le VPC configuré. Si les téléversements GCS sont activés,
indiquez le bucket avec `UPLOADS_BUCKET=nom-du-bucket`.


to visit our console google 

https://console.cloud.google.com/run/jobs/executions/details/europe-west2/digitalcapfx-migrate-vcmr7?project=913954257406

Service [digitalcapfx-frontend] revision [digitalcapfx-frontend-00001-dcn] has been deployed and is serving 100 percent of traffic.
Service URL: https://digitalcapfx-frontend-913954257406.europe-west2.run.app

Frontend : https://digitalcapfx-frontend-sxplrmriha-nw.a.run.app
Backend : https://digitalcapfx-backend-sxplrmriha-nw.a.run.app

password bd = dX9>fqKK=a"~1IFi

pour ce connecter a la base de donnees dans GCP

gcloud sql connect digitalcapfx-db \
  --user=digitalcapfx_admin \
  --database=digitalfx