package fzimport

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/netguard"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/workspace"
)

// Sites is what the import needs from the site service.
type Sites interface {
	// Create saves a site and its secrets.
	Create(ctx context.Context, actor, workspaceID uuid.UUID, session string, in site.Input, changes site.SecretChanges) (site.Site, error)
	// List returns the sites of a workspace.
	List(ctx context.Context, actor, workspaceID uuid.UUID) ([]site.Site, error)
	// CreateCollection creates a collection inside another one, or at the top level.
	CreateCollection(ctx context.Context, actor, workspaceID uuid.UUID, name string, parentID uuid.UUID) (site.Collection, error)
	// ListCollections returns the collections of a workspace.
	ListCollections(ctx context.Context, actor, workspaceID uuid.UUID) ([]site.Collection, error)
	// CanSeal reports whether the session can store secrets, which needs the vault to be unlocked.
	CanSeal(session string) bool
}

// Authorizer decides whether a user may manage the sites of a workspace.
type Authorizer interface {
	// Authorize returns the role of the user or an error that says why the action is refused.
	Authorize(ctx context.Context, userID, workspaceID uuid.UUID, action workspace.Action) (workspace.Role, error)
}

// Conflict says what to do with a site whose name is already taken.
type Conflict string

const (
	// SkipName leaves the site out.
	SkipName Conflict = "skip"
	// RenameSite imports the site under a free name such as "Name (2)".
	RenameSite Conflict = "rename"
)

// Status is what happened, or would happen, to a site.
type Status string

const (
	// StatusCreated means the site was saved.
	StatusCreated Status = "created"
	// StatusWouldCreate means the site would be saved; nothing was written.
	StatusWouldCreate Status = "would_create"
	// StatusSkipped means the site was left out; the reason says why.
	StatusSkipped Status = "skipped"
	// StatusFailed means the site could not be saved; the reason says why.
	StatusFailed Status = "failed"
)

// Entry is one site to import, whatever file it came from: its folders, its settings and the secrets the file gives it.
type Entry struct {
	// Path is the names of the folders that hold it, outermost first.
	Path []string
	// Input is its settings, named as it should be.
	Input site.Input
	// Secrets are the secrets the file gives it; stored only when the import asks for them.
	Secrets site.SecretChanges
	// PasswordSkipped says the file has a password that could not be read.
	PasswordSkipped bool
}

// hasSecrets reports whether an entry carries any secret.
func (e Entry) hasSecrets() bool {
	c := e.Secrets
	for _, value := range []*string{c.Password, c.PrivateKey, c.Passphrase, c.JumpPassword, c.JumpPrivateKey, c.JumpPassphrase, c.ProxyPassword, c.Account} {
		if value != nil {
			return true
		}
	}
	return false
}

// entryOf turns a server of a FileZilla file into an entry.
func entryOf(server Server) Entry {
	e := Entry{
		Path: server.Path, PasswordSkipped: server.PasswordSkipped,
		Input: site.Input{Name: server.Name, Protocol: server.Protocol, FTPEncryption: server.FTPEncryption, Host: server.Host, Port: server.Port, Username: server.Username, AuthMethod: server.AuthMethod, RemoteDir: server.RemoteDir},
	}
	if server.Password != "" {
		e.Secrets.Password = &server.Password
	}
	if server.Account != "" {
		e.Secrets.Account = &server.Account
	}
	return e
}

// Options tell how to import.
type Options struct {
	// DryRun reports what would happen without writing anything.
	DryRun bool
	// Passwords stores the passwords, and any other secret, of the file in the vault; it needs the vault to be unlocked.
	Passwords bool
	// OnConflict says what to do with names that are taken; skip when empty.
	OnConflict Conflict
	// Into is the collection that receives everything; the nil identifier imports at the top level.
	Into uuid.UUID
}

// Item is what happened to one site of the file.
type Item struct {
	// Name is the name the site has, or would have.
	Name string
	// Path is the folders that hold it, joined with slashes.
	Path string
	// Status is what happened to it.
	Status Status
	// Reason is the stable code that says why a site was skipped or failed; empty otherwise.
	Reason string
	// PasswordStored says the password of the file was saved with the site.
	PasswordStored bool
	// PasswordSkipped says the file had a password that was not saved, because it could not be read or the import did not ask for it.
	PasswordSkipped bool
}

// Result is the outcome of an import.
type Result struct {
	// Items has one entry per site of the file, in the order of the file, sites that cannot be represented last.
	Items []Item
}

// Created counts the sites that were created.
func (r Result) Created() int {
	n := 0
	for _, item := range r.Items {
		if item.Status == StatusCreated {
			n++
		}
	}
	return n
}

// Service imports site managers.
type Service struct {
	authz Authorizer
	sites Sites
}

// NewService returns a service.
func NewService(authz Authorizer, sites Sites) *Service {
	return &Service{authz: authz, sites: sites}
}

// failures maps what creating a site can refuse to the stable code of its reason.
var failures = []struct {
	err  error
	code string
}{
	{site.ErrInvalidName, "invalid_name"},
	{site.ErrInvalidHost, "invalid_host"},
	{site.ErrInvalidPort, "invalid_port"},
	{site.ErrInvalidProtocol, "invalid_protocol"},
	{site.ErrInvalidUsername, "invalid_username"},
	{site.ErrInvalidAuthMethod, "invalid_auth_method"},
	{site.ErrSecretTooLarge, "secret_too_large"},
	{site.ErrNameTaken, "name_taken"},
	{netguard.ErrBlocked, "destination_blocked"},
	{netguard.ErrUnresolvable, "host_unresolvable"},
	{site.ErrCollectionTooDeep, "collection_too_deep"},
	{site.ErrCollectionNameTaken, "collection_name_taken"},
}

// reasonOf returns the code of a failure that belongs to one site, or "" for any other error.
func reasonOf(err error) string {
	for _, f := range failures {
		if errors.Is(err, f.err) {
			return f.code
		}
	}
	return ""
}

// run is the state of one import.
type run struct {
	s        *Service
	ctx      context.Context
	actor    uuid.UUID
	ws       uuid.UUID
	session  string
	opts     Options
	names    map[string]bool
	folders  map[string]uuid.UUID
	children map[uuid.UUID]map[string]uuid.UUID
}

// Import creates the sites of a parsed FileZilla file. Sites are independent: one that fails does not stop the others. With
// Passwords the vault must be unlocked, which is checked before anything is written.
func (s *Service) Import(ctx context.Context, actor, workspaceID uuid.UUID, session string, servers []Server, skipped []Skipped, opts Options) (Result, error) {
	entries := make([]Entry, len(servers))
	for i, server := range servers {
		entries[i] = entryOf(server)
	}
	return s.ImportEntries(ctx, actor, workspaceID, session, nil, entries, skipped, opts)
}

// ImportEntries creates the folders and the sites of any file: first every folder of folders, which keeps the empty ones, then
// each entry in the folders of its path. It works as [Service.Import] does.
func (s *Service) ImportEntries(ctx context.Context, actor, workspaceID uuid.UUID, session string, folders [][]string, entries []Entry, skipped []Skipped, opts Options) (Result, error) {
	if _, err := s.authz.Authorize(ctx, actor, workspaceID, workspace.ActionManageSites); err != nil {
		return Result{}, err
	}
	if opts.OnConflict == "" {
		opts.OnConflict = SkipName
	}
	if opts.Passwords && !opts.DryRun && !s.sites.CanSeal(session) && slices.ContainsFunc(entries, Entry.hasSecrets) {
		return Result{}, site.ErrLocked
	}
	existing, err := s.sites.List(ctx, actor, workspaceID)
	if err != nil {
		return Result{}, err
	}
	all, err := s.sites.ListCollections(ctx, actor, workspaceID)
	if err != nil {
		return Result{}, err
	}
	r := &run{s: s, ctx: ctx, actor: actor, ws: workspaceID, session: session, opts: opts, names: map[string]bool{}, folders: map[string]uuid.UUID{}, children: map[uuid.UUID]map[string]uuid.UUID{}}
	for _, e := range existing {
		r.names[strings.ToLower(e.Name)] = true
	}
	for _, c := range all {
		if r.children[c.ParentID] == nil {
			r.children[c.ParentID] = map[string]uuid.UUID{}
		}
		r.children[c.ParentID][strings.ToLower(c.Name)] = c.ID
	}
	if !opts.DryRun {
		for _, folder := range folders {
			if _, err := r.folderFor(folder); err != nil && reasonOf(err) == "" {
				return Result{}, err
			}
		}
	}
	var result Result
	for _, entry := range entries {
		item, err := r.one(entry)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, item)
	}
	for _, out := range skipped {
		result.Items = append(result.Items, Item{Name: out.Name, Path: strings.Join(out.Path, "/"), Status: StatusSkipped, Reason: out.Reason})
	}
	return result, nil
}

// freeName returns the name a site is given: its own, or the first free "Name (n)" when the policy renames, or "" to skip it.
func (r *run) freeName(name string) string {
	if !r.names[strings.ToLower(name)] {
		return name
	}
	if r.opts.OnConflict != RenameSite {
		return ""
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s (%d)", name, n)
		if !r.names[strings.ToLower(candidate)] {
			return candidate
		}
	}
}

// folderFor returns the collection that receives the sites of a path, creating the collections that are missing.
func (r *run) folderFor(path []string) (uuid.UUID, error) {
	parent := r.opts.Into
	for i, name := range path {
		key := strings.ToLower(strings.Join(path[:i+1], "/"))
		if id, ok := r.folders[key]; ok {
			parent = id
			continue
		}
		id, ok := r.children[parent][strings.ToLower(name)]
		if !ok {
			created, err := r.s.sites.CreateCollection(r.ctx, r.actor, r.ws, name, parent)
			if err != nil {
				return uuid.Nil, err
			}
			id = created.ID
			if r.children[parent] == nil {
				r.children[parent] = map[string]uuid.UUID{}
			}
			r.children[parent][strings.ToLower(name)] = id
		}
		r.folders[key] = id
		parent = id
	}
	return parent, nil
}

// one imports, or plans, a single site.
func (r *run) one(entry Entry) (Item, error) {
	secrets := entry.hasSecrets()
	item := Item{Name: entry.Input.Name, Path: strings.Join(entry.Path, "/"), PasswordSkipped: entry.PasswordSkipped || (!r.opts.Passwords && secrets)}
	name := r.freeName(entry.Input.Name)
	if name == "" {
		item.Status, item.Reason = StatusSkipped, "name_taken"
		return item, nil
	}
	item.Name = name
	input := entry.Input
	input.Name = name
	if _, err := site.Normalize(input); err != nil {
		item.Status, item.Reason = StatusFailed, reasonOf(err)
		return item, nil
	}
	if r.opts.DryRun {
		r.names[strings.ToLower(name)] = true
		item.Status, item.PasswordStored = StatusWouldCreate, r.opts.Passwords && secrets
		return item, nil
	}
	folder, err := r.folderFor(entry.Path)
	if err != nil {
		if reason := reasonOf(err); reason != "" {
			item.Status, item.Reason = StatusFailed, reason
			return item, nil
		}
		return item, err
	}
	input.CollectionID = folder
	var changes site.SecretChanges
	if r.opts.Passwords {
		changes = entry.Secrets
	}
	if _, err := r.s.sites.Create(r.ctx, r.actor, r.ws, r.session, input, changes); err != nil {
		if reason := reasonOf(err); reason != "" {
			item.Status, item.Reason = StatusFailed, reason
			return item, nil
		}
		return item, err
	}
	r.names[strings.ToLower(name)] = true
	item.Status, item.PasswordStored = StatusCreated, r.opts.Passwords && secrets
	return item, nil
}
