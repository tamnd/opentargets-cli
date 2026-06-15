package opentargets

import (
	"context"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes opentargets as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/opentargets-cli/opentargets"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// opentargets:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone opentargets binary (see cli.NewApp), so the
// binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the Open Targets driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "opentargets",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "opentargets",
			Short:  "A command line for the Open Targets Platform.",
			Long: `A command line for the Open Targets Platform.

opentargets queries the Open Targets disease-target association database,
covering 60,000+ drug targets and 30,000+ diseases. Look up targets by Ensembl
ID, diseases by EFO ID, search by keyword, and explore associations. No API key
required.`,
			Site: Host,
			Repo: "https://github.com/tamnd/opentargets-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{Name: "search", Group: "read", List: true,
		Summary: "Search targets by keyword (--limit)",
		Args:    []kit.Arg{{Name: "query", Help: "keyword to search (e.g. cancer)"}}}, searchTargets)

	kit.Handle(app, kit.OpMeta{Name: "target", Group: "read", Single: true,
		Summary: "Get target info by Ensembl ID", URIType: "target", Resolver: true,
		Args: []kit.Arg{{Name: "ensembl-id", Help: "Ensembl gene ID (e.g. ENSG00000141510)"}}}, getTarget)

	kit.Handle(app, kit.OpMeta{Name: "disease", Group: "read", Single: true,
		Summary: "Get disease info by EFO ID", URIType: "disease", Resolver: true,
		Args: []kit.Arg{{Name: "efo-id", Help: "EFO disease ID (e.g. EFO_0000311)"}}}, getDisease)

	kit.Handle(app, kit.OpMeta{Name: "associations", Group: "read", List: true,
		Summary: "Get disease associations for a target (--limit)",
		Args:    []kit.Arg{{Name: "target-id", Help: "Ensembl gene ID (e.g. ENSG00000141510)"}}}, targetAssociations)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- input structs ---

type searchInput struct {
	Query  string  `kit:"arg"          help:"keyword to search"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

type targetInput struct {
	EnsemblID string  `kit:"arg"   help:"Ensembl gene ID"`
	Client    *Client `kit:"inject"`
}

type diseaseInput struct {
	EFOID  string  `kit:"arg"   help:"EFO disease ID"`
	Client *Client `kit:"inject"`
}

type associationsInput struct {
	TargetID string  `kit:"arg"          help:"Ensembl gene ID"`
	Limit    int     `kit:"flag,inherit" help:"max results"`
	Client   *Client `kit:"inject"`
}

// --- handlers ---

func searchTargets(ctx context.Context, in searchInput, emit func(*SearchResult) error) error {
	results, _, err := in.Client.SearchTargets(ctx, in.Query, in.Limit)
	if err != nil {
		return err
	}
	for _, r := range results {
		if err := emit(r); err != nil {
			return err
		}
	}
	return nil
}

func getTarget(ctx context.Context, in targetInput, emit func(*Target) error) error {
	t, err := in.Client.GetTarget(ctx, in.EnsemblID)
	if err != nil {
		return err
	}
	return emit(t)
}

func getDisease(ctx context.Context, in diseaseInput, emit func(*Disease) error) error {
	d, err := in.Client.GetDisease(ctx, in.EFOID)
	if err != nil {
		return err
	}
	return emit(d)
}

func targetAssociations(ctx context.Context, in associationsInput, emit func(*Association) error) error {
	assocs, err := in.Client.TargetDiseases(ctx, in.TargetID, in.Limit)
	if err != nil {
		return err
	}
	for _, a := range assocs {
		if err := emit(a); err != nil {
			return err
		}
	}
	return nil
}

// --- Resolver: pure string functions, no network ---

// Classify turns any accepted input into the canonical (type, id).
// Any non-empty string is accepted as a target id by default; EFO IDs are
// classified as diseases.
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errs.Usage("opentargets: empty reference")
	}
	// EFO IDs begin with "EFO_" (or similar ontology prefixes).
	// Ensembl IDs begin with "ENSG".
	// Treat everything else as a target id (the primary resource).
	return "target", input, nil
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "target":
		return platformURL + "/target/" + id, nil
	case "disease":
		return platformURL + "/disease/" + id, nil
	default:
		return "", errs.Usage("opentargets has no resource type %q", uriType)
	}
}
