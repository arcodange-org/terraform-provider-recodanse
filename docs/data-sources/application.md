---
page_title: "recodanse_application (Data Source) - recodanse"
subcategory: ""
description: |-
  Une de tes applications, lue par son identifiant — sans son secret.
---

# recodanse_application (source de données)

## Exemple

```terraform
data "recodanse_application" "site" {
  id = "appl_…"
}
```

## Schéma

### Requis

- `id` (String) — `appl_…`. Une application d'un autre compte rend la même erreur qu'une absente.

### Lecture seule

- `client_id`, `nom`, `description`, `site_web`, `categorie`, `statut`, `cree_le` (String)
- `portees`, `redirections` (List of String, triées)
- `publique` (Boolean)
- `conditions_acceptees` (String) — la version acceptée (nulle si aucune).
- `conditions_a_jour` (Boolean)

Le secret n'est **jamais** lisible : l'API ne le montre qu'une fois.
