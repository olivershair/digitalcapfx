# DigitalFX — Présentation à l’attention du personnel de la BCEA

## En bref

DigitalFX est une plateforme financière numérique destinée à permettre à ses utilisateurs — particuliers et entreprises — d’accéder, depuis une interface web, à des services de portefeuille, de paiements et de change. Elle réunit une application web, une API et des intégrations avec des prestataires spécialisés pour le KYC, les paiements, les comptes et les actifs numériques.

La plateforme est conçue pour faciliter les opérations financières et leur suivi, tout en prévoyant des contrôles d’identité, des limites et des fonctions d’administration. Son périmètre et ses fonctionnalités devront être présentés à la BCEA selon les services effectivement autorisés et les partenaires agréés impliqués.

## À quoi sert l’application ?

Selon les fonctionnalités présentes dans le code, l’application prévoit notamment :

- l’inscription des particuliers et des entreprises ;
- la collecte et le suivi des vérifications d’identité (KYC) et d’entreprise (KYB) ;
- la consultation de portefeuilles et d’historiques d’opérations ;
- des fonctions de change entre devises ;
- des dépôts et retraits, notamment via des intégrations de paiement et de monnaie mobile ;
- des comptes virtuels en EUR, GBP et USD via un prestataire ;
- des fonctions liées aux portefeuilles crypto et aux échanges d’actifs numériques ;
- des outils d’administration pour gérer les accès, les limites et certains contrôles opérationnels.

Ces fonctionnalités dépendent en partie de prestataires externes. DigitalFX ne doit donc pas être présentée comme une banque ni comme l’émetteur des comptes ou des services fournis par ces partenaires.

## Modèle économique envisagé

Le code laisse entrevoir plusieurs sources possibles de revenus :

- **marge de change**, au moyen de taux d’achat et de vente distincts ;
- **frais sur certaines opérations**, par exemple des retraits ou des opérations liées aux actifs numériques ;
- **offres destinées aux entreprises et aux commerçants**, avec gestion d’équipes ou de comptes professionnels.

Les tarifs, commissions, partage de revenus avec les partenaires et conditions applicables aux clients restent à formaliser et à valider. Ils devront être transparents, communiqués avant confirmation de l’opération et conformes aux exigences réglementaires applicables.

## Langages et technologies

- **Go** : langage du serveur API. Il est adapté aux services réseau, aux traitements concurrents et à la création de binaires conteneurisés.
- **TypeScript et Next.js** : technologies de l’application web. TypeScript apporte un typage utile pour fiabiliser les échanges entre l’interface et l’API ; Next.js structure l’application côté web.
- **SQL et PostgreSQL** : stockage des données relationnelles, notamment celles nécessaires aux comptes, opérations et contrôles métier.
- **Redis** : composant complémentaire utilisé par le serveur pour certaines fonctions de données temporaires ou de coordination.

Ces choix séparent l’interface, les règles métier et le stockage, ce qui facilite leur maintenance et leur évolution.

## Serveurs et rôle de chaque composant

L’architecture décrite par le dépôt comprend :

1. **Application web** — interface utilisée par les clients et les équipes d’administration. Elle est développée avec Next.js ; son hébergement de production doit être confirmé dans le dossier de déploiement.
2. **Serveur API** — application Go qui reçoit les requêtes, applique les règles métier et communique avec la base de données et les prestataires externes.
3. **Base PostgreSQL** — stockage persistant des données de l’application. Les migrations de schéma sont prévues pour être exécutées par un job dédié.
4. **Redis** — service complémentaire utilisé par l’API.
5. **Stockage de documents** — l’intégration Google Cloud Storage est prévue pour les fichiers, notamment ceux liés aux parcours de vérification.
6. **Services partenaires** — les fonctions KYC, paiements, comptes et actifs numériques s’appuient sur des prestataires externes. Leur disponibilité et leurs délais influencent le service final.

Le dépôt contient une configuration de construction d’images vers Google Artifact Registry et des références à Cloud Run. Cela ne suffit toutefois pas à confirmer à lui seul la configuration complète de production, la région d’hébergement, la redondance ni les sauvegardes effectivement activées.

## Disponibilité, SLA, RTO et RPO

### État vérifiable

Le code consulté ne définit pas de SLA formel et ne prouve pas l’existence de sauvegardes restaurables, d’un basculement automatique ou d’un site de reprise testé. Il serait donc inexact de présenter des niveaux de service comme déjà garantis.

### Objectifs proposés pour une première exploitation encadrée

| Indicateur | Objectif proposé | Signification |
|---|---:|---|
| Disponibilité mensuelle | **99,5 %** | Environ 3 h 39 min d’indisponibilité maximale sur un mois de 30 jours, hors exclusions contractuelles à définir. |
| RTO | **4 heures** | Délai cible pour rétablir les fonctions essentielles après un incident majeur. |
| RPO | **15 minutes** | Perte maximale de données visée en cas d’incident, sous réserve d’une sauvegarde et d’une réplication adaptées. |

Ces chiffres sont des **objectifs à valider**, et non un engagement opérationnel déjà établi.

### Pourquoi ces objectifs ?

Une disponibilité de 99,5 % constitue un objectif initial mesurable pour un service numérique, tout en reconnaissant les dépendances à des infrastructures et prestataires tiers. Un RTO de quatre heures vise un rétablissement dans la même demi-journée. Un RPO de quinze minutes limite la quantité de données susceptible d’être perdue, ce qui est important pour les historiques et le suivi des opérations.

Pour rendre ces objectifs crédibles, il faudra notamment mettre en place et tester : des sauvegardes régulières avec restauration vérifiée, une supervision et des alertes, une procédure de gestion d’incident, des contrôles d’accès, ainsi qu’un plan de continuité et de reprise après sinistre. Les SLA des prestataires partenaires devront également être examinés, car ils peuvent affecter la disponibilité de certaines fonctions.

## Engagement envers les clients et les autorités

DigitalFX entend offrir un service numérique traçable, avec des parcours de vérification, des limites opérationnelles et des outils d’administration. La demande d’agrément doit s’accompagner d’une documentation vérifiable sur le périmètre exact des services, les responsabilités de chaque partenaire, la protection des données, la gestion des incidents et la continuité d’activité.

Les objectifs de disponibilité et de reprise présentés ici devront être confirmés par des preuves techniques et opérationnelles avant d’être communiqués comme des engagements.

## Éléments techniques consultés

- [Présentation technique du backend](./digitalcapfx-backend/walkthrough.md)
- [Routes de l’API](./digitalcapfx-backend/internal/api/routes.go)
- [Configuration du serveur](./digitalcapfx-backend/internal/api/server.go)
- [Dépendances du backend](./digitalcapfx-backend/go.mod)
- [Dépendances du frontend](./digitalcapfx-frontend/package.json)
- [Construction de l’image serveur](./digitalcapfx-backend/Dockerfile)
- [Construction des images de déploiement](./digitalcapfx-backend/cloudbuild.deploy.yaml)