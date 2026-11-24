# Correctifs de sécurité — DigitalFX

**Date :** 24 novembre 2026
**Périmètre :** application web — `digitalcapfx-backend` (API Go) et `digitalcapfx-frontend` (Next.js)
**Statut :** les 18 failles identifiées sont corrigées : 4 critiques (lot 1), 12 de gravité élevée, moyenne ou faible (lot 2), puis 2 liées à l'inscription (lot 3). Des tests de non-régression ont été ajoutés. Backend : `go build`, `go vet` et `go test ./...` au vert. Frontend : `tsc` et `next build` au vert.

> ⚠️ Document sensible : il décrit des vulnérabilités qui ont existé en production. Diffusion restreinte à l'équipe technique et aux personnes habilitées.

---

## Synthèse

| # | Faille | Gravité | Impact avant correctif | Statut |
|---|--------|---------|------------------------|--------|
| 1 | Contournement de la 2FA et jetons interchangeables | Critique | Connexion sans code TOTP ; refresh token (7 jours) utilisable comme jeton d'accès | ✅ Corrigé |
| 2 | Brute-force du PIN et des codes OTP | Critique | Prise de contrôle de compte par essais successifs | ✅ Corrigé |
| 3 | Webhooks de crédit acceptés sans signature | Critique | Crédit frauduleux de soldes si un secret n'est pas configuré | ✅ Corrigé — **action requise en production** |
| 4 | Accès aux fichiers d'autres utilisateurs (IDOR) | Critique | Fuite des pièces d'identité et selfies KYC de tous les clients | ✅ Corrigé |
| R0 | Jetons de session mélangés entre utilisateurs (proxy web) | Critique | Un utilisateur pouvait recevoir les cookies de session d'un autre | ✅ Corrigé |
| R1 | CSRF via le proxy web | Élevée | Virement ou retrait déclenché depuis un site malveillant | ✅ Corrigé |
| R2 | IDOR sur le détail des transactions | Élevée | Lecture des transactions de n'importe quel client | ✅ Corrigé |
| R3 | Seed et clé privée exportables sans réauthentification | Élevée | Vol des cryptos avec un simple jeton volé | ✅ Corrigé |
| R4 | Sessions non révoquées (déconnexion, désactivation) | Élevée | Accès maintenu après déconnexion ou blocage du compte | ✅ Corrigé |
| R5 | Webhook MetaMap non signé | Élevée | Faux rejets ou faux statuts KYC | ✅ Corrigé |
| R6 | CORS `*` avec cookies autorisés | Élevée | Appels cross-origin authentifiés | ✅ Corrigé |
| R7 | Aucun en-tête de sécurité sur le site | Moyenne | Clickjacking, absence de CSP et de HSTS | ✅ Corrigé |
| R8 | Énumération des comptes (PIN oublié) | Moyenne | Savoir si un numéro ou un e-mail est client | ✅ Corrigé |
| R9 | Données sensibles dans les logs et les réponses | Moyenne | OTP, téléphones et erreurs internes exposés | ✅ Corrigé |
| R10 | Swagger public | Moyenne | Cartographie complète de l'API | ✅ Corrigé |
| R11 | IP client falsifiable | Faible | IP fausses dans les appareils et les alertes de connexion | ✅ Corrigé |
| R12 | Inscription sans vérification du numéro | Élevée | Création d'un compte au nom du numéro d'une autre personne | ✅ Corrigé |
| R13 | Le script de déploiement effaçait la configuration | Élevée | SMS désactivé → aucune inscription possible après un déploiement | ✅ Corrigé |

---

## 1. Contournement de la 2FA et jetons interchangeables

### Problème
L'API émet trois types de jetons JWT, tous signés avec le même `JWT_SECRET` :

- **access** : jeton d'accès, 30 min ;
- **refresh** : jeton de renouvellement, 7 jours ;
- **2fa_pending** : jeton remis après un PIN correct, en attendant le code TOTP.

Le middleware d'authentification vérifiait la signature et l'expiration, mais **pas le type du jeton** (champ `sub`).

### Scénario d'attaque
1. L'attaquant connaît le PIN d'une victime qui a activé la 2FA.
2. `POST /auth/login` lui renvoie un jeton `2fa_pending`.
3. Il envoie ce jeton directement en `Authorization: Bearer …`. Il obtient alors un accès complet au compte (wallets, virements, export de la seed) **sans jamais fournir le code TOTP**.

De la même façon, un refresh token volé donnait 7 jours d'accès direct à l'API.

### Correctif
- `internal/api/middleware/auth.go` : seul un jeton dont le `sub` vaut `access` est accepté. L'algorithme est restreint à HS256 (`jwt.WithValidMethods`).
- `internal/pkg/token/token.go` : ajout de `ParseAccess`, `ParseRefresh` et `ParseWithSubject`. `Parse` n'accepte plus que HS256, et `ParsePending2FA` passe par la vérification du sujet.
- `internal/services/auth_service.go` (`RefreshToken`) : seul un jeton `refresh` peut être échangé contre une nouvelle paire.

### Vérification
`internal/api/handlers/security_fixes_test.go` contient deux tests :
- `TestAuth_OnlyAccessTokensAccepted` : un jeton 2FA et un refresh token reçoivent 401, un access token reçoit 200.
- `TestToken_ParseRefreshRejectsOtherKinds`.

---

## 2. Brute-force du PIN et des codes OTP

### Problème
- Aucune limitation du nombre d'essais sur le login PIN, la vérification OTP, le code TOTP, la réinitialisation du PIN et la vérification de l'e-mail.
- La réinitialisation du PIN acceptait un code à 6 chiffres valable 10 minutes, sans limite d'essais. Tester le million de combinaisons permettait de **changer le PIN de n'importe quel compte**.
- Aucune limite d'envoi de codes : on pouvait multiplier les codes valides en même temps, et inonder la victime de SMS ou d'e-mails.
- Les codes étaient générés avec `math/rand`, un générateur non cryptographique.

### Correctif
Un nouveau fichier, `internal/services/auth_attempts.go`, ajoute des compteurs stockés dans **Redis**, donc partagés entre toutes les instances Cloud Run.

| Point d'entrée | Clé du compteur | Règle |
|----------------|-----------------|-------|
| `POST /auth/login` (PIN) | ID utilisateur | 5 échecs → verrouillage 15 min |
| `POST /auth/2fa/login` (TOTP) | ID utilisateur | 5 échecs → verrouillage 15 min |
| `POST /auth/otp/verify` | Téléphone normalisé | 5 échecs → verrouillage 15 min |
| `POST /auth/reset-pin` | ID utilisateur | 5 échecs → verrouillage 15 min |
| `POST /auth/email/verify` | ID utilisateur | 5 échecs → verrouillage 15 min |
| `POST /auth/otp/send` | Téléphone normalisé | 5 envois max par heure |
| `POST /auth/forgot-pin` | Téléphone ou e-mail du compte | 5 envois max par heure |

Fonctionnement :
- Tant que le compte est verrouillé, le secret **n'est même pas vérifié**. L'API répond `429 TOO_MANY_ATTEMPTS`.
- Une réussite remet le compteur à zéro.
- Le verrouillage (15 min) dure plus longtemps qu'un OTP (10 min). Un code ciblé a donc expiré quand le verrou se lève.
- Les compteurs sont liés au **compte**, pas à l'adresse IP. L'en-tête `X-Forwarded-For` est contrôlé par le client, donc une limite par IP se contourne facilement.
- Tous les OTP sont maintenant générés avec `crypto/rand` (`secureOTP()`).
- Les handlers renvoient `429` grâce au nouveau helper `response.TooManyRequests`.

### Compromis assumés
- **Redis indisponible** : les vérifications laissent passer (*fail-open*) et l'incident est journalisé. Cela évite qu'une panne Redis bloque toutes les connexions. Il faut surveiller le message `auth lockout check failed` dans les logs.
- **Déni de service ciblé** : un tiers qui connaît le numéro d'une victime peut bloquer sa connexion pendant 15 minutes. C'est le compromis standard face au risque de prise de contrôle.

### Vérification
`TestSecureOTP` (dans `internal/services/security_fixes_test.go`) vérifie que les codes ont bien 6 chiffres et qu'ils sont aléatoires.

---

## 3. Webhooks de crédit acceptés sans signature

### Problème
Quatre webhooks publics peuvent créditer des soldes ou modifier des transactions :

| Webhook | Variable d'environnement |
|---------|--------------------------|
| `POST /webhooks/payments` (dépôts crypto WaaS) | `PAYMENTS_WEBHOOK_SECRET` |
| `POST /webhooks/caas` (transferts et swaps USDC) | `CAAS_WEBHOOK_SECRET` |
| `POST /webhooks/nomba` (crédits NGN) | `NOMBA_WEBHOOK_SECRET` |
| `POST /webhooks/nilos` (dépôts EUR, GBP, …) | `NILOS_WEBHOOK_SECRET` |

Quand le secret était vide, **la vérification de signature était simplement ignorée**. N'importe qui pouvait alors envoyer un faux événement `wallet.deposit.confirmed` et se créditer un solde. Le script `scripts/deploy-gcp.sh` n'injecte aucune de ces quatre variables.

### Correctif
Les webhooks échouent désormais en mode fermé (*fail-closed*), avec une **erreur explicite** :
- **Secret absent** : réponse `503`, et rien n'est traité. Le code 503 pousse le fournisseur à **réessayer plus tard**, donc aucun dépôt réel n'est perdu une fois le secret configuré. La réponse indique quelle variable manque :
  ```json
  {
    "success": false,
    "error": {
      "code": "WEBHOOK_SECRET_MISSING",
      "message": "the payments (Rach WaaS) webhook is disabled because the server secret PAYMENTS_WEBHOOK_SECRET is not configured — set PAYMENTS_WEBHOOK_SECRET in the backend environment (Secret Manager) and redeploy; no delivery is processed until then"
    },
    "data": { "webhook": "payments (Rach WaaS)", "missing_secret": "PAYMENTS_WEBHOOK_SECRET" }
  }
  ```
- **Signature invalide** : réponse `401` avec le code `WEBHOOK_SIGNATURE_INVALID`. Le message indique l'en-tête vérifié et la variable de secret utilisée.
- Seul le **nom** de la variable apparaît dans la réponse, jamais sa valeur.
- Au démarrage, l'API journalise une erreur pour chaque secret manquant (`internal/api/routes.go`).
- La signature reçue n'est plus écrite dans les logs du webhook payments.

Fichiers modifiés : `payments_webhook.go`, `caas_webhook.go`, `nomba_webhook.go`, `nilos_webhook.go`, `services/nomba_service.go` et `services/nilos_service.go` (nouvelle erreur `ErrWebhookNotConfigured`).

### Vérification
`TestWebhooks_MissingSecretIsExplicit` vérifie que les webhooks payments et caas renvoient bien un 503 `WEBHOOK_SECRET_MISSING` qui nomme la variable.

### ⚠️ Action requise avant le déploiement
Si l'une de ces variables n'est pas définie en production, **le webhook correspondant refusera toutes les livraisons** après le déploiement. Il faut donc :

1. Récupérer chaque secret dans la console du fournisseur concerné (Rach WaaS, Rach CaaS, Nomba, Nilos).
2. Le créer dans Secret Manager, puis l'ajouter à `--set-secrets` dans `scripts/deploy-gcp.sh`, par exemple :
   `PAYMENTS_WEBHOOK_SECRET=PAYMENTS_WEBHOOK_SECRET:latest,CAAS_WEBHOOK_SECRET=CAAS_WEBHOOK_SECRET:latest,NOMBA_WEBHOOK_SECRET=NOMBA_WEBHOOK_SECRET:latest,NILOS_WEBHOOK_SECRET=NILOS_WEBHOOK_SECRET:latest`
3. Après le déploiement, vérifier qu'aucune erreur `webhook secret not set` n'apparaît au démarrage.

---

## 4. Accès aux fichiers d'autres utilisateurs (IDOR)

### Problème
`GET /api/v1/uploads/read-url?object=<chemin>` générait une URL signée, valable jusqu'à 7 jours, pour **n'importe quel objet** du bucket. Le seul contrôle était d'être connecté.

Les chemins suivent le format `kyc/<id-utilisateur>/<uuid>-<nom>`. Un utilisateur qui connaissait ou devinait ces chemins pouvait donc télécharger les **pièces d'identité et selfies KYC** des autres clients.

### Correctif
- `internal/services/upload_service.go` : nouvelle méthode `ReadURLForUser`. Elle n'accepte que les chemins de la forme `<purpose connu>/<id de l'appelant>/<fichier>`. Elle refuse `..`, les chemins absolus, les antislashs, les chemins non normalisés et l'ID d'un autre utilisateur.
- `internal/api/handlers/upload_handler.go` : l'endpoint utilise `ReadURLForUser` et renvoie `404` pour un objet qui n'appartient pas à l'appelant. Le code 404 évite de confirmer que l'objet existe.
- `ReadURL` (sans contrôle d'appartenance) reste réservée au code interne de confiance, comme le précise son commentaire.

### Vérification
`TestObjectOwnedBy` couvre le propriétaire, un autre utilisateur, la traversée `..`, un chemin absolu, un préfixe inconnu et un chemin incomplet.

### Impact fonctionnel
Le frontend client n'appelle cet endpoint que pour les fichiers de l'utilisateur, donc rien ne change pour lui. Si un back-office staff utilisait cet endpoint pour consulter les documents KYC des clients, il recevra désormais 404. Il faudra alors lui fournir un endpoint `/admin/...` protégé par la permission `kyc.view`.

---

# Lot 2 — Failles élevées, moyennes et faibles

## R0. Jetons de session mélangés entre utilisateurs (proxy web) — *découverte pendant le lot 2*

**Problème.** Dans `digitalcapfx-frontend/src/app/api/proxy/route.ts`, le rafraîchissement de jeton était dédupliqué par **une seule promesse globale**, partagée par tous les visiteurs du serveur Next.js. Si deux utilisateurs avaient un jeton expiré au même moment, le second recevait le résultat du premier, c'est-à-dire **les cookies de session de l'autre utilisateur**.

**Correctif.** Les rafraîchissements en cours sont stockés dans une `Map` indexée par refresh token. Chaque utilisateur a donc son propre rafraîchissement, et les requêtes parallèles d'un même navigateur restent dédupliquées.

## R1. CSRF via le proxy web

**Problème.** `/api/proxy` envoyait la requête à n'importe quel endpoint du backend, avec n'importe quelle méthode et des en-têtes fournis par le navigateur, en s'authentifiant avec le cookie `noe_token`. Les cookies n'avaient pas d'attribut `SameSite` explicite, et le corps était lu comme du JSON sans vérifier le `Content-Type`. Un formulaire `text/plain` posté depuis un autre site pouvait donc déclencher un virement.

**Correctif** (nouveau module `src/app/api/_lib/security.ts`, appliqué à `/api/proxy` et `/api/upload`) :
- **Vérification d'origine** : l'en-tête `Origin` doit correspondre au domaine du site, ou à défaut `Sec-Fetch-Site` doit valoir `same-origin`. Sinon : `403 CSRF_REJECTED`. Des domaines supplémentaires peuvent être autorisés avec la variable `ALLOWED_ORIGINS`.
- **`Content-Type: application/json` obligatoire** sur le proxy (sinon `415`). Un formulaire HTML ne peut pas l'envoyer sans requête préalable CORS, que le proxy n'accorde jamais.
- **Cookies `SameSite=Lax`**, `httpOnly`, `Secure` en production.
- **Validation de l'endpoint** : pas de `..` ni de `%` (traversée encodée) dans le chemin, pas de `//`, `:`, `@`, `\` ou `#`. Les chemins `webhooks/*` et `swagger/*` sont interdits. Seules les méthodes GET, POST, PUT, PATCH et DELETE sont acceptées.
- **Les en-têtes fournis par le navigateur ne sont plus relayés** : il était possible d'écraser `Authorization` ou de falsifier `X-Forwarded-For`.
- `/api/upload` : vérification d'origine, liste blanche des valeurs `purpose`, taille limitée.
- Le service frontend `webhook.service.ts`, inutilisé, permettait de déclencher des webhooks depuis le navigateur. Il a été supprimé.

## R2. IDOR sur le détail des transactions

**Problème.** `GET /api/v1/accounts/{currency}/transactions/{id}` renvoyait n'importe quelle transaction à partir de son UUID.

**Correctif.** `AccountService.GetTransaction(ctx, userID, id)` vérifie que la transaction appartient à un compte de l'utilisateur. Sinon, la réponse est un `404`, identique à celle d'une transaction inexistante. Les autres endpoints qui prennent un ID (tickets, bénéficiaires, directeurs, notifications, appareils, cartes, retraits, swaps et transferts CaaS, équipe) ont été vérifiés : ils filtrent déjà par utilisateur.

## R3. Export de la seed et de la clé privée sans réauthentification

**Correctif.** Ces exports exigent maintenant une **réauthentification** (`AuthService.VerifyStepUp`) :
- `POST /api/v1/wallets/seed` (anciennement `GET`) et `POST /api/v1/wallets/export-key` exigent `pin`, plus `totp_code` si la 2FA est activée.
- Après 5 échecs, l'export est verrouillé 15 minutes (`429`).
- Les erreurs sont explicites : `403 STEP_UP_REQUIRED` (PIN ou code manquant, ou compte sans PIN ni 2FA) et `403 STEP_UP_FAILED` (PIN ou code incorrect).

## R4. Sessions non révoquées

**Problème.** Un jeton d'accès restait valable jusqu'à son expiration, même après une déconnexion, la déconnexion d'un appareil, une réinitialisation du PIN ou la désactivation du compte par un admin. Les cookies duraient 30 jours.

**Correctif :**
- Le nouveau middleware `middleware.ActiveSession` (ajouté après `Auth` sur toutes les routes protégées) exige à chaque requête que la session existe, soit active et non expirée, et que `users.is_active` soit vrai. Il fait une requête SQL indexée.
- `RefreshToken` refuse un compte désactivé et vérifie que la session appartient bien à l'utilisateur du jeton.
- Côté web, le cookie `noe_token` dure maintenant 30 min (comme le jeton d'accès) et `noe_refresh_token` 7 jours (comme le refresh token), au lieu de 30 jours. Ces durées sont réglables avec `ACCESS_COOKIE_MAX_AGE_SECONDS` et `REFRESH_COOKIE_MAX_AGE_SECONDS`.

## R5. Webhook MetaMap non signé

**Correctif.** `POST /webhooks/metamap` vérifie maintenant l'en-tête `X-Signature` : HMAC-SHA256 du corps brut avec `METAMAP_WEBHOOK_SECRET`, comparé en temps constant. Comme les autres webhooks, il échoue en mode fermé : `503 WEBHOOK_SECRET_MISSING` sans secret, `401 WEBHOOK_SIGNATURE_INVALID` si la signature est mauvaise. `kyc/metamap_provider.go`, qui acceptait tout quand le secret était vide, a aussi été corrigé.

## R6. CORS trop ouvert

**Correctif.** `AllowedOrigins: *` a été supprimé. Les origines viennent de `CORS_ALLOWED_ORIGINS` (liste séparée par des virgules ; la valeur `*` est refusée). **Si la variable est vide, aucun accès cross-origin n'est accordé.** C'est le bon réglage ici, puisque le navigateur passe par le proxy Next.js côté serveur. Attention : `go-chi/cors` interprète une liste vide comme `*`, donc le middleware n'est monté que si des origines sont configurées.

## R7. En-têtes de sécurité

**Site web** (`next.config.ts`, sur toutes les pages) :
- **CSP** : `default-src 'self'`, `frame-ancestors 'none'`, `object-src 'none'`, `base-uri 'self'`, `form-action 'self'`. Seuls les tiers réellement utilisés sont autorisés : Fontshare, l'iframe KYC Didit (`*.didit.me`) et les URL signées Google Cloud Storage.
- Également : `X-Frame-Options: DENY` (anti-clickjacking), HSTS, `nosniff`, `Referrer-Policy` et `Permissions-Policy`. L'en-tête `X-Powered-By` est supprimé.
- **Limite** : `script-src` contient encore `'unsafe-inline'`, nécessaire aux scripts de démarrage de Next.js. Passer à des *nonces* serait un durcissement ultérieur.

**API** : le nouveau middleware `SecurityHeaders` ajoute `nosniff`, `X-Frame-Options: DENY`, HSTS, `Referrer-Policy: no-referrer` et une CSP `default-src 'none'`.

## R8. Énumération des comptes

**Correctif :**
- `POST /auth/forgot-pin` répond **toujours** `200` avec le même message (« if an account exists… »), que le compte existe ou non.
- `POST /auth/reset-pin` répond `INVALID_OTP` pour un compte inconnu comme pour un code faux.
- Le login renvoyait déjà `invalid credentials` dans les deux cas.

## R9. Données sensibles dans les logs et les réponses

**Correctif :**
- Les OTP ne sont **jamais** journalisés en production, même avec `DEBUG=true`.
- Les numéros de téléphone sont masqués dans tous les logs (`redact.Phone`, par exemple `**********678`). Cela couvre l'authentification, HUB2, CaaS, les retraits et le webhook HUB2.
- Les signatures de webhook reçues ne sont plus journalisées.
- Les proxys web renvoient des erreurs génériques. Le détail reste uniquement dans les logs serveur.

## R10. Swagger public

**Correctif.** `/swagger/*` n'est servi que si `SWAGGER_ENABLED=true`. Par défaut, il est activé hors production et désactivé en production (`ENV=production`).

## R11. IP client falsifiable

**Problème.** `chi/middleware.RealIP` et `realIP()` lisaient la valeur la plus à gauche de `X-Forwarded-For`, que le client contrôle entièrement.

**Correctif :**
- Le nouveau middleware `middleware.ClientIP(TRUSTED_PROXY_HOPS)` lit l'entrée ajoutée par **nos** proxys, en comptant depuis la droite. Par défaut, il y en a 2 : le proxy Next.js et le front-end Cloud Run. Si l'en-tête est incohérent, l'adresse TCP du pair est utilisée.
- Le proxy Next.js transmet l'IP réelle du visiteur, lue à droite de son propre `X-Forwarded-For`.
- `realIP()` utilise uniquement cette valeur.

## Vérification (lot 2)

| Test | Couvre |
|------|--------|
| `TestClientIPFromXFF`, `TestClientIP_FallsBackToPeer` | R11 |
| `TestSecurityHeaders` | R7 (API) |
| `TestVerifyMetaMapWebhook` | R5 (fail-closed, signature valide, invalide ou absente) |
| `redact.TestPhone` | R9 |
| `next build` + `tsc --noEmit` | R0, R1, R7 (frontend) |

R2, R3 et R4 dépendent de la base de données et ne sont pas couverts par les tests unitaires. Ils sont à valider en recette (voir la checklist ci-dessous).

---

# Lot 3 — Inscription

## R12. Inscription possible sans vérifier le numéro de téléphone

**Problème.** `POST /auth/register` ne vérifiait pas que le numéro avait été validé par le code SMS. N'importe qui pouvait créer un compte via l'API avec le numéro d'une autre personne, sans recevoir de code.

**Correctif :**
- `POST /auth/otp/verify` renvoie maintenant `data.verification_token`. C'est une **preuve signée** que le code SMS a été saisi pour ce numéro, valable **60 minutes** (sujet JWT `phone_verified`). Le middleware d'authentification la refuse : elle ne permet pas de se connecter.
- `POST /auth/register` exige `phone_verification_token`. Le numéro de la preuve doit être celui de l'inscription, sinon la réponse est `400 PHONE_NOT_VERIFIED`, avec un message explicite.
- Côté web, le jeton est gardé en mémoire (store d'inscription) et envoyé à l'inscription. Si la preuve a expiré ou si le numéro a changé, l'utilisateur est renvoyé à l'étape de vérification du téléphone.

**Vérifié de bout en bout en local :** inscription sans preuve → 400 ; preuve d'un autre numéro → 400 ; parcours normal → 201 ; preuve utilisée comme jeton de connexion → 401.

## R13. Le script de déploiement effaçait la configuration (inscription bloquée)

**Problème.** `scripts/deploy-gcp.sh` utilisait `--set-env-vars` et `--set-secrets`, qui **remplacent toutes** les variables du service Cloud Run. `BREVO_API_KEY` (SMS), les identifiants SMTP, les clés KYC, les secrets de webhooks et `ADMIN_NOTIFY_EMAIL` étaient donc effacés à chaque déploiement. L'envoi du code SMS échouait (`SMS delivery is temporarily unavailable`) et plus personne ne pouvait s'inscrire.

**Correctif :** le script utilise `--update-env-vars` et `--update-secrets` (rien n'est plus effacé). `BREVO_API_KEY` devient un secret obligatoire, les secrets facultatifs sont montés s'ils existent, et les variables non secrètes sont transmises depuis le shell. En fin de déploiement, un rapport signale toute configuration bloquante (code de sortie 2). Le détail est dans `DEPLOY_GCP.md`.

**Vérifié hors ligne** avec un faux `gcloud`, sur trois scénarios : configuration de production existante conservée, premier déploiement en mode KYC manuel, et clé Didit absente (rapport bloquant).

---

# Actions requises au déploiement

1. **Secrets des webhooks** (à créer dans Secret Manager, puis à ajouter dans `--set-secrets` de `scripts/deploy-gcp.sh`) : `PAYMENTS_WEBHOOK_SECRET`, `CAAS_WEBHOOK_SECRET`, `NOMBA_WEBHOOK_SECRET`, `NILOS_WEBHOOK_SECRET` et, si `KYC_PROVIDER=metamap`, `METAMAP_WEBHOOK_SECRET`. Tant qu'un secret manque, le webhook correspondant répond `503 WEBHOOK_SECRET_MISSING` en nommant la variable. Le fournisseur réessaie, donc aucun dépôt n'est perdu une fois le secret ajouté.
2. **`TRUSTED_PROXY_HOPS`** : la valeur par défaut est `2` (proxy Next.js + Cloud Run). Si un load balancer est ajouté devant, il faut augmenter cette valeur.
3. **`CORS_ALLOWED_ORIGINS`** : à laisser vide, sauf si un autre site doit appeler l'API directement depuis le navigateur.
4. **Frontend** : si le site est servi sous plusieurs domaines, il faut tous les lister dans `ALLOWED_ORIGINS`, sinon le proxy répondra `403 CSRF_REJECTED`.
5. **Après le déploiement, les utilisateurs devront se reconnecter une fois** si leurs jetons ont été émis avant le correctif et que leur session n'est plus active.

## Checklist de recette

- [ ] Connexion avec 2FA : le jeton « en attente » ne donne accès à aucune route (401).
- [ ] 6 PIN faux de suite : réponse `429`, puis le bon PIN reste refusé pendant 15 min.
- [ ] Déconnexion : l'ancien jeton d'accès reçoit immédiatement `401`.
- [ ] Désactivation d'un client par un admin : ses requêtes reçoivent immédiatement `403 account is inactive`.
- [ ] `GET /accounts/XAF/transactions/<id d'un autre client>` : `404`.
- [ ] `POST /wallets/seed` sans PIN : `403 STEP_UP_REQUIRED` ; avec le bon PIN : `200`.
- [ ] `POST /api/proxy` depuis un autre domaine : `403 CSRF_REJECTED`.
- [ ] Le site ne s'affiche pas dans une iframe d'un autre domaine.
- [ ] Le KYC Didit (iframe + caméra) fonctionne toujours avec la CSP.
- [ ] `/swagger/index.html` en production : `404`.
- [ ] Webhook sans secret configuré : `503 WEBHOOK_SECRET_MISSING`, qui nomme la variable.

## Limites résiduelles connues

| Limite | Pourquoi | Piste |
|--------|----------|-------|
| Si Redis est indisponible, le verrouillage anti brute-force laisse passer | Pour ne pas bloquer toutes les connexions | Alerte sur le log `auth lockout check failed` |
| Un tiers peut verrouiller le login d'une victime pendant 15 min | Effet inhérent au verrouillage par compte | Ajouter un CAPTCHA après 3 échecs |
| `ForgotPIN`/`ResetPIN` peuvent renvoyer `429` uniquement pour un compte existant (après 5 essais) | Les compteurs sont liés au compte | Indexer aussi les compteurs sur l'identifiant saisi |
| CSP avec `'unsafe-inline'` pour les scripts | Contrainte de Next.js sans nonces | Passer aux nonces via le middleware Next.js |
| Les secrets des webhooks Nilos et CaaS acceptent plusieurs en-têtes et formats de signature | Schéma du fournisseur non confirmé | Restreindre au format officiel dès qu'il est confirmé |

---

## Liste des fichiers modifiés

### Lot 1 (backend)
```
internal/pkg/token/token.go                       (sujets de jetons, HS256 uniquement)
internal/api/middleware/auth.go                   (accepte uniquement les jetons "access")
internal/services/auth_attempts.go                (NOUVEAU — verrouillage Redis, OTP crypto)
internal/services/auth_service.go                 (verrouillage + limites d'envoi + crypto/rand)
internal/api/handlers/auth_handler.go             (réponses 429)
internal/pkg/response/response.go                 (helpers TooManyRequests, WebhookSecretMissing, WebhookSignatureInvalid)
internal/api/handlers/payments_webhook.go         (fail-closed)
internal/api/handlers/caas_webhook.go             (fail-closed)
internal/api/handlers/nomba_webhook.go            (503 si non configuré)
internal/api/handlers/nilos_webhook.go            (503 si non configuré)
internal/services/nomba_service.go                (fail-closed, ErrWebhookNotConfigured)
internal/services/nilos_service.go                (fail-closed)
internal/api/routes.go                            (alerte au démarrage si secret manquant)
internal/services/upload_service.go               (ReadURLForUser, contrôle d'appartenance)
internal/api/handlers/upload_handler.go           (utilise ReadURLForUser, 404)
internal/api/handlers/security_fixes_test.go      (NOUVEAU — tests jetons + erreur explicite des webhooks)
internal/services/security_fixes_test.go          (NOUVEAU — tests IDOR et OTP)
```

### Lot 2 — backend (`digitalcapfx-backend/`)
```
internal/api/middleware/auth.go                   (ActiveSession — R4)
internal/api/middleware/security.go               (NOUVEAU — ClientIP R11, SecurityHeaders R7)
internal/api/routes.go                            (ClientIP, SecurityHeaders, CORS R6, Swagger R10, ActiveSession, POST /wallets/seed)
internal/config/config.go                         (CORS_ALLOWED_ORIGINS, SWAGGER_ENABLED, TRUSTED_PROXY_HOPS)
internal/services/account_service.go              (contrôle de propriété — R2)
internal/api/handlers/account_handler.go          (R2)
internal/services/auth_attempts.go                (VerifyStepUp — R3)
internal/api/handlers/wallet_handler.go           (réauthentification seed / export-key — R3)
internal/services/auth_service.go                 (refresh : compte actif — R4 ; OTP non loggés, téléphones masqués — R9)
internal/services/kyc_service.go                  (VerifyMetaMapWebhook — R5)
internal/api/handlers/kyc_handler.go              (signature MetaMap — R5)
internal/kyc/metamap_provider.go                  (fail-closed + temps constant — R5)
internal/api/handlers/auth_handler.go             (anti-énumération R8 ; realIP R11)
internal/pkg/redact/redact.go                     (NOUVEAU — masquage des téléphones R9)
internal/api/handlers/webhook_handler.go, internal/services/{hub2,crypto,withdrawal}_service.go  (logs masqués — R9)
.env.example                                      (nouvelles variables de sécurité)
internal/api/middleware/security_test.go, internal/pkg/redact/redact_test.go, internal/services/security_fixes_test.go  (tests)
```

### Lot 2 — frontend (`digitalcapfx-frontend/`)
```
src/app/api/_lib/security.ts                      (NOUVEAU — anti-CSRF, cookies, IP client, erreurs génériques)
src/app/api/proxy/route.ts                        (R0, R1, R4 cookies, R9, R11)
src/app/api/upload/route.ts                       (R1, R9)
next.config.ts                                    (en-têtes de sécurité — R7)
src/services/webhook.service.ts                   (SUPPRIMÉ — déclenchait des webhooks depuis le navigateur)
```

## Commandes de vérification

```bash
cd digitalcapfx-backend
go build ./... && go vet ./internal/... && go test ./...

cd ../digitalcapfx-frontend
npx tsc --noEmit && npx next build
```
