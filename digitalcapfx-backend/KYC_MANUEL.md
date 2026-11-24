# KYC manuel — activation et procédure

Le mode manuel permet de vérifier l'identité des clients **sans fournisseur KYC** (Didit, Sumsub, MetaMap). Le client téléverse sa pièce d'identité et un selfie où il la tient, puis soumet son dossier. Un membre de l'équipe conformité l'approuve ou le rejette.

## 1. Activer ou désactiver le mode manuel

Le mode se choisit avec une seule variable d'environnement du backend :

| Valeur de `KYC_PROVIDER` | Effet |
|---|---|
| `manual` | KYC manuel (revue par le staff) |
| `didit` (par défaut), `sumsub`, `metamap` | Vérification automatique par le fournisseur |

**Sur Cloud Run, sans reconstruire l'image :**
```bash
gcloud run services update <service-backend> --region <région> --update-env-vars KYC_PROVIDER=manual
# retour au fournisseur :
gcloud run services update <service-backend> --region <région> --update-env-vars KYC_PROVIDER=didit
```

Au démarrage, l'API écrit dans les logs : `KYC running in MANUAL mode`.

**Recommandé :** définir `ADMIN_NOTIFY_EMAIL` (par exemple l'adresse de l'équipe conformité). Cette adresse reçoit un e-mail à chaque nouveau dossier à examiner.

Aucun changement n'est nécessaire côté site web : il détecte le mode automatiquement (`verification_mode` dans `GET /kyc/status` et `GET /kyc/requirements`).

## 2. Parcours du client (site web → Paramètres → Vérification)

1. **Formulaire** : identité, adresse et informations financières (inchangé).
2. **Documents** : en plus des documents habituels, le mode manuel demande :
   - `id_document` — pièce d'identité, recto (**obligatoire**) : CNI, passeport, permis ou titre de séjour, en cours de validité ;
   - `id_document_back` — verso (facultatif, utile pour une CNI ou un permis) ;
   - `selfie_with_id` — selfie en tenant la même pièce à côté du visage (**obligatoire**).

   Pour un compte entreprise (KYB), le selfie remplace le contrôle de vivacité du dirigeant, et tous les documents KYB obligatoires sont exigés.
3. **Vérifier et soumettre** : le client voit la liste de ce qui est fait et de ce qui manque, puis clique sur **« Soumettre mon dossier »**.
   - Si un document obligatoire manque, la soumission est refusée (`400 KYC_DOCUMENTS_MISSING`) avec la liste des documents manquants.
   - Sinon, le statut passe à « en cours d'examen ». Le client reçoit une notification, et l'équipe un e-mail.
4. Une fois le dossier soumis, il est **verrouillé** : le client ne peut plus modifier ses documents (`409 KYC_FILE_LOCKED`) jusqu'à la décision.

## 3. Procédure de revue (staff)

Permissions nécessaires : `kyc:view` pour consulter, `kyc:approve` et `kyc:reject` pour décider.

| Action | Requête |
|---|---|
| Dossiers en attente | `GET /api/v1/admin/kyc/pending` |
| Ouvrir un dossier | `GET /api/v1/admin/kyc/{user_id}` |
| Approuver | `POST /api/v1/admin/kyc/{user_id}/approve` |
| Rejeter | `POST /api/v1/admin/kyc/{user_id}/reject` avec le corps `{"reason": "…"}` |

Le dossier (`GET /admin/kyc/{user_id}`) contient :
- `verification_mode: "manual"` et `identity_status` ;
- `intake` : les informations saisies par le client ;
- `documents[]` : chaque document avec un **`view_url` valable 15 minutes**. Il suffit de rouvrir le dossier pour obtenir un nouveau lien. Si `owned_by_user` vaut `false`, le fichier ne provient pas des uploads de ce client : aucun lien n'est fourni, et le dossier est à traiter comme suspect.

**Points de contrôle conseillés :**
- La pièce est en cours de validité et lisible, et ses quatre coins sont visibles.
- Le nom et la date de naissance de la pièce correspondent au formulaire et au compte.
- Le visage du selfie correspond à la photo de la pièce, et c'est bien la **même** pièce qui est tenue.
- Aucun signe de retouche : polices irrégulières, photo collée, capture d'écran d'un autre écran.
- Le justificatif de domicile date de moins de 3 mois (si fourni).
- En cas de doute : rejeter avec un motif précis, ou demander un appel vidéo.

**Décisions :**
- **Approuver** : l'accès aux fonctions financières est débloqué immédiatement, et les comptes fiat (EUR, GBP, USD, NGN) sont provisionnés. Le client reçoit un e-mail et une notification.
- **Rejeter** : le client reçoit le motif, son dossier repasse en « à corriger », et il peut remplacer ses documents puis soumettre à nouveau. Le motif doit donc être clair et actionnable (par exemple « Selfie flou, la pièce n'est pas lisible »).

Chaque décision est enregistrée avec l'identifiant de l'admin (`kyc_admin_actions`).

## 4. Sécurité

- Un client ne peut joindre à son dossier que des fichiers qu'il a lui-même téléversés (`400 INVALID_DOCUMENT` sinon).
- Les liens de consultation du staff expirent après 15 minutes. Les fichiers restent privés dans le bucket.
- En mode manuel, les webhooks des fournisseurs KYC ne peuvent pas modifier un statut.
- Limite assumée : il n'y a pas de détection automatique de vivacité ni de faux documents. Pour un volume important ou des profils à risque, il faut prévoir un fournisseur automatique ou un appel vidéo.

## 5. Fichiers concernés (pour les développeurs)

| Fichier | Rôle |
|---|---|
| `internal/kyc/manual_provider.go` | Fournisseur « manual » (pas de session, webhooks refusés) |
| `internal/services/kyc_manual.go` | Soumission, contrôle des documents, notifications, liens staff, décision → « à corriger » |
| `internal/services/kyc_service.go` | Exigences selon le mode, verrouillage des uploads, vue admin enrichie |
| `internal/services/upload_service.go` | `OwnedObject` : résolution et contrôle d'appartenance des références de fichiers |
| `internal/api/handlers/kyc_handler.go` | `POST /kyc/init` → `202 {mode:"manual"}` ou `400 KYC_DOCUMENTS_MISSING` |
| `digitalcapfx-frontend/.../verification/ManualReviewStep.tsx` | Étape « Vérifier et soumettre » |
| `digitalcapfx-frontend/.../VerificationTab.tsx` | Détection du mode, soumission explicite (pas de lancement automatique) |
| `internal/services/kyc_manual_test.go` | Tests du mode manuel |
