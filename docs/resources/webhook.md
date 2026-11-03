---
page_title: "recodanse_webhook (Resource) - recodanse"
subcategory: ""
description: |-
  Un webhook d'une application : ReCoDanse y envoie, signé Standard Webhooks, ce que l'application doit savoir.
---

# recodanse_webhook (ressource)

Un webhook ne se corrige pas : changer l'adresse ou les événements le **recrée**,
avec une nouvelle clé de signature `whsec_…`.

## Exemple

```terraform
resource "recodanse_webhook" "site" {
  application = recodanse_application.site.id
  url         = "https://festival.example/webhooks/recodanse"
  evenements  = ["autorisation.retiree", "compte.efface"]
}
```

## Schéma

### Requis

- `application` (String) — l'identifiant `appl_…`.
- `url` (String) — https:// seulement, hôte public.
- `evenements` (Set of String) — `autorisation.retiree`, `compte.efface`, `publication.retiree`.

### Lecture seule

- `id` (String) — `webh_…`.
- `secret` (String, sensible) — la clé `whsec_…`, connue à la pose seulement.
- `actif` (Boolean) — faux après un 410 du destinataire.

## Import

```shell
tofu import recodanse_webhook.site appl_…/webh_…
```
