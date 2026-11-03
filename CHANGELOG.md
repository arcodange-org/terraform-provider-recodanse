# Journal des versions

Le format suit [Keep a Changelog](https://keepachangelog.com/fr/1.1.0/) ; les
numéros suivent SemVer. Une version est publiée par son tag `vX.Y.Z` (release
signée sur GitHub, registres Terraform et OpenTofu : `arcodange-org/recodanse`).

## 0.2.0 — 2026-11-03 — première version publique

### Publication

- Adresse aux registres : `arcodange-org/recodanse` ; module Go
  `github.com/arcodange-org/terraform-provider-recodanse` ; licence MPL-2.0.
- Release signée par goreleaser (`.github/workflows/release.yml`) : archives par
  plateforme, `SHA256SUMS` signé par GPG, manifeste (protocole 6.0), et
  `recodanse-connexion` pour Linux, macOS et Windows.
- La CI de la forge ne joue `-race` que si le noyau du runner le permet, et le dit.

### Ajouté

- **Son propre dépôt** : sur la forge d'Arcodange, avec un miroir public sur GitHub ;
  l'historique est repris du dépôt de l'API, où le fournisseur est né.
- **Environnements** : `environnement` = `production` (défaut) | `qa` |
  `bac-a-sable` (déclaré, adresses à confirmer : refus clair sans `api` et
  `emetteur`) ; `api`, `emetteur` surchargeables (`RECODANSE_API`,
  `RECODANSE_EMETTEUR`, `RECODANSE_ENVIRONNEMENT`).
- **Jeton** : `jeton` accepte un `rdk_…` OU un jeton d'accès OAuth ;
  `jeton_fichier` (`RECODANSE_JETON_FICHIER`) ; fichier par défaut
  `~/.config/recodanse/jeton`. Refus : deux sources au même rang, un fichier
  lisible par d'autres, un fichier obtenu sur un autre émetteur. Renouvellement
  automatique d'un jeton rangé avec `offline_access`.
- **CLI `recodanse-connexion`** : `connexion` (OAuth device code, RFC 8628 ;
  `--hors-ligne`, `--ci`, `--sortie`), `qui`, `deconnexion` (RFC 7009) et
  `deconnexion --ci` (révoque `RECODANSE_JETON`, et `RECODANSE_JETON_DE_RENOUVELLEMENT`
  s'il existe ; 0 révoqué ou déjà inactif, 1 sinon) — relevé par la démo CI :
  sans lui, le jeton d'un job vivait son heure.
- **Sources de données** : `recodanse_catalogue`, `recodanse_application`,
  `recodanse_applications`, `recodanse_comptes_fictifs` (bac à sable).
- **Ressource** `recodanse_evenement_du_bac_a_sable` (bac à sable).
- `docs/` au format du registre, `.goreleaser.yml` et
  `terraform-registry-manifest.json` (jamais exécutés), CI Gitea.

### Changé

- `adresse` est **déprécié** au profit de `api` (même sens) ; il reste lu.

### Ce qui reste

- Les routes `/v1/bac-a-sable/comptes` et `/v1/bac-a-sable/evenements` (contrat
  supposé, README) sont à ouvrir côté API.
- La connexion OAuth device code côté ReCoDanse (client `recodanse-outils`,
  `/appareil`, garde `Developpeur` qui accepte le jeton d'accès) : à déployer.
- La garde de la copie du catalogue (côté API) doit lire CE dépôt (ou une route
  de catalogue par espèce, à ouvrir côté API).

## 0.1.0 — 2026-10-20 (dans le dépôt de l'API)

- `recodanse_application` (rotation, soumettre, import), `recodanse_webhook`,
  essai d'acceptation Terraform + OpenTofu contre un Hydra réel.
