---
page_title: "recodanse_application (Resource) - recodanse"
subcategory: ""
description: |-
  Une application tierce de ReCoDanse (OpenID Connect ou compte de service).
---

# recodanse_application (ressource)

Une application tierce : son nom, son espèce, les portées qu'elle **demande**,
ses adresses de retour — et deux intentions que l'écran porte par des boutons,
`rotation` (faire tourner le secret) et `soumettre` (demander la revue).

## Exemple

```terraform
resource "time_rotating" "secret" {
  rotation_days = 90
}

resource "recodanse_application" "studio" {
  nom       = "Planning du studio"
  categorie = "service"
  portees   = ["evenements.publier", "creneaux.publier"]
  rotation  = { quand = time_rotating.secret.id }

  soumettre              = true
  accepte_les_conditions = "2026-10-20" # lue sur dev.recodanse.fr, écrite de ta main
}

resource "recodanse_application" "site" {
  nom          = "Site du festival"
  categorie    = "connexion"
  portees      = ["openid", "profile", "email"]
  redirections = ["https://festival.example/retour"]
  publique     = true # PKCE, pas de secret
}
```

## Schéma

### Requis

- `nom` (String) — le nom que la personne lit sur la page « Autoriser » (2 à 60 caractères, sans espace au bord).
- `categorie` (String) — `connexion` (OpenID Connect, une personne autorise) ou `service` (publie pour le compte qui l'a déclarée). **Changer recrée** l'application.
- `portees` (Set of String) — les portées demandées. `connexion` : openid, profile, email, offline_access, videos.lire, videos.ranger, videos.deposer, videos.annoter, programmes.poser, evenements.proposer · `service` : evenements.publier, creneaux.publier. Une portée hors catégorie est refusée **au plan**.

### Optionnel

- `description` (String) — 500 caractères au plus.
- `site_web` (String) — https:// seulement.
- `redirections` (Set of String) — `connexion` seulement (au moins une) : https://, http://127.0.0.1 ou un schéma d'app native ; dix au plus.
- `publique` (Boolean) — client sans secret (PKCE obligatoire). **Changer recrée** l'application.
- `rotation` (Map of String) — des valeurs libres ; quand l'une change, le secret **tourne** (l'ancien cesse de valoir à l'instant). Refusée sur un client public.
- `soumettre` (Boolean) — demander la revue (developpement → en_revue). Une soumission ne se retire pas.
- `accepte_les_conditions` (String) — la version des conditions des développeurs acceptée, exigée pour soumettre ; une version périmée est refusée au plan en nommant la bonne. Jamais remplie à ta place.

### Lecture seule

- `id` (String) — `appl_…`, distinct du `client_id`.
- `client_id` (String) — l'identifiant de client OAuth2.
- `client_secret` (String, sensible) — connu à la création et après chaque rotation, **jamais relu**. Nul pour un client public.
- `statut` (String) — developpement, en_revue, publiee, suspendue ou non_verifiee.
- `cree_le` (String)

## Import

```shell
tofu import recodanse_application.site appl_…
```

Une application importée n'a pas de secret dans l'état tant qu'on ne le fait pas tourner.
