package backup

import (
	"context"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/fzimport"
	"github.com/fprambs/transferly/internal/core/ports"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/core/workspace"
)

// Content is what a file seals: the collections and the sites of a workspace, with their secrets in clear text.
type Content struct {
	// ExportedAt is when the file was written.
	ExportedAt time.Time `json:"exportedAt"`
	// Collections are the paths of every collection, parents first, so the empty ones come back too.
	Collections [][]string `json:"collections"`
	// Sites are the sites, in the order of their names.
	Sites []Site `json:"sites"`
}

// Site is one site of a file: the settings of [site.Input], named as a file keeps them, and its secrets (ADR-0022).
type Site struct {
	// Name is the name of the site.
	Name string `json:"name"`
	// Path is the names of the collections that hold it, outermost first; empty at the top level.
	Path []string `json:"path"`
	// Connector is the type of its connector, such as sftp or ftp.
	Connector string `json:"connector"`
	// Config is the configuration of its connector.
	Config map[string]string `json:"config"`
	// SpeedLimit caps the bytes per second of the site's transfers; zero means no cap of its own.
	SpeedLimit int64 `json:"speedLimitBps,omitempty"`
	// RemoteDir is the folder the site opens in.
	RemoteDir string `json:"remoteDir,omitempty"`
	// Color is the label the site wears in the interface, if any.
	Color string `json:"color,omitempty"`
	// Secrets are the secrets of the site in clear text, by kind, such as password or private_key.
	Secrets map[string]string `json:"secrets,omitempty"`
}

// Sites is what the export needs from the site service.
type Sites interface {
	// List returns the sites of a workspace.
	List(ctx context.Context, actor, workspaceID uuid.UUID) ([]site.Site, error)
	// ListCollections returns the collections of a workspace.
	ListCollections(ctx context.Context, actor, workspaceID uuid.UUID) ([]site.Collection, error)
	// OpenSecret decrypts one secret of a site with the key of the session.
	OpenSecret(ctx context.Context, actor, workspaceID, id uuid.UUID, session string, kind site.SecretKind) ([]byte, error)
	// CanSeal reports whether the session has the vault unlocked.
	CanSeal(session string) bool
}

// Importer creates the sites of a file.
type Importer interface {
	// ImportEntries creates folders and sites as the FileZilla import does.
	ImportEntries(ctx context.Context, actor, workspaceID uuid.UUID, session string, folders [][]string, entries []fzimport.Entry, skipped []fzimport.Skipped, opts fzimport.Options) (fzimport.Result, error)
}

// Authorizer decides whether a user may manage the sites of a workspace.
type Authorizer interface {
	// Authorize returns the role of the user or an error that says why the action is refused.
	Authorize(ctx context.Context, userID, workspaceID uuid.UUID, action workspace.Action) (workspace.Role, error)
}

// Service exports and imports the sites of a workspace.
type Service struct {
	authz    Authorizer
	sites    Sites
	importer Importer
	sealer   *Sealer
	clock    ports.Clock
}

// NewService returns a service.
func NewService(authz Authorizer, sites Sites, importer Importer, sealer *Sealer, clock ports.Clock) *Service {
	return &Service{authz: authz, sites: sites, importer: importer, sealer: sealer, clock: clock}
}

// Export writes every collection and site of a workspace, with their secrets, to a file sealed with password, and says how many
// sites it holds. It needs the right to export secrets, which a team gives its admins and owner only, and, when a site has a secret,
// the vault unlocked in the session.
func (s *Service) Export(ctx context.Context, actor, workspaceID uuid.UUID, session, password string) ([]byte, int, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionExportSecrets); err != nil {
		return nil, 0, err
	}
	if err := checkPassword(password); err != nil {
		return nil, 0, err
	}
	sites, err := s.sites.List(ctx, actor, workspaceID)
	if err != nil {
		return nil, 0, err
	}
	if !s.sites.CanSeal(session) && slices.ContainsFunc(sites, func(x site.Site) bool { return len(x.Secrets) > 0 }) {
		return nil, 0, site.ErrLocked
	}
	collections, err := s.sites.ListCollections(ctx, actor, workspaceID)
	if err != nil {
		return nil, 0, err
	}
	paths := collectionPaths(collections)
	content := Content{ExportedAt: s.clock.Now().UTC(), Collections: make([][]string, 0, len(collections)), Sites: make([]Site, 0, len(sites))}
	for _, c := range collections {
		content.Collections = append(content.Collections, paths[c.ID])
	}
	slices.SortFunc(content.Collections, func(a, b []string) int { return len(a) - len(b) })
	for _, x := range sites {
		exported := siteOf(x, paths[x.CollectionID])
		for _, kind := range x.Secrets {
			value, err := s.sites.OpenSecret(ctx, actor, workspaceID, x.ID, session, kind)
			if err != nil {
				return nil, 0, err
			}
			exported.Secrets[string(kind)] = string(value)
			clear(value)
		}
		content.Sites = append(content.Sites, exported)
	}
	file, err := s.sealer.Seal(ctx, password, content)
	return file, len(sites), err
}

// Import creates the collections and the sites of a file sealed with password, as the FileZilla import does: with a dry run, a
// policy for names that are taken, a collection that receives everything, and the secrets stored only when opts asks for them.
func (s *Service) Import(ctx context.Context, actor, workspaceID uuid.UUID, session, password string, file []byte, opts fzimport.Options) (fzimport.Result, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageSites); err != nil {
		return fzimport.Result{}, err
	}
	content, err := s.sealer.Open(ctx, password, file)
	if err != nil {
		return fzimport.Result{}, err
	}
	entries := make([]fzimport.Entry, 0, len(content.Sites))
	for _, x := range content.Sites {
		entries = append(entries, entryOf(x))
	}
	return s.importer.ImportEntries(ctx, actor, workspaceID, session, content.Collections, entries, nil, opts)
}

// checkPassword refuses an export password that breaks the policy, before any secret is opened.
func checkPassword(password string) error {
	if len(password) > maxPasswordBytes || utf8.RuneCountInString(password) < MinPasswordRunes {
		return ErrWeakPassword
	}
	return nil
}

// collectionPaths returns the path of names of every collection, outermost first.
func collectionPaths(collections []site.Collection) map[uuid.UUID][]string {
	byID := make(map[uuid.UUID]site.Collection, len(collections))
	for _, c := range collections {
		byID[c.ID] = c
	}
	paths := make(map[uuid.UUID][]string, len(collections))
	var pathOf func(id uuid.UUID) []string
	pathOf = func(id uuid.UUID) []string {
		if path, ok := paths[id]; ok {
			return path
		}
		c, ok := byID[id]
		if !ok {
			return nil
		}
		path := append(slices.Clone(pathOf(c.ParentID)), c.Name)
		paths[id] = path
		return path
	}
	for _, c := range collections {
		pathOf(c.ID)
	}
	return paths
}

// siteOf returns the settings of a site as a file keeps them, without secrets yet.
func siteOf(x site.Site, path []string) Site {
	return Site{
		Name: x.Name, Path: path, Connector: x.Connector, Config: x.Config, SpeedLimit: x.SpeedLimit, RemoteDir: x.RemoteDir, Color: string(x.Color),
		Secrets: map[string]string{},
	}
}

// entryOf turns a site of a file into an entry of the import. Its settings and secrets are checked against the schema of its connector when
// it is created, like any site's.
func entryOf(x Site) fzimport.Entry {
	in := site.Input{Name: x.Name, Connector: x.Connector, Config: x.Config, SpeedLimit: x.SpeedLimit, RemoteDir: x.RemoteDir, Color: site.Color(x.Color)}
	changes := make(site.SecretChanges, len(x.Secrets))
	for kind, value := range x.Secrets {
		changes[site.SecretKind(kind)] = value
	}
	return fzimport.Entry{Path: x.Path, Input: in, Secrets: changes}
}
