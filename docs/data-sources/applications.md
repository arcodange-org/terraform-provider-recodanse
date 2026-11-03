---
page_title: "recodanse_applications (Data Source) - recodanse"
subcategory: ""
description: |-
  Toutes tes applications, la plus récente d'abord — sans secret.
---

# recodanse_applications (source de données)

## Exemple

```terraform
data "recodanse_applications" "toutes" {}

output "noms" {
  value = [for a in data.recodanse_applications.toutes.applications : a.nom]
}
```

## Schéma

### Lecture seule

- `applications` (List of Object) — chaque objet porte les attributs de la source `recodanse_application`.
- `max` (Number) — combien d'applications un compte peut déclarer.
