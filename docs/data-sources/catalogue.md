---
page_title: "recodanse_catalogue (Data Source) - recodanse"
subcategory: ""
description: |-
  Les espèces d'applications, les portées de chacune, les événements des webhooks — et les portées que l'API annonce.
---

# recodanse_catalogue (source de données)

Le catalogue que le fournisseur applique au plan, et ce que l'API **annonce**
(`/.well-known/oauth-protected-resource`, `scopes_supported` ; route publique,
aucun jeton n'y part). Un écart entre les deux produit un **avertissement** : le
fournisseur est à mettre à jour.

La version des conditions des développeurs en vigueur n'y est **pas** : elle se
lit sur dev.recodanse.fr et s'écrit de sa main.

## Exemple

```terraform
data "recodanse_catalogue" "c" {}

output "portees_d_un_service" {
  value = data.recodanse_catalogue.c.portees["service"]
}
```

## Schéma

### Lecture seule

- `especes` (List of String) — agent, connexion, service.
- `especes_declarables` (List of String) — connexion, service (l'agent s'enregistre seul, RFC 7591).
- `portees` (Map of List of String) — les portées permises, par espèce.
- `evenements_des_webhooks` (List of String)
- `portees_annoncees` (List of String) — ce que l'API annonce.
