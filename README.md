# Le fournisseur Terraform / OpenTofu de ReCoDanse

Les **applications tierces** d'un compte développeur ReCoDanse — OpenID Connect
ou compte de service — et leurs **webhooks**, décrits dans un fichier qu'on relit
et qu'on versionne : rotation des secrets, portées demandées, webhooks, revue.

Le fournisseur ne fait rien que l'écran « Mes applications » ne fasse : il parle
à `/v1/applications` (contrat publié : `openapi-tiers.yaml`, sur dev.recodanse.fr).
Le dépôt porte aussi **`recodanse-connexion`**, la CLI qui connecte ces outils à
ton compte par l'**OAuth device code** — sans copier un jeton à la main.

> **Source** : la forge d'Arcodange, avec un miroir public sur GitHub,
> `github.com/arcodange-org/terraform-provider-recodanse`, qui est aussi le chemin du module Go.
> Adresse aux registres : **`arcodange-org/recodanse`** (registry.terraform.io et registry.opentofu.org).
> Licence : MPL-2.0.

## Sommaire

- [S'authentifier : deux espèces de jetons](#sauthentifier--deux-espèces-de-jetons)
- [Les environnements](#les-environnements)
- [Installer](#installer)
- [La CLI `recodanse-connexion`](#la-cli-recodanse-connexion)
- [Exemples, un par environnement](#exemples-un-par-environnement)
- [Ressources et sources de données](#ressources-et-sources-de-données)
- [Le bac à sable — seulement là](#le-bac-à-sable--seulement-là)
- [Faire tourner, révoquer, se déconnecter](#faire-tourner-révoquer-se-déconnecter)
- [Ce qu'il faut savoir](#ce-quil-faut-savoir)
- [Ce que les essais prouvent](#ce-que-les-essais-prouvent)

## S'authentifier : deux espèces de jetons

| | Comment l'obtenir | Durée | Le révoquer |
|---|---|---|---|
| **Jeton d'accès OAuth** du client `recodanse-outils`, portée `openid developpeur` | `recodanse-connexion connexion` : ouvrir une adresse, approuver avec SON compte ReCoDanse | 1 h ; avec `--hors-ligne`, renouvelé seul pendant 30 j | `recodanse-connexion deconnexion`, ou retirer « Les outils du développeur » dans « Applications autorisées » |
| **Jeton de développeur** `rdk_…` | l'app : « Mes applications » → « Jetons de développeur » (montré une fois) | 30, 90 ou 365 j | dans l'app, d'un geste |

L'un comme l'autre gère **tes applications de développeur, rien d'autre** : ni
vidéo, ni compte, ni autorisation donnée par une personne. Le compte doit avoir
son adresse confirmée et les conditions de service acceptées.

⚠ La connexion par OAuth device code demande, côté ReCoDanse, le client
`recodanse-outils`, la page `/appareil` et une API qui accepte le jeton d'accès
OAuth : tant que ce n'est pas déployé, l'API ne reconnaît que les jetons `rdk_…`.

**D'où le fournisseur lit le jeton** — du plus explicite au plus implicite ; deux
sources au même rang sont une ERREUR, jamais un choix silencieux :

1. l'attribut `jeton`, ou l'attribut `jeton_fichier` ;
2. la variable `RECODANSE_JETON`, ou la variable `RECODANSE_JETON_FICHIER` ;
3. le fichier `~/.config/recodanse/jeton`, s'il existe (celui qu'écrit `recodanse-connexion connexion`).

Un fichier lisible par d'autres que soi est refusé (`chmod 600`). Un fichier
obtenu sur un AUTRE émetteur que celui de l'environnement est refusé : un jeton
de la QA ne part pas vers la production. Le jeton n'apparaît dans aucun message.

## Les environnements

| `environnement` | API | Émetteur (Hydra) |
|---|---|---|
| `production` (défaut) | `https://api.recodanse.fr` | `https://connexion.recodanse.fr` |
| `qa` | `https://api-qa.recodanse.fr` | `https://connexion-qa.recodanse.fr` |
| `bac-a-sable` | **déclaré, adresses à confirmer** | (adresses prévues : `bac-a-sable.recodanse.fr` et `connexion.bac-a-sable.recodanse.fr`) |

`api` et `emetteur` (variables `RECODANSE_API`, `RECODANSE_EMETTEUR`) surchargent
la table. Le bac à sable **refuse, en le disant**, tant qu'on ne donne pas les
deux : deviner une adresse enverrait un jeton vers un hôte inconnu. `http://`
n'est admis que vers `127.0.0.1` / `localhost` (essais locaux). L'ancien attribut
`adresse` reste lu (déprécié : c'est `api`).

## Installer

Une fois publié : `source = "arcodange-org/recodanse"`, et `terraform init` / `tofu init` le
téléchargent du registre. D'ici là (et pour essayer une branche) : on construit, puis on installe
par un **miroir local** (lu sans réseau par Terraform ET OpenTofu) ou par **`dev_overrides`**.

```bash
git clone https://github.com/arcodange-org/terraform-provider-recodanse.git
cd terraform-provider-recodanse
V=0.2.0
go build -ldflags "-X main.version=$V" -o terraform-provider-recodanse .
go build -ldflags "-X main.version=$V" -o recodanse-connexion ./cmd/recodanse-connexion

# Le miroir local (source « arcodange-org/recodanse ») :
D=~/.terraform.d/plugins/registry.terraform.io/arcodange-org/recodanse/$V/$(go env GOOS)_$(go env GOARCH)
mkdir -p "$D" && cp terraform-provider-recodanse "$D/terraform-provider-recodanse_v$V"
```

Pour développer le fournisseur lui-même, `dev_overrides` dans le fichier de
configuration du CLI (`~/.terraformrc`, ou `TF_CLI_CONFIG_FILE`) — c'est ce que
font les essais :

```hcl
provider_installation {
  dev_overrides {
    "arcodange-org/recodanse" = "/chemin/vers/le/dossier/du/binaire"
  }
  direct {}
}
```

Terraform ou OpenTofu : les deux sont joués par l'essai ; un seul des deux suffit.

## La CLI `recodanse-connexion`

```bash
recodanse-connexion connexion --environnement qa            # 1 h, fichier ~/.config/recodanse/jeton (0600)
recodanse-connexion connexion --environnement qa --hors-ligne   # + offline_access : renouvelé seul 30 j
recodanse-connexion qui                                     # qui est connecté, prouvé par GET /v1/applications
recodanse-connexion deconnexion                             # révocation (RFC 7009) puis fichier effacé
recodanse-connexion deconnexion --ci                        # en CI : révoque RECODANSE_JETON, aucun fichier
```

`connexion` découvre l'émetteur (`/.well-known/openid-configuration`), demande un
code pour le client `recodanse-outils` (portée `openid developpeur`), puis écrit
**sur la sortie d'erreur** l'adresse à ouvrir (`verification_uri_complete`), le
code à vérifier (`user_code`) et sa durée. Elle attend au rythme que l'émetteur
impose (`interval`, allongé de 5 s à chaque `slow_down`), s'arrête sur
`expired_token` et `access_denied` en le disant, et ne range rien dans ces cas.
Le seul geste humain : ouvrir l'adresse, se connecter avec son compte ReCoDanse,
approuver « Les outils du développeur ».

Drapeaux : `--environnement`, `--api`, `--emetteur` (ou `RECODANSE_ENVIRONNEMENT`,
`RECODANSE_API`, `RECODANSE_EMETTEUR`) ; `--sortie FICHIER` (ou
`RECODANSE_JETON_FICHIER`) ; `--hors-ligne` ; `--ci`. `qui` et `deconnexion`
prennent `--fichier` ; `deconnexion` prend aussi `--ci` (les deux ensemble : faute d'usage).

**En CI** (`--ci`, GitHub Actions comme Gitea Actions) : AUCUN fichier. La CLI
écrit d'abord `::add-mask::<jeton>` sur la sortie standard, puis
`RECODANSE_JETON`, `RECODANSE_ENVIRONNEMENT`, `RECODANSE_API` et
`RECODANSE_EMETTEUR` dans `$GITHUB_ENV` — le jeton n'est écrit nulle part
ailleurs. `--ci` refuse `--hors-ligne` (un jeton de CI vit une heure).

**À la fin du job**, `deconnexion --ci` révoque ce jeton, sinon il vit son heure :
elle lit `RECODANSE_JETON` (et `RECODANSE_JETON_DE_RENOUVELLEMENT` s'il existe —
`connexion --ci` n'en écrit jamais) et le révoque (RFC 7009) à la révocation que
découvre `RECODANSE_EMETTEUR`. Elle n'écrit aucun fichier et n'affiche jamais le
jeton. Code **0** si l'émetteur a répondu 200 (révoqué, ou déjà inactif : RFC 7009
§ 2.2) ; **1** sinon — aucun jeton dans l'environnement, un jeton `rdk_…` (il se
révoque dans l'app, pas chez l'émetteur), émetteur injoignable ou qui refuse.

```yaml
# .gitea/workflows/plan.yml (syntaxe GitHub Actions)
- run: recodanse-connexion connexion --ci --environnement qa   # l'adresse et le code s'affichent au journal
- run: tofu plan -out plan.tfplan                              # le fournisseur lit RECODANSE_JETON
- if: always()
  run: recodanse-connexion deconnexion --ci                    # le jeton ne survit pas au job
```

Codes de sortie : 0 réussi, 1 échec (dit pourquoi), 2 faute d'usage.

## Exemples, un par environnement

```hcl
terraform {
  required_providers {
    recodanse = { source = "arcodange-org/recodanse" }
  }
}

# Production : rien à écrire (jeton : ~/.config/recodanse/jeton ou RECODANSE_JETON).
provider "recodanse" {}

# QA :
provider "recodanse" {
  alias         = "qa"
  environnement = "qa"
}

# Bac à sable — tant que ses adresses ne sont pas confirmées, on les donne :
provider "recodanse" {
  alias         = "bac"
  environnement = "bac-a-sable"
  api           = "https://bac-a-sable.recodanse.fr"            # à confirmer
  emetteur      = "https://connexion.bac-a-sable.recodanse.fr"  # à confirmer
  jeton_fichier = pathexpand("~/.config/recodanse/jeton-bac")
}
```

Un exemple complet (compte de service à secret tournant, connexion publique,
webhook, secret vers un coffre) :

```hcl
terraform {
  required_providers {
    recodanse = { source = "arcodange-org/recodanse" }
    time      = { source = "hashicorp/time" }
  }
}

provider "recodanse" {
  environnement = "qa"
}

# Le secret tourne tous les 90 jours : quand `rotation` change, l'ancien
# secret cesse de valoir À L'INSTANT et le nouveau entre dans l'état.
resource "time_rotating" "secret_du_studio" {
  rotation_days = 90
}

resource "recodanse_application" "studio" {
  nom       = "Planning du studio"
  categorie = "service"
  portees   = ["evenements.publier", "creneaux.publier"]
  rotation  = { quand = time_rotating.secret_du_studio.id }

  # Demander la revue. La version se lit sur dev.recodanse.fr ; le
  # fournisseur ne l'accepte JAMAIS à ta place, et refuse au plan une
  # version qui n'est plus en vigueur (en nommant la bonne).
  soumettre              = true
  accepte_les_conditions = "2026-10-20"
}

resource "recodanse_application" "site" {
  nom          = "Site du festival"
  categorie    = "connexion"
  portees      = ["openid", "profile", "email"]
  redirections = ["https://festival.example/retour"]
  publique     = true # PKCE, pas de secret
}

resource "recodanse_webhook" "site" {
  application = recodanse_application.site.id
  url         = "https://festival.example/webhooks/recodanse"
  evenements  = ["autorisation.retiree", "compte.efface"]
}

# Le secret part vers ton coffre — jamais vers une sortie en clair.
resource "vault_kv_secret_v2" "studio" {
  mount = "kv"
  name  = "recodanse/studio"
  data_json = jsonencode({
    client_id     = recodanse_application.studio.client_id
    client_secret = recodanse_application.studio.client_secret
  })
}
```

## Ressources et sources de données

| Nom | Espèce | Ce qu'il fait |
|---|---|---|
| `recodanse_application` | ressource | déclarer, corriger, faire tourner le secret (`rotation`), soumettre (`soumettre`), retirer ; import `appl_…` |
| `recodanse_webhook` | ressource | poser, retirer (changer = recréer, nouveau `whsec_…`) ; import `appl_…/webh_…` |
| `recodanse_evenement_du_bac_a_sable` | ressource | **bac à sable seulement** — déclencher un événement pour un compte fictif |
| `recodanse_catalogue` | source | espèces, portées par espèce, événements des webhooks — et les portées que l'API **annonce** |
| `recodanse_application` | source | une de tes applications, par `id` (sans secret) |
| `recodanse_applications` | source | toutes tes applications (sans secret), et le plafond `max` |
| `recodanse_comptes_fictifs` | source | **bac à sable seulement** — les comptes fictifs |

La documentation au format du registre vit dans [`docs/`](docs/index.md).

**Le catalogue** est une copie (`internal/fournisseur/portees.go`) : l'API n'a pas
de route qui donne les portées PAR ESPÈCE. À chaque lecture, la source la
confronte à ce que l'API annonce (`/.well-known/oauth-protected-resource`,
`scopes_supported` — route publique, aucun jeton n'y part) et AVERTIT d'un écart
dans un sens ou dans l'autre.

⚠ Aucune source de données ne lit la version des conditions EN VIGUEUR : la lire
permettrait d'écrire `accepte_les_conditions = data.….conditions_en_vigueur`,
c'est-à-dire d'accepter un texte sans l'avoir lu.

## Le bac à sable — seulement là

`recodanse_comptes_fictifs` et `recodanse_evenement_du_bac_a_sable` se **refusent
dès le plan** si l'environnement du fournisseur n'est pas `bac-a-sable`, sans un appel. L'API, elle, ne monte leurs routes qu'au
bac à sable (ailleurs : 404).

```hcl
data "recodanse_comptes_fictifs" "tous" { provider = recodanse.bac }

resource "recodanse_evenement_du_bac_a_sable" "effacement" {
  provider    = recodanse.bac
  application = recodanse_application.site.id
  compte      = data.recodanse_comptes_fictifs.tous.comptes[0].cle
  evenement   = "compte.efface"
  triggers    = { essai = "1" } # changer une valeur = redéclencher
}
```

Créer la ressource **déclenche** l'événement une fois ; changer `triggers` le
redéclenche ; la **détruire ne fait rien** (un envoi parti ne se reprend pas).
Les deux événements retirent VRAIMENT l'accord : pour redéclencher, réponds
d'abord « Autoriser » au nom du compte fictif (« Continuer en tant que »), sinon
l'API répond 409 `pas_autorisee` — le message le dit.

⚠ **Contrat supposé** : `GET /v1/bac-a-sable/comptes` et
`POST /v1/bac-a-sable/evenements` n'existent pas encore côté API (aujourd'hui :
`/v0/tiers/bac-a-sable/*`, routes de l'app, par cookie). Le fournisseur est écrit et éprouvé
contre une doublure de ce contrat :

| Route | Corps | Réponse |
|---|---|---|
| `GET /v1/bac-a-sable/comptes` | — | 200 `{"comptes":[{"cle","prenom","titre"}]}` |
| `POST /v1/bac-a-sable/evenements` | `{"application":"appl_…","compte":"<cle>","evenement":"autorisation.retiree"\|"compte.efface"}` | 202 `{"evenement","sujet"}` ; 400 `evenement_non_declenchable`, `compte_fictif_inconnu` ; 404 application d'un autre compte ou bac à sable inactif ; 409 `pas_autorisee` ; 401 `jeton_invalide` ; 429 `quota_depasse` |

## Faire tourner, révoquer, se déconnecter

- **Faire tourner un secret d'application** : changer une valeur de `rotation`
  (ou laisser `time_rotating` le faire), puis `apply`. L'ancien secret cesse de
  valoir **à l'instant** (Hydra n'en garde qu'un) : le nouveau doit atteindre ton
  coffre dans le même `apply`.
- **Faire tourner la clé d'un webhook** : recréer le webhook
  (`tofu apply -replace=recodanse_webhook.x`) ; la nouvelle clé `whsec_…` entre
  dans l'état.
- **Révoquer un jeton `rdk_…`** : dans l'app, « Mes applications » → « Jetons de
  développeur ». Le plan suivant est refusé (`jeton_invalide`).
- **Se déconnecter (jeton OAuth)** : `recodanse-connexion deconnexion` révoque le
  jeton de renouvellement (Hydra emporte toute la chaîne) puis le jeton d'accès,
  et efface le fichier. Si l'émetteur est injoignable, le fichier est GARDÉ (rien
  n'est révoqué) et la commande le dit. Autre voie : retirer « Les outils du
  développeur » dans « Applications autorisées ».
- **Se déconnecter en CI** : `recodanse-connexion deconnexion --ci` en dernier pas
  du job (`if: always()`) — 0 révoqué ou déjà inactif, 1 sinon.
- **Le renouvellement** : avec `--hors-ligne`, un jeton à moins de deux minutes
  de son expiration est renouvelé par le fournisseur ou par `qui`, et le fichier
  est réécrit (Hydra fait tourner le jeton de renouvellement). Sans
  `--hors-ligne`, le message dit de relancer `connexion`.
- **L'état** porte les secrets (marqués sensibles) : il se range **chiffré**
  (chiffrement d'état d'OpenTofu, ou un état distant chiffré).

## Ce qu'il faut savoir

| | |
|---|---|
| **Le secret** | connu à la création et après chaque rotation, **jamais relu** (l'API ne le montre qu'une fois). Il vit dans l'état, marqué sensible : l'état se garde comme un secret. Un client public n'en a pas. |
| **`rotation`** | une carte libre, comme les `keepers` du fournisseur `random`. Changer une valeur fait tourner le secret. Refusée sur un client public. |
| **Les portées** | celles que l'application DEMANDE. Une portée hors de sa catégorie est refusée **au plan**. Une portée ajoutée à une application **publiée** est refusée par l'API (`portees_figees`) : elle repasse par la revue. |
| **`soumettre`** | developpement → en_revue. ReCoDanse relit chaque application ; rien ne se publie par un `apply`. Une soumission ne se retire pas. Si elle échoue juste après la déclaration, l'application est gardée (pas recréée) et le prochain `apply` retente. ⚠ Une application `connexion` ne se soumet que si un `recodanse_webhook` abonné à `autorisation.retiree` et `compte.efface` répond 2xx à un essai signé et 4xx au même essai mal signé : le webhook dépend de l'identifiant de l'application, donc il naît APRÈS la première soumission — le premier `apply` avertit (`portee_sans_webhook_d_effacement`), le second soumet. ⚡ Sans portée gardée par l'effacement (`profile`, `email`, `offline_access`, `videos.lire`) — `openid` et des écritures seulement —, l'application se soumet sans webhook, dès le premier `apply`. |
| **`categorie`, `publique`** | les changer **recrée** l'application : nouveau `client_id`. |
| **Un webhook** | ne se corrige pas (aucune route) : changer l'adresse ou les événements le recrée, avec un nouveau secret `whsec_…`. |
| **Le quota** | un seau par jeton (lectures, écritures). Sur un 429, le fournisseur attend `Retry-After` et rejoue, trois fois au plus. Il ne rejoue **jamais** une écriture sur une autre erreur. |
| **Importer** | `terraform import recodanse_application.x appl_…` ; `terraform import recodanse_webhook.x appl_…/webh_…`. Une application importée n'a pas de secret dans l'état tant qu'on ne la fait pas tourner. |

## Ce que les essais prouvent

```bash
go vet ./... && go test ./...   # ce que joue la CI (.gitea/workflows/ci.yml)
```

**Unitaires** (en CI) : la configuration du fournisseur et ses sources de
données jouées par le protocole (tfprotov6) contre une doublure d'API ; la CLI
jouée en entier contre une doublure d'émetteur qui rejoue
`authorization_pending` → `slow_down` → succès, `expired_token`,
`access_denied` ; le renouvellement et la révocation ; le bac à sable contre
la doublure du contrat supposé. Seules les attentes sont remplacées.

**Deux SAUTS DÉCLARÉS** — un saut silencieux est la forme locale du défaut que
tout ce projet traque ; ceux-ci se nomment ici, et chacun dit sa raison dans la
sortie de `go test -v` :

| Essai | Se saute sans | Échoue au lieu de sauter avec |
|---|---|---|
| `TestLeFournisseurJoueParTerraformEtOpenTofu` (`acceptation_test.go`) | `RECODANSE_ESSAI_API`, `RECODANSE_ESSAI_HYDRA`, `RECODANSE_ESSAI_BINAIRES` — la CI n'a ni Terraform, ni OpenTofu, ni Hydra | `RECODANSE_ESSAI_OBLIGATOIRE=1` |
| `TestLeFournisseurContreLaQA` (`acceptation_qa_test.go`) | `RECODANSE_ACCEPTATION_QA=1` — il parle à la QA DÉPLOYÉE | `RECODANSE_ACCEPTATION_QA=1` sans `RECODANSE_JETON` |

**L'essai local à vrais binaires**, contre l'API ReCoDanse lancée en local (en
mémoire, comptes de démonstration ouverts ; son dépôt n'est pas public) et un
**Hydra réel** (image `oryd/hydra:v26.2.0`, aucun binaire à installer) —
déclarer, relire sans écart, lire les trois sources de données, faire tourner le
secret (Hydra refuse l'ancien, accepte le nouveau), ajouter une portée, refuser
au plan une portée hors catégorie et une version périmée des conditions,
soumettre, importer, révoquer le jeton (le plan suivant est refusé), détruire :

```bash
docker network create essai
docker run -d --name essai-pg --network essai -e POSTGRES_PASSWORD=essai -e POSTGRES_DB=hydra postgres:16-alpine
H=~/.cache/hydra-essai; mkdir -p $H   # un dossier PARTAGÉ avec Docker, monté SANS :ro (Hydra refuse sinon)
# $H/hydra.yaml : la configuration Hydra de référence de ReCoDanse (PKCE exigé des
# clients publics, jetons d'accès opaques, enregistrement dynamique éteint).
HY="-e DSN=postgres://postgres:essai@essai-pg:5432/hydra?sslmode=disable -e SECRETS_SYSTEM=une-cle-locale-de-trente-deux-octets \
    -e URLS_SELF_ISSUER=http://127.0.0.1:4744 -e URLS_LOGIN=http://127.0.0.1:3999/autoriser -e URLS_CONSENT=http://127.0.0.1:3999/autoriser \
    -e URLS_DEVICE_VERIFICATION=http://127.0.0.1:3999/appareil -e URLS_DEVICE_SUCCESS=http://127.0.0.1:3999/appareil/fini \
    -e SERVE_PUBLIC_HOST=0.0.0.0 -e SERVE_ADMIN_HOST=0.0.0.0 -v $H:/conf"
docker run --rm --network essai $HY oryd/hydra:v26.2.0 migrate sql up -e --yes --config /conf/hydra.yaml
docker run -d --name essai-hydra --network essai $HY -p 127.0.0.1:4744:4444 -p 127.0.0.1:4745:4445 \
  oryd/hydra:v26.2.0 serve all --dev --sqa-opt-out --config /conf/hydra.yaml
# L'API ReCoDanse, en mémoire, comptes de démonstration ouverts, reliée à cet Hydra
# (admin http://127.0.0.1:4745, émetteur http://127.0.0.1:4744), servie sur 127.0.0.1:8781.
# Ici :
RECODANSE_ESSAI_API=http://127.0.0.1:8781 RECODANSE_ESSAI_HYDRA=http://127.0.0.1:4744 \
  RECODANSE_ESSAI_BINAIRES=/opt/homebrew/bin/tofu RECODANSE_ESSAI_OBLIGATOIRE=1 \
  RECODANSE_ACCEPTATION_QA=1 RECODANSE_ACCEPTATION_QA_REPETITION=http://127.0.0.1:8781 \
  go test -count=1 -run TestLeFournisseur -v .
```

`RECODANSE_ACCEPTATION_QA_REPETITION` rejoue le scénario de la QA (« Fixture
fournisseur <horodatage> » : déclarer, relire, faire tourner, webhook, catalogue,
détruire) contre l'API locale, avec le jeton d'un compte de démonstration.

**Contre la QA** (pas encore joué : la QA ne sert pas encore la connexion
OAuth device code) — avec le jeton d'un COMPTE DE TEST, jamais celui d'une
vraie personne :

```bash
RECODANSE_ACCEPTATION_QA=1 RECODANSE_JETON=rdk_… go test -count=1 -run TestLeFournisseurContreLaQA -v .
```

### Relevés

Joué avec OpenTofu 1.10.5 contre Hydra v26.2.0 (conteneur) et l'API locale :
l'essai à vrais binaires et la répétition de la QA, verts.
La CLI a joué l'OAuth device code contre ce MÊME Hydra (client
`recodanse-outils` créé à la main par l'admin, approbation par une doublure
d'humain qui appelle l'admin comme le fera `/appareil`) : connexion, `qui`
(sujet lu par userinfo), renouvellement (jeton d'accès ET renouvellement
changés), déconnexion — l'introspection de Hydra rend ensuite `active: false`
pour les deux. `qui` a reçu 401 `jeton_invalide` de l'API : la garde
`Developpeur` n'acceptait alors que `rdk_`.

Sabotages, relevés dans l'ordre intact → sabotages → rétabli, chacun **vérifié
appliqué** avant lecture, paquet compilable (un sabotage qui ne compilait pas a
été refait) ; intact et rétabli : `go test ./...` code 0.

| Sabotage | Code | Ce qui rougit |
|---|---|---|
| `slow_down` n'allonge plus l'attente | 1 | `TestLAttenteRespecteIntervalPuisRalentitSurSlowDown`, `TestConnexionRangeLeJetonEn0600SansLAfficher` |
| `--ci` écrit le jeton sur la sortie d'erreur | 1 | `TestEnCILeJetonEstMasquePuisEcritDansGithubEnvEtNullePartAilleurs` |
| la permission du fichier n'est plus vérifiée | 1 | `TestUnFichierLisibleParDAutresEstRefuse` |
| l'émetteur du fichier n'est plus confronté | 1 | `TestUnJetonDUnAutreEmetteurNePartPas`, `TestUnFichierDUnAutreEnvironnementNePartPasEtNeSAffichePas` |
| le bac à sable sans adresses n'est plus refusé | 1 | `TestLeBacASableRefuseTantQuIlNAPasDAdresses`, `TestLeBacASableSansAdressesRefuseClairement` |
| la route publique du catalogue reçoit le jeton | 1 | `TestLeCatalogueSeLitEtSeConfronteALAnnonceDeLAPI` |
| les objets du bac à sable ne se refusent plus ailleurs | 1 | `TestHorsDuBacASableLesComptesFictifsSeRefusentSansAppel`, `TestHorsDuBacASableLEvenementSeRefuseAuPlan` |
| détruire l'événement le redéclenche | 1 | `TestCreerLEvenementLeDeclencheUneFoisEtDetruireNAppelleRien` |
| le renouvellement suivant n'est pas rangé | 1 | `TestUnJetonExpireSeRenouvelleEtLeFichierEstReecrit` |
| `deconnexion` ne révoque plus le renouvellement | 1 | `TestDeconnexionRevoqueLeRenouvellementPuisLAccesEtEfface` |
| `deconnexion --ci` ne révoque plus le jeton d'accès | 1 | `TestDeconnexionCIRevoqueLeJetonDeLEnvironnement`, `…AussiUnRenouvellementPresent`, `…EchoueQuandRienNEstRevoque` |
| `deconnexion --ci` tait un refus de l'émetteur | 1 | `TestDeconnexionCIEchoueQuandRienNEstRevoque/révocation_refusée`, `/sans_révocation` |
| `deconnexion --ci` envoie un `rdk_` à l'émetteur | 1 | `TestDeconnexionCIEchoueQuandRienNEstRevoque/jeton_rdk_` |
| `deconnexion --ci` cite le jeton dans son message | 1 | `TestDeconnexionCIRevoqueLeJetonDeLEnvironnement`, `…AussiUnRenouvellementPresent` |
| la source relit le `client_id` comme nom (vrais binaires) | 1 | les deux essais à vrais binaires : « nom lu … » |
| `Delete` d'une application n'appelle plus l'API (vrais binaires) | 1 | les deux essais : « appl_… survit à la destruction : 200 » |

Sabotages relevés avant que le fournisseur ait son propre dépôt, avec
Terraform 1.16.6 et OpenTofu 1.10.6 :

| Sabotage | Ce qui rougit |
|---|---|
| la rotation n'est plus annoncée au plan (`client_secret` gardé) | les deux binaires : « Provider produced inconsistent result after apply » |
| la rotation ne change pas le secret | « la rotation n'a pas changé le secret » |
| la portée hors catégorie n'est plus refusée | « une portée interdite devait être refusée au plan (code 0) » |
| la version des conditions n'est plus lue au plan | « une version périmée devait être refusée au plan en nommant "2026-10-20" » |
| l'import ne déduit plus `soumettre` | « après l'import : le plan d'après devait être vide (code 2) » |
| un service garde `redirections = []` | « Provider produced inconsistent result after apply » |
| `Delete` n'appelle pas l'API | « appl_… survit à la destruction : 200 » |
| côté API, `Actif` ignore la révocation (binaire saboté, relancé) | « après la révocation, le plan devait être refusé (jeton_invalide), code 0 » |

### La copie du catalogue

`internal/fournisseur/portees.go` (`PorteesParCategorie`, `PorteesDesAgents`) et
`EvenementsDesWebhooks` sont des COPIES du catalogue de l'API. Une garde côté
API, qui confrontait ces copies au catalogue, doit désormais lire ce dépôt-ci (voir
le CHANGELOG, « Ce qui reste »). En attendant, la source `recodanse_catalogue`
avertit de tout écart à chaque lecture.

## Publier

Un tag `vX.Y.Z` poussé sur GitHub fait jouer `.github/workflows/release.yml` : goreleaser
(`.goreleaser.yml`) construit les archives par plateforme, `SHA256SUMS`, sa signature GPG et
le manifeste (`terraform-registry-manifest.json`, protocole 6.0), puis publie la release.
Les registres lisent la release : celui de Terraform par son webhook, celui d'OpenTofu de
lui-même. La clé publique de signature est celle déclarée aux deux registres.
