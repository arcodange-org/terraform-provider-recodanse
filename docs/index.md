---
page_title: "Fournisseur recodanse"
subcategory: ""
description: |-
  Les applications tierces et les webhooks d'un compte développeur ReCoDanse, pilotés par un jeton d'accès OAuth (recodanse-connexion) ou un jeton de développeur rdk_.
---

# Fournisseur `recodanse`

Le fournisseur décrit en code ce que l'écran « Mes applications » de ReCoDanse
fait à la main : déclarer une application tierce (OpenID Connect ou compte de
service), faire tourner son secret, la soumettre à la revue, poser ses webhooks
— et, au bac à sable seulement, déclencher les événements d'un compte fictif.

Il agit au nom d'**un compte développeur** et ne gère que **ses** applications :
aucun jeton qu'il présente ne lit une vidéo, un compte ou une autorisation
donnée par une personne.

## Exemple

```terraform
terraform {
  required_providers {
    recodanse = { source = "arcodange-org/recodanse" }
  }
}

# Après `recodanse-connexion connexion --environnement qa` :
provider "recodanse" {
  environnement = "qa"
}
```

## S'authentifier

- **OAuth device code** : `recodanse-connexion connexion` (CLI livrée avec le
  fournisseur) affiche une adresse et un code ; on approuve avec son compte
  ReCoDanse ; le jeton d'accès est rangé dans `~/.config/recodanse/jeton` (0600),
  que le fournisseur lit sans configuration. En CI, `--ci` le pose, masqué, dans
  `$GITHUB_ENV` (`RECODANSE_JETON`).
- **Jeton de développeur** `rdk_…`, créé dans l'app (« Mes applications » →
  « Jetons de développeur »), passé par `RECODANSE_JETON`.

Ordre de lecture : `jeton` ou `jeton_fichier` ; puis `RECODANSE_JETON` ou
`RECODANSE_JETON_FICHIER` ; puis `~/.config/recodanse/jeton`. Deux sources au
même rang sont une erreur. Un fichier lisible par d'autres, ou obtenu sur un
autre émetteur que celui de l'environnement, est refusé. Le jeton n'apparaît
dans aucun message.

## Les environnements

| `environnement` | API | Émetteur |
|---|---|---|
| `production` (défaut) | `https://api.recodanse.fr` | `https://connexion.recodanse.fr` |
| `qa` | `https://api-qa.recodanse.fr` | `https://connexion-qa.recodanse.fr` |
| `bac-a-sable` | à confirmer — donner `api` et `emetteur` | à confirmer |

## Schéma

### Optionnel

- `environnement` (String) — `production` (défaut), `qa` ou `bac-a-sable`. Variable : `RECODANSE_ENVIRONNEMENT`.
- `api` (String) — l'adresse de l'API, si elle n'est pas celle de l'environnement. Variable : `RECODANSE_API`.
- `emetteur` (String) — l'adresse de la porte d'autorisation (Hydra), qui sert à renouveler un jeton rangé par `recodanse-connexion`. Variable : `RECODANSE_EMETTEUR`.
- `jeton` (String, sensible) — un jeton `rdk_…` ou un jeton d'accès OAuth du client `recodanse-outils`. Variable : `RECODANSE_JETON`. Jamais en clair dans un fichier versionné.
- `jeton_fichier` (String) — un fichier 0600 : celui de `recodanse-connexion`, ou un jeton nu. Variable : `RECODANSE_JETON_FICHIER`.
- `adresse` (String, **déprécié**) — ancien nom de `api`.

## Révocation et renouvellement

- Faire tourner un secret d'application : changer `rotation` (voir `recodanse_application`).
- Révoquer un jeton `rdk_…` : dans l'app. Se déconnecter : `recodanse-connexion deconnexion`
  (RFC 7009), ou retirer « Les outils du développeur » dans « Applications autorisées ».
- En CI, `recodanse-connexion deconnexion --ci` (dernier pas, `if: always()`) révoque le
  `RECODANSE_JETON` posé par `connexion --ci` : code 0 si révoqué ou déjà inactif, 1 sinon.
- Avec `recodanse-connexion connexion --hors-ligne`, le fournisseur renouvelle seul un
  jeton qui expire (30 jours) et réécrit le fichier.
- L'état porte des secrets marqués sensibles : il se range chiffré.
