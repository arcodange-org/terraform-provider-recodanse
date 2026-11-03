package fournisseur

// webhook.go — la ressource `recodanse_webhook`.
//
// Un webhook ne se corrige pas : l'API n'a pas de route pour ça. Changer
// l'adresse ou les événements le RECRÉE — et son secret `whsec_…` avec lui, qui
// ne se montre qu'à la pose. KISS : une ressource qui ment sur sa mise à jour
// serait pire qu'une ressource qui se recrée en le disant.

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/arcodange-org/terraform-provider-recodanse/internal/api"
)

// EvenementsDesWebhooks — ce qu'un webhook peut écouter (le catalogue de
// `webhooks.Catalogue`, confronté par la même garde que les portées).
var EvenementsDesWebhooks = []string{"autorisation.retiree", "compte.efface", "publication.retiree"}

var regexpHTTPS = regexp.MustCompile(`^https://[^\s]+$`)

type modeleWebhook struct {
	ID          types.String `tfsdk:"id"`
	Application types.String `tfsdk:"application"`
	URL         types.String `tfsdk:"url"`
	Evenements  types.Set    `tfsdk:"evenements"`
	Secret      types.String `tfsdk:"secret"`
	Actif       types.Bool   `tfsdk:"actif"`
}

type ressourceWebhook struct{ client *api.Client }

var (
	_ resource.ResourceWithConfigure   = (*ressourceWebhook)(nil)
	_ resource.ResourceWithImportState = (*ressourceWebhook)(nil)
)

func nouveauWebhook() resource.Resource { return &ressourceWebhook{} }

func (r *ressourceWebhook) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook"
}

func (r *ressourceWebhook) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	remplacer := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	resp.Schema = schema.Schema{
		Description: "Un webhook d'une application : ReCoDanse y envoie, signé « Standard Webhooks », ce que l'application doit savoir.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed: true, PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"application": schema.StringAttribute{
				Required: true, Description: "L'identifiant `appl_…` de l'application.", PlanModifiers: remplacer,
			},
			"url": schema.StringAttribute{
				Required: true, Description: "https:// seulement, joignable sur le réseau public. Changer recrée le webhook.",
				Validators:    []validator.String{stringvalidator.RegexMatches(regexpHTTPS, "une adresse https://")},
				PlanModifiers: remplacer,
			},
			"evenements": schema.SetAttribute{
				Required: true, ElementType: types.StringType,
				Description: "autorisation.retiree, compte.efface, publication.retiree. Changer recrée le webhook.",
				Validators: []validator.Set{setvalidator.SizeAtLeast(1),
					setvalidator.ValueStringsAre(stringvalidator.OneOf(EvenementsDesWebhooks...))},
				PlanModifiers: []planmodifier.Set{setplanmodifier.RequiresReplace()},
			},
			"secret": schema.StringAttribute{
				Computed: true, Sensitive: true,
				Description:   "La clé de signature `whsec_…`, connue à la pose seulement.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"actif": schema.BoolAttribute{Computed: true, Description: "Faux après un 410 du destinataire."},
		},
	}
}

func (r *ressourceWebhook) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	c, ok := clientDe(req.ProviderData)
	if !ok {
		resp.Diagnostics.AddError("Fournisseur mal configuré", fmt.Sprintf("client inattendu : %T", req.ProviderData))
		return
	}
	r.client = c
}

func (r *ressourceWebhook) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan modeleWebhook
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	k, err := r.client.PoserUnWebhook(ctx, plan.Application.ValueString(), plan.URL.ValueString(),
		chaines(ctx, plan.Evenements, &resp.Diagnostics))
	if err != nil {
		resp.Diagnostics.AddError("Poser le webhook", err.Error())
		return
	}
	plan.ID, plan.Secret, plan.Actif = types.StringValue(k.ID), secretOuNul(k.Secret), types.BoolValue(k.Actif)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *ressourceWebhook) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var etat modeleWebhook
	resp.Diagnostics.Append(req.State.Get(ctx, &etat)...)
	if resp.Diagnostics.HasError() {
		return
	}
	ks, err := r.client.Webhooks(ctx, etat.Application.ValueString())
	if api.EstIntrouvable(err) {
		resp.State.RemoveResource(ctx) // l'application est partie, ses webhooks avec elle
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Lire les webhooks", err.Error())
		return
	}
	for _, k := range ks {
		if k.ID != etat.ID.ValueString() {
			continue
		}
		ev, d := types.SetValueFrom(ctx, types.StringType, k.Evenements)
		resp.Diagnostics.Append(d...)
		etat.URL, etat.Evenements, etat.Actif = types.StringValue(k.URL), ev, types.BoolValue(k.Actif)
		if etat.Secret.IsUnknown() {
			etat.Secret = types.StringNull()
		}
		resp.Diagnostics.Append(resp.State.Set(ctx, etat)...)
		return
	}
	resp.State.RemoveResource(ctx)
}

// Update — jamais appelée : tout attribut configurable recrée le webhook.
func (r *ressourceWebhook) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Webhook non modifiable", "un webhook se recrée ; ce chemin ne devrait pas être atteint.")
}

func (r *ressourceWebhook) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var etat modeleWebhook
	resp.Diagnostics.Append(req.State.Get(ctx, &etat)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.client.RetirerUnWebhook(ctx, etat.ID.ValueString()); err != nil && !api.EstIntrouvable(err) {
		resp.Diagnostics.AddError("Retirer le webhook", err.Error())
	}
}

// ImportState — `appl_…/webh_…` : un webhook se lit par son application.
func (r *ressourceWebhook) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	app, webh, ok := strings.Cut(req.ID, "/")
	if !ok || !strings.HasPrefix(app, "appl_") || !strings.HasPrefix(webh, "webh_") {
		resp.Diagnostics.AddError("Identifiant d'import", "attendu : appl_…/webh_… (l'application, puis le webhook)")
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("application"), app)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), webh)...)
}
