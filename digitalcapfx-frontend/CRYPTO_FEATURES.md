# Visibilité des fonctionnalités liées aux cryptomonnaies

Les fonctionnalités liées aux cryptomonnaies sont masquées dans le frontend à
l’aide du drapeau `ALLOW_CRYPTO`, défini dans
[`src/config/featureFlags.ts`](./src/config/featureFlags.ts). Sa valeur est
actuellement `false`.

Lorsque cette option est désactivée, l’application masque les soldes des
portefeuilles crypto et stablecoins (USDC, USDT et iUSD inclus), les
portefeuilles blockchain, les actions d’envoi ou de réception de cryptomonnaies
ainsi que Phone Send, les sources crypto utilisées pour alimenter une carte,
les écrans d’échange/swap et les opérations liées aux cryptomonnaies dans
l’historique. Le compte en dollars fiduciaires USD n’est pas une
cryptomonnaie : il reste disponible, tout comme les autres comptes fiduciaires
et les flux de paiement fiduciaires.

Ce drapeau concerne uniquement le frontend. Les routes backend, les
intégrations, les identifiants ainsi que les données enregistrées des
portefeuilles et des transactions restent inchangés. La réactivation ne
nécessite donc ni restauration ni migration des API.

## Réactiver les fonctionnalités crypto

1. Dans `src/config/featureFlags.ts`, remplacer la valeur de `ALLOW_CRYPTO`
   par `true` à la place de `false`.
2. Compiler puis déployer le frontend.
3. Avant d’activer les fonctionnalités pour les clients, vérifier les
   prestataires externes, les identifiants, les webhooks ainsi que les éventuels
   traitements KYC ou validations opérationnelles requis.

Aucune modification du backend n’est nécessaire pour ce drapeau de visibilité.
Lorsque `ALLOW_CRYPTO` passe à `true`, les pages et les commandes crypto déjà
présentes dans le frontend redeviennent disponibles.
