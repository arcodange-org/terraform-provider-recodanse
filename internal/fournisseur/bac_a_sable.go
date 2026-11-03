package fournisseur

// bac_a_sable.go — BAC À SABLE SEULEMENT : une
// source de données et une ressource par scénario du bac à sable.
//
//   - `recodanse_comptes_fictifs` : les comptes fictifs (des personas de
//     démonstration) au nom desquels on peut répondre « Autoriser » ;
//   - `recodanse_evenement_du_bac_a_sable` : déclencher, sur les webhooks d'une
//     de SES applications, `autorisation.retiree` ou `compte.efface` pour un
//     compte fictif qui l'a autorisée.
//
// ⚠ CE N'EST PAS UN OBJET QUI DURE : un événement se DÉCLENCHE. Créer la
// ressource le déclenche une fois ; la détruire ne fait RIEN (un envoi parti ne
// se reprend pas) ; changer `triggers` (comme les `triggers` de
// `terraform_data`) le redéclenche. `autorisation.retiree` retire VRAIMENT
// l'accord : pour redéclencher, le compte fictif doit d'abord autoriser de
// nouveau l'application — sinon l'API répond 409 `pas_autorisee`.
//
// ⚠ HORS DU BAC À SABLE, LES DEUX SE REFUSENT DÈS LE PLAN : l'environnement du
// fournisseur doit être « bac-a-sable ». L'API, elle, ne monte ces routes
// qu'au bac à sable (ailleurs : 404) — la double ceinture évite qu'un `apply`
// en production découvre l'erreur à mi-chemin.

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/arcodange-org/terraform-provider-recodanse/internal/api"
	"github.com/arcodange-org/terraform-provider-recodanse/internal/environnement"
)

// EvenementsDeclenchables — ce qu'on déclenche au bac à sable.
var EvenementsDeclenchables = []string{"autorisation.retiree", "compte.efface"}

// horsDuBacASable — l'erreur, si le fournisseur ne parle pas au bac à sable.
func horsDuBacASable(c *api.Client, quoi string, diags *diag.Diagnostics) bool {
	if c == nil || c.Environnement == environnement.BacASable {
		return false
	}
	diags.AddError("Bac à sable seulement",
		fmt.Sprintf("%s n'existe qu'au bac à sable des développeurs ; le fournisseur parle à « %s ». "+
			"Écris environnement = \"bac-a-sable\" (avec api et emetteur tant que ses adresses ne sont pas confirmées).", quoi, c.Environnement))
	return true
}

// expliquer404 — un 404 de ces routes : l'application, le compte, ou le bac à sable absent de l'API.
func expliquer404(err error) string {
	if api.EstIntrouvable(err) {
		return err.Error() + " — l'application n'est pas à ce compte, ou cette API n'a pas de bac à sable actif (ses routes n'existent qu'au bac à sable)"
	}
	return err.Error()
}

// ── recodanse_comptes_fictifs ────────────────────────────────────────────────

type sourceComptesFictifs struct{ client *api.Client }

func nouveauxComptesFictifs() datasource.DataSource { return &sourceComptesFictifs{} }

func (s *sourceComptesFictifs) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_comptes_fictifs"
}

func (s *sourceComptesFictifs) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "BAC À SABLE SEULEMENT — les comptes fictifs (des personas de démonstration) au nom desquels on répond « Autoriser ».",
		Attributes: map[string]dschema.Attribute{
			"comptes": dschema.ListNestedAttribute{Computed: true,
				Description: "Chaque compte : `cle` (à donner à recodanse_evenement_du_bac_a_sable.compte), `prenom`, `titre`.",
				NestedObject: dschema.NestedAttributeObject{Attributes: map[string]dschema.Attribute{
					"cle":    dschema.StringAttribute{Computed: true},
					"prenom": dschema.StringAttribute{Computed: true},
					"titre":  dschema.StringAttribute{Computed: true},
				}}},
		},
	}
}

func (s *sourceComptesFictifs) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	s.client = configurerSource(req, resp)
}

func (s *sourceComptesFictifs) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	if s.client == nil {
		resp.Diagnostics.AddError("Fournisseur non configuré", "lire les comptes fictifs exige un jeton")
		return
	}
	if horsDuBacASable(s.client, "recodanse_comptes_fictifs", &resp.Diagnostics) {
		return
	}
	cs, err := s.client.ComptesFictifs(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Lire les comptes fictifs", expliquer404(err))
		return
	}
	typ := types.ObjectType{AttrTypes: map[string]attr.Type{"cle": types.StringType, "prenom": types.StringType, "titre": types.StringType}}
	objets := make([]attr.Value, 0, len(cs))
	for _, c := range cs {
		o, d := types.ObjectValue(typ.AttrTypes, map[string]attr.Value{
			"cle": types.StringValue(c.Cle), "prenom": types.StringValue(c.Prenom), "titre": types.StringValue(c.Titre)})
		resp.Diagnostics.Append(d...)
		objets = append(objets, o)
	}
	liste, d := types.ListValue(typ, objets)
	resp.Diagnostics.Append(d...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("comptes"), liste)...)
}

// ── recodanse_evenement_du_bac_a_sable ───────────────────────────────────────

type modeleEvenement struct {
	ID          types.String `tfsdk:"id"`
	Application types.String `tfsdk:"application"`
	Compte      types.String `tfsdk:"compte"`
	Evenement   types.String `tfsdk:"evenement"`
	Triggers    types.Map    `tfsdk:"triggers"`
	Sujet       types.String `tfsdk:"sujet"`
	DeclencheLe types.String `tfsdk:"declenche_le"`
}

type ressourceEvenement struct {
	client     *api.Client
	maintenant func() time.Time
}

var _ resource.ResourceWithModifyPlan = (*ressourceEvenement)(nil)

func nouvelEvenementDuBacASable() resource.Resource { return &ressourceEvenement{} }

func (r *ressourceEvenement) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_evenement_du_bac_a_sable"
}

func (r *ressourceEvenement) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	remplacer := []planmodifier.String{stringplanmodifier.RequiresReplace()}
	calcule := []planmodifier.String{stringplanmodifier.UseStateForUnknown()}
	resp.Schema = schema.Schema{
		Description: "BAC À SABLE SEULEMENT — déclencher `autorisation.retiree` ou `compte.efface` sur les webhooks d'une de " +
			"tes applications, pour un compte fictif qui l'a autorisée. Créer = déclencher une fois ; " +
			"changer `triggers` = redéclencher ; détruire ne fait rien (un envoi parti ne se reprend pas).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{Computed: true, PlanModifiers: calcule,
				Description: "<application>/<compte>/<evenement>/<instant> — propre à ce fournisseur, l'API n'en garde pas."},
			"application": schema.StringAttribute{Required: true, PlanModifiers: remplacer,
				Description: "L'identifiant `appl_…` d'une de TES applications."},
			"compte": schema.StringAttribute{Required: true, PlanModifiers: remplacer,
				Description: "La `cle` d'un compte fictif (source recodanse_comptes_fictifs) qui a autorisé l'application."},
			"evenement": schema.StringAttribute{Required: true, PlanModifiers: remplacer,
				Validators:  []validator.String{stringvalidator.OneOf(EvenementsDeclenchables...)},
				Description: "autorisation.retiree ou compte.efface. Les deux retirent VRAIMENT l'accord ; compte.efface n'efface pas le compte fictif (il sert à tous)."},
			"triggers": schema.MapAttribute{Optional: true, ElementType: types.StringType,
				PlanModifiers: []planmodifier.Map{mapplanmodifier.RequiresReplace()},
				Description:   "Des valeurs libres : quand l'une change, l'événement se redéclenche (le compte doit avoir autorisé de nouveau)."},
			"sujet": schema.StringAttribute{Computed: true, PlanModifiers: calcule,
				Description: "Le pseudonyme du compte fictif POUR cette application — celui que ton webhook reçoit."},
			"declenche_le": schema.StringAttribute{Computed: true, PlanModifiers: calcule, Description: "RFC 3339."},
		},
	}
}

func (r *ressourceEvenement) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

// ModifyPlan — hors du bac à sable, refusé AU PLAN (création comme destruction
// n'ont de sens qu'au bac à sable ; une destruction, qui ne fait rien, passe).
func (r *ressourceEvenement) ModifyPlan(_ context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	horsDuBacASable(r.client, "recodanse_evenement_du_bac_a_sable", &resp.Diagnostics)
}

func (r *ressourceEvenement) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan modeleEvenement
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() || horsDuBacASable(r.client, "recodanse_evenement_du_bac_a_sable", &resp.Diagnostics) {
		return
	}
	ev, err := r.client.DeclencherUnEvenement(ctx, plan.Application.ValueString(), plan.Compte.ValueString(), plan.Evenement.ValueString())
	if err != nil {
		detail := expliquer404(err)
		if api.CodeDe(err) == "pas_autorisee" {
			detail += " — au bac à sable, réponds « Autoriser » au nom de ce compte fictif (« Continuer en tant que »), puis relance l'apply."
		}
		resp.Diagnostics.AddError("Déclencher l'événement du bac à sable", detail)
		return
	}
	maintenant := time.Now
	if r.maintenant != nil {
		maintenant = r.maintenant
	}
	instant := maintenant().UTC().Format(time.RFC3339)
	plan.ID = types.StringValue(fmt.Sprintf("%s/%s/%s/%s", plan.Application.ValueString(), plan.Compte.ValueString(),
		plan.Evenement.ValueString(), instant))
	plan.Sujet = types.StringValue(ev.Sujet)
	plan.DeclencheLe = types.StringValue(instant)
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

// Read — rien à relire : l'API ne garde pas « un déclenchement » comme un objet.
func (r *ressourceEvenement) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var etat modeleEvenement
	resp.Diagnostics.Append(req.State.Get(ctx, &etat)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, etat)...)
}

// Update — jamais appelée : tout attribut configurable redéclenche.
func (r *ressourceEvenement) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Événement non modifiable", "un événement se redéclenche ; ce chemin ne devrait pas être atteint.")
}

// Delete — NE FAIT RIEN, exprès : un envoi parti ne se reprend pas. L'état oublie.
func (r *ressourceEvenement) Delete(context.Context, resource.DeleteRequest, *resource.DeleteResponse) {
}
