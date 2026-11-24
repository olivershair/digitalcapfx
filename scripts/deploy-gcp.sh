#!/usr/bin/env bash
set -Eeuo pipefail

PROJECT_ID="${PROJECT_ID:-digitalcap-fx-501314}"
REGION="${REGION:-europe-west2}"
REPOSITORY="${REPOSITORY:-digitalcapfx}"
INSTANCE="${INSTANCE:-digitalcapfx-db}"
BACKEND_SERVICE="${BACKEND_SERVICE:-digitalcapfx-backend}"
FRONTEND_SERVICE="${FRONTEND_SERVICE:-digitalcapfx-frontend}"
MIGRATE_JOB="${MIGRATE_JOB:-digitalcapfx-migrate}"
RUNTIME_SA="${RUNTIME_SA:-digitalcapfx-dev@${PROJECT_ID}.iam.gserviceaccount.com}"
VPC_NETWORK="${VPC_NETWORK:-default}"
VPC_SUBNET="${VPC_SUBNET:-default}"
UPLOADS_BUCKET="${UPLOADS_BUCKET:-}"
HUB2_MODE="${HUB2_MODE:-sandbox}"

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

die() {
  printf 'Erreur : %s\n' "$*" >&2
  exit 1
}

command -v gcloud >/dev/null 2>&1 || die "gcloud CLI est requis."
command -v python3 >/dev/null 2>&1 || die "python3 est requis (lecture de la configuration Cloud Run actuelle)."
[[ "$REGION" == "europe-west2" ]] ||
  die "La configuration Cloud Build backend est fixée à europe-west2."
[[ "$REPOSITORY" == "digitalcapfx" ]] ||
  die "La configuration Cloud Build backend utilise le dépôt digitalcapfx."
[[ "$HUB2_MODE" == "sandbox" || "$HUB2_MODE" == "production" ]] ||
  die "HUB2_MODE doit valoir sandbox ou production."

ACTIVE_ACCOUNT="$(gcloud auth list --filter='status:ACTIVE' --format='value(account)' 2>/dev/null)"
[[ -n "$ACTIVE_ACCOUNT" ]] || die "Aucun compte gcloud actif. Lancez d'abord gcloud auth login."

ACTIVE_PROJECT="$(gcloud config get-value project 2>/dev/null)"
[[ "$ACTIVE_PROJECT" == "$PROJECT_ID" ]] ||
  die "Le projet gcloud actif est '$ACTIVE_PROJECT', mais PROJECT_ID vaut '$PROJECT_ID'."

printf 'Ce script va construire les images, exécuter les migrations et déployer\n'
printf 'des services Cloud Run dans le projet %s (%s).\n' "$PROJECT_ID" "$REGION"
printf 'Ces opérations peuvent entraîner des frais. Pour continuer, tapez exactement le PROJECT_ID : '
read -r CONFIRMATION
[[ "$CONFIRMATION" == "$PROJECT_ID" ]] || die "Confirmation incorrecte ; aucune opération lancée."

if ! SA_OUTPUT="$(gcloud iam service-accounts describe "$RUNTIME_SA" \
  --project="$PROJECT_ID" 2>&1)"; then
  printf '%s\n' "$SA_OUTPUT" >&2
  die "Impossible de vérifier le compte de service $RUNTIME_SA. Vérifiez qu'il existe et que votre compte peut le consulter."
fi

if [[ "$ACTIVE_ACCOUNT" == *@*.gserviceaccount.com ]]; then
  DEPLOYER_MEMBER="serviceAccount:${ACTIVE_ACCOUNT}"
else
  DEPLOYER_MEMBER="user:${ACTIVE_ACCOUNT}"
fi

printf '\nVérification du droit d’utiliser le compte de service Cloud Run...\n'
if ! IAM_OUTPUT="$(gcloud iam service-accounts add-iam-policy-binding "$RUNTIME_SA" \
  --project="$PROJECT_ID" \
  --member="$DEPLOYER_MEMBER" \
  --role=roles/iam.serviceAccountUser 2>&1)"; then
  printf '%s\n' "$IAM_OUTPUT" >&2
  printf 'Un administrateur IAM doit accorder ce droit une fois :\n' >&2
  printf 'gcloud iam service-accounts add-iam-policy-binding "%s" --project="%s" --member="%s" --role=roles/iam.serviceAccountUser\n' \
    "$RUNTIME_SA" "$PROJECT_ID" "$DEPLOYER_MEMBER" >&2
  die "Droit iam.serviceaccounts.actAs manquant pour le compte de déploiement."
fi

printf '\nVérification des droits du compte d’exécution...\n'
if ! IAM_OUTPUT="$(gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member="serviceAccount:${RUNTIME_SA}" \
  --role=roles/cloudsql.client \
  --quiet 2>&1)"; then
  printf '%s\n' "$IAM_OUTPUT" >&2
  printf 'Un administrateur IAM doit accorder l’accès Cloud SQL :\n' >&2
  printf 'gcloud projects add-iam-policy-binding "%s" --member="serviceAccount:%s" --role=roles/cloudsql.client\n' \
    "$PROJECT_ID" "$RUNTIME_SA" >&2
  die "Impossible d’accorder roles/cloudsql.client au compte d’exécution."
fi

ensure_secret() {
  local secret_name="$1"
  local describe_output

  if ! describe_output="$(gcloud secrets describe "$secret_name" \
    --project="$PROJECT_ID" 2>&1)"; then
    if [[ "$describe_output" != *"not found"* && "$describe_output" != *"NOT_FOUND"* ]]; then
      printf '%s\n' "$describe_output" >&2
      die "Impossible de vérifier le secret ${secret_name}."
    fi

    printf 'Secret %s absent. Saisissez sa valeur (entrée masquée) : ' "$secret_name"
    local secret_value
    IFS= read -r -s secret_value
    printf '\n'
    [[ -n "$secret_value" ]] || die "La valeur du secret ${secret_name} ne peut pas être vide."

    if ! IAM_OUTPUT="$(gcloud secrets create "$secret_name" \
      --project="$PROJECT_ID" \
      --replication-policy=automatic 2>&1)"; then
      printf '%s\n' "$IAM_OUTPUT" >&2
      unset secret_value
      die "Impossible de créer le secret ${secret_name}."
    fi

    if ! printf '%s' "$secret_value" | gcloud secrets versions add "$secret_name" \
      --project="$PROJECT_ID" \
      --data-file=- >/dev/null; then
      unset secret_value
      die "Secret ${secret_name} créé, mais impossible d'ajouter sa première version."
    fi
    unset secret_value
  fi

  if ! IAM_OUTPUT="$(gcloud secrets add-iam-policy-binding "$secret_name" \
    --project="$PROJECT_ID" \
    --member="serviceAccount:${RUNTIME_SA}" \
    --role=roles/secretmanager.secretAccessor \
    --quiet 2>&1)"; then
    printf '%s\n' "$IAM_OUTPUT" >&2
    printf 'Un administrateur IAM doit accorder l’accès à ce secret :\n' >&2
    printf 'gcloud secrets add-iam-policy-binding "%s" --project="%s" --member="serviceAccount:%s" --role=roles/secretmanager.secretAccessor\n' \
      "$secret_name" "$PROJECT_ID" "$RUNTIME_SA" >&2
    die "Impossible d’accorder l’accès au secret ${secret_name}."
  fi
}

# ─── Configuration du backend ────────────────────────────────────────────────
# IMPORTANT : le déploiement utilise --update-env-vars / --update-secrets. Les
# variables déjà configurées sur le service (console, déploiements précédents)
# sont CONSERVÉES ; ce script ne fait qu'ajouter ou mettre à jour.
# (Avant, --set-env-vars / --set-secrets effaçaient tout le reste à chaque
# déploiement — notamment BREVO_API_KEY, ce qui bloquait l'envoi du code SMS
# et donc toute inscription.)

# Secrets indispensables : créés à la demande s'ils n'existent pas.
#   BREVO_API_KEY = envoi des codes SMS → sans lui, personne ne peut s'inscrire.
REQUIRED_SECRETS=(DATABASE_URL REDIS_URL JWT_SECRET
  PAYMENTS_API_KEY CAAS_API_KEY HUB2_API_KEY HUB2_SECRET_KEY
  BREVO_API_KEY)

# Secrets facultatifs : montés s'ils existent dans Secret Manager, sinon un
# avertissement explicite indique la fonctionnalité concernée.
OPTIONAL_SECRETS=(BREVO_SMTP_USER BREVO_SMTP_KEY
  PAYMENTS_WEBHOOK_SECRET CAAS_WEBHOOK_SECRET NOMBA_WEBHOOK_SECRET NILOS_WEBHOOK_SECRET
  NOMBA_CLIENT_ID NOMBA_CLIENT_SECRET NILOS_API_KEY NILOS_API_SECRET
  DIDIT_API_KEY DIDIT_WEBHOOK_SECRET
  SUMSUB_APP_TOKEN SUMSUB_SECRET_KEY SUMSUB_WEBHOOK_SECRET
  METAMAP_CLIENT_ID METAMAP_CLIENT_SECRET METAMAP_WEBHOOK_SECRET
  FIREBASE_CREDENTIALS_JSON)

# Variables non secrètes transmises si elles sont définies dans le shell au
# lancement du script (ex. KYC_PROVIDER=manual ADMIN_NOTIFY_EMAIL=… ./deploy-gcp.sh).
PASSTHROUGH_ENV=(KYC_PROVIDER ADMIN_NOTIFY_EMAIL APP_BASE_URL OWNER_PHONES
  BREVO_FROM_EMAIL BREVO_FROM_NAME BREVO_SMS_SENDER
  DIDIT_WORKFLOW_ID DIDIT_CALLBACK_URL SUMSUB_LEVEL_NAME METAMAP_FLOW_ID
  NOMBA_ACCOUNT_ID NOMBA_BASE_URL NILOS_ORG_ID NILOS_BASE_URL GOOGLE_CLIENT_ID
  CORS_ALLOWED_ORIGINS SWAGGER_ENABLED TRUSTED_PROXY_HOPS DEFAULT_COUNTRY_CODE
  MAINTENANCE_MODE MAINTENANCE_MESSAGE SMS_UNSUPPORTED_CALLING_CODES)

secret_exists() {
  gcloud secrets describe "$1" --project="$PROJECT_ID" >/dev/null 2>&1
}

grant_secret_access() {
  local secret_name="$1"
  if ! IAM_OUTPUT="$(gcloud secrets add-iam-policy-binding "$secret_name" \
    --project="$PROJECT_ID" \
    --member="serviceAccount:${RUNTIME_SA}" \
    --role=roles/secretmanager.secretAccessor \
    --quiet 2>&1)"; then
    printf '%s\n' "$IAM_OUTPUT" >&2
    die "Impossible d’accorder l’accès au secret ${secret_name}."
  fi
}

# Lit les variables actuellement configurées sur un service Cloud Run.
# Affiche une ligne par variable : "plain NOM" ou "secret NOM" (jamais de valeur,
# sauf KYC_PROVIDER, non sensible, pour le rapport final).
service_env() {
  local json
  json="$(gcloud run services describe "$1" --project="$PROJECT_ID" --region="$REGION" \
    --format=json 2>/dev/null)" || return 0
  printf '%s' "$json" | python3 -c '
import json, sys
d = json.load(sys.stdin)
cs = d.get("spec", {}).get("template", {}).get("spec", {}).get("containers", []) or []
for e in (cs[0].get("env", []) if cs else []) or []:
    kind = "secret" if "valueFrom" in e else "plain"
    extra = " " + e.get("value", "") if (kind == "plain" and e["name"] == "KYC_PROVIDER") else ""
    print(kind + " " + e["name"] + extra)
'
}

CURRENT_BACKEND_ENV="$(service_env "$BACKEND_SERVICE")"
is_plain_on_service() { grep -qxE "plain $1( .*)?" <<<"$CURRENT_BACKEND_ENV"; }
is_secret_on_service() { grep -qx "secret $1" <<<"$CURRENT_BACKEND_ENV"; }

for SECRET_NAME in "${REQUIRED_SECRETS[@]}"; do
  # Déjà fourni en clair sur le service : conservé (voir l'avertissement plus bas).
  is_plain_on_service "$SECRET_NAME" && continue
  ensure_secret "$SECRET_NAME"
done

BACKEND_SECRETS=()
WARNINGS=()
MISSING_OPTIONAL=()
for SECRET_NAME in "${REQUIRED_SECRETS[@]}" "${OPTIONAL_SECRETS[@]}"; do
  if is_plain_on_service "$SECRET_NAME"; then
    # Cloud Run refuse de transformer une variable en clair en secret : on la
    # conserve telle quelle (rien n'est perdu) et on signale la migration à faire.
    WARNINGS+=("${SECRET_NAME} est défini EN CLAIR sur le service : conservé tel quel. Recommandé : le migrer vers Secret Manager.")
    continue
  fi
  if secret_exists "$SECRET_NAME"; then
    grant_secret_access "$SECRET_NAME"
    BACKEND_SECRETS+=("${SECRET_NAME}=${SECRET_NAME}:latest")
  elif ! is_secret_on_service "$SECRET_NAME"; then
    MISSING_OPTIONAL+=("$SECRET_NAME")
  fi
done
if ((${#MISSING_OPTIONAL[@]})); then
  WARNINGS+=("Secrets facultatifs non configurés (normal s'ils ne servent pas) : ${MISSING_OPTIONAL[*]}")
fi

INSTANCE_CONNECTION_NAME="${INSTANCE_CONNECTION_NAME:-$(gcloud sql instances describe "$INSTANCE" \
  --project="$PROJECT_ID" \
  --format='value(connectionName)')}"
[[ -n "$INSTANCE_CONNECTION_NAME" ]] ||
  die "Impossible de récupérer le nom de connexion de l'instance Cloud SQL '$INSTANCE'."

BACKEND_IMAGE="${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPOSITORY}/server:latest"
MIGRATE_IMAGE="${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPOSITORY}/migrate:latest"
FRONTEND_IMAGE="${REGION}-docker.pkg.dev/${PROJECT_ID}/${REPOSITORY}/frontend:latest"

printf '\n[1/5] Construction et publication du backend et de l’image de migration...\n'
gcloud builds submit \
  --project="$PROJECT_ID" \
  --config=digitalcapfx-backend/cloudbuild.deploy.yaml \
  digitalcapfx-backend

printf '\n[2/5] Construction et publication du frontend...\n'
gcloud builds submit \
  --project="$PROJECT_ID" \
  --tag="$FRONTEND_IMAGE" \
  digitalcapfx-frontend

printf '\n[3/5] Exécution des migrations PostgreSQL...\n'
gcloud run jobs deploy "$MIGRATE_JOB" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --image="$MIGRATE_IMAGE" \
  --service-account="$RUNTIME_SA" \
  --set-cloudsql-instances="$INSTANCE_CONNECTION_NAME" \
  --set-secrets="DATABASE_URL=DATABASE_URL:latest" \
  --command=/bin/sh \
  --args=-c,'migrate -path=/migrations -database="$DATABASE_URL" up' \
  --quiet
gcloud run jobs execute "$MIGRATE_JOB" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --wait

printf '\n[4/5] Déploiement du backend...\n'
# Liste avec le délimiteur "|" (syntaxe ^|^ de gcloud) pour accepter des
# valeurs contenant des virgules (ex. CORS_ALLOWED_ORIGINS, OWNER_PHONES) ou des « @ » (e-mails).
BACKEND_ENV_LIST=("ENV=production" "GCP_PROJECT_ID=${PROJECT_ID}"
  "PAYMENTS_API_URL=https://payments.rach.finance" "CAAS_API_URL=https://caas.rach.finance"
  "HUB2_BASE_URL=https://api.hub2.io" "HUB2_MODE=${HUB2_MODE}")
if [[ -n "$UPLOADS_BUCKET" ]]; then
  BACKEND_ENV_LIST+=("UPLOADS_BUCKET=${UPLOADS_BUCKET}" "GCP_SIGNER_SA=${RUNTIME_SA}")
fi
for VAR_NAME in "${PASSTHROUGH_ENV[@]}"; do
  if [[ -n "${!VAR_NAME:-}" ]]; then
    if is_secret_on_service "$VAR_NAME"; then
      WARNINGS+=("${VAR_NAME} est un secret sur le service : la valeur du shell est ignorée.")
      continue
    fi
    [[ "${!VAR_NAME}" != *'|'* ]] || die "La valeur de ${VAR_NAME} ne doit pas contenir '|' (délimiteur interne)."
    BACKEND_ENV_LIST+=("${VAR_NAME}=${!VAR_NAME}")
  fi
done
BACKEND_ENV_VARS="^|^$(IFS='|'; printf '%s' "${BACKEND_ENV_LIST[*]}")"
BACKEND_SECRET_ARGS=()
if ((${#BACKEND_SECRETS[@]})); then
  BACKEND_SECRET_ARGS=("--update-secrets=$(IFS=,; printf '%s' "${BACKEND_SECRETS[*]}")")
fi

gcloud run deploy "$BACKEND_SERVICE" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --image="$BACKEND_IMAGE" \
  --service-account="$RUNTIME_SA" \
  --add-cloudsql-instances="$INSTANCE_CONNECTION_NAME" \
  --network="$VPC_NETWORK" \
  --subnet="$VPC_SUBNET" \
  --vpc-egress=all-traffic \
  --update-env-vars="$BACKEND_ENV_VARS" \
  ${BACKEND_SECRET_ARGS[@]+"${BACKEND_SECRET_ARGS[@]}"} \
  --allow-unauthenticated \
  --quiet

BACKEND_URL="$(gcloud run services describe "$BACKEND_SERVICE" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --format='value(status.url)')"
[[ -n "$BACKEND_URL" ]] || die "Le backend a été déployé, mais son URL n'a pas pu être récupérée."
BACKEND_API_URL="${BACKEND_API_URL:-${BACKEND_URL}/api/v1}"
BACKEND_API_URL="${BACKEND_API_URL%/}"
[[ "$BACKEND_API_URL" == https://* ]] ||
  die "BACKEND_API_URL doit être une URL HTTPS (par exemple https://api.digitalcapfx.com)."
if [[ "$BACKEND_API_URL" != */api/v1 ]]; then
  BACKEND_API_URL="${BACKEND_API_URL}/api/v1"
fi

printf '\n[5/5] Déploiement du frontend...\n'
gcloud run deploy "$FRONTEND_SERVICE" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --image="$FRONTEND_IMAGE" \
  --service-account="$RUNTIME_SA" \
  --update-env-vars="BACKEND_API_URL=${BACKEND_API_URL}" \
  --allow-unauthenticated \
  --quiet

FRONTEND_URL="$(gcloud run services describe "$FRONTEND_SERVICE" \
  --project="$PROJECT_ID" \
  --region="$REGION" \
  --format='value(status.url)')"

printf '\nDéploiement terminé.\nFrontend : %s\nBackend Cloud Run : %s\nAPI utilisée par le frontend : %s\n' \
  "$FRONTEND_URL" "$BACKEND_URL" "$BACKEND_API_URL"

# ─── Vérification de la configuration réellement déployée ───────────────────
DEPLOYED_ENV="$(service_env "$BACKEND_SERVICE")"
has_var() { grep -qE "^(plain|secret) $1( |$)" <<<"$DEPLOYED_ENV"; }
KYC_PROVIDER_DEPLOYED="$(sed -n 's/^plain KYC_PROVIDER //p' <<<"$DEPLOYED_ENV")"
KYC_PROVIDER_DEPLOYED="${KYC_PROVIDER_DEPLOYED:-didit}"

BLOCKING=()
has_var BREVO_API_KEY || BLOCKING+=("BREVO_API_KEY manquant → les codes SMS ne partent pas : AUCUNE inscription ni réinitialisation de PIN par SMS possible.")
case "$KYC_PROVIDER_DEPLOYED" in
  didit)   has_var DIDIT_API_KEY || BLOCKING+=("KYC_PROVIDER=didit mais DIDIT_API_KEY manquant → la vérification d'identité ne peut pas démarrer (ou passez KYC_PROVIDER=manual).") ;;
  sumsub)  has_var SUMSUB_APP_TOKEN && has_var SUMSUB_SECRET_KEY || BLOCKING+=("KYC_PROVIDER=sumsub mais SUMSUB_APP_TOKEN / SUMSUB_SECRET_KEY manquants → KYC impossible.") ;;
  metamap) has_var METAMAP_CLIENT_ID || BLOCKING+=("KYC_PROVIDER=metamap mais METAMAP_CLIENT_ID manquant → KYC impossible.") ;;
esac

INFO=()
has_var BREVO_SMTP_KEY || INFO+=("BREVO_SMTP_KEY manquant → aucun e-mail (bienvenue, vérification d'e-mail, alertes de connexion, décisions KYC).")
has_var ADMIN_NOTIFY_EMAIL || INFO+=("ADMIN_NOTIFY_EMAIL non défini → l'équipe n'est pas prévenue des nouvelles inscriptions ni des dossiers KYC à examiner.")
for W in PAYMENTS_WEBHOOK_SECRET CAAS_WEBHOOK_SECRET NOMBA_WEBHOOK_SECRET NILOS_WEBHOOK_SECRET; do
  has_var "$W" || INFO+=("${W} manquant → ce webhook répond 503 WEBHOOK_SECRET_MISSING (dépôts non crédités tant qu'il manque).")
done

printf '\nKYC : %s\n' "$KYC_PROVIDER_DEPLOYED"
if ((${#WARNINGS[@]})); then
  printf '\nAvertissements :\n'; printf '  - %s\n' "${WARNINGS[@]}"
fi
if ((${#INFO[@]})); then
  printf '\nFonctionnalités dégradées :\n'; printf '  - %s\n' "${INFO[@]}"
fi
if ((${#BLOCKING[@]})); then
  printf '\n❌ CONFIGURATION BLOQUANTE :\n' >&2; printf '  - %s\n' "${BLOCKING[@]}" >&2
  exit 2
fi
printf '\n✅ Configuration critique OK : l’inscription et la vérification d’identité peuvent aller jusqu’au bout.\n'
