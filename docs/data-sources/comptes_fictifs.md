---
page_title: "recodanse_comptes_fictifs (Data Source) - recodanse"
subcategory: "Bac à sable"
description: |-
  BAC À SABLE SEULEMENT — les comptes fictifs (des personas de démonstration).
---

# recodanse_comptes_fictifs (source de données)

**Bac à sable seulement** : refusée si l'environnement
du fournisseur n'est pas `bac-a-sable`. Les comptes fictifs au nom desquels on
répond « Autoriser » (« Continuer en tant que »).

## Exemple

```terraform
data "recodanse_comptes_fictifs" "tous" {}
```

## Schéma

### Lecture seule

- `comptes` (List of Object) — `cle` (à donner à `recodanse_evenement_du_bac_a_sable.compte`), `prenom`, `titre`.

## Contrat (supposé)

`GET /v1/bac-a-sable/comptes` → 200 `{"comptes":[{"cle","prenom","titre"}]}` ;
404 si le bac à sable n'est pas actif sur cette API.
