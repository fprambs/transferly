package httpapi

import (
	"context"
	"net/http"

	"github.com/fprambs/transferly/internal/core/connections"
	"github.com/fprambs/transferly/internal/core/quick"
	"github.com/fprambs/transferly/internal/core/site"
)

// quickDTO converts a quick connection to its public representation, which never holds credentials.
func quickDTO(c quick.Connection) QuickConnection {
	return QuickConnection{
		Id: c.Site.ID, Name: c.Site.Name, Protocol: QuickConnectionProtocol(c.Site.Protocol), FtpEncryption: encryptionDTO(c.Site.FTPEncryption), Host: c.Site.Host,
		Port: c.Site.Port, Username: c.Site.Username, AuthMethod: QuickConnectionAuthMethod(c.Site.AuthMethod), ListTimezone: c.Site.ListTimezone,
		Charset: Charset(c.Site.Charset), CreatedAt: c.Site.CreatedAt,
	}
}

// secretBytes returns the bytes of an optional text, or nil when it was left out.
func secretBytes(text *string) []byte {
	if text == nil {
		return nil
	}
	return []byte(*text)
}

// ListQuickConnections lists the user's quick connections of a workspace.
func (s *server) ListQuickConnections(ctx context.Context, request ListQuickConnectionsRequestObject) (ListQuickConnectionsResponseObject, error) {
	p, _ := principalFrom(ctx)
	list, err := s.deps.Quick.List(ctx, p.User.ID, request.WorkspaceId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(ListQuickConnections200JSONResponse, 0, len(list))
	for _, c := range list {
		out = append(out, quickDTO(c))
	}
	return out, nil
}

// CreateQuickConnection starts keeping a server the user wants to try.
func (s *server) CreateQuickConnection(ctx context.Context, request CreateQuickConnectionRequestObject) (CreateQuickConnectionResponseObject, error) {
	p, _ := principalFrom(ctx)
	if ok, wait := s.deps.Limits.QuickConnect.Allow("quick:" + p.User.ID.String()); !ok {
		return CreateQuickConnection429ApplicationProblemPlusJSONResponse{tooManyAttempts(wait)}, nil
	}
	b := request.Body
	in := site.Input{Protocol: site.Protocol(b.Protocol), Host: b.Host, Username: b.Username}
	if b.Port != nil {
		in.Port = *b.Port
	}
	if b.FtpEncryption != nil {
		in.FTPEncryption = site.FTPEncryption(*b.FtpEncryption)
	}
	if b.AuthMethod != nil {
		in.AuthMethod = site.AuthMethod(*b.AuthMethod)
	}
	if b.ListTimezone != nil {
		in.ListTimezone = *b.ListTimezone
	}
	if b.Charset != nil {
		in.Charset = string(*b.Charset)
	}
	secrets := quick.Secrets{Password: secretBytes(b.Password), PrivateKey: secretBytes(b.PrivateKey), Passphrase: secretBytes(b.Passphrase)}
	created, err := s.deps.Quick.Create(ctx, p.User.ID, request.WorkspaceId, in, secrets)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	return CreateQuickConnection201JSONResponse(quickDTO(created)), nil
}

// CloseQuickConnection ends a quick connection and its pooled connections.
func (s *server) CloseQuickConnection(ctx context.Context, request CloseQuickConnectionRequestObject) (CloseQuickConnectionResponseObject, error) {
	p, _ := principalFrom(ctx)
	if err := s.deps.Quick.Close(p.User.ID, request.WorkspaceId, request.QuickId); err != nil {
		return domainProblem{status: http.StatusNotFound, code: "quick_connection_not_found"}, nil
	}
	s.deps.Connections.CloseKey(connections.Key{UserID: p.User.ID, WorkspaceID: request.WorkspaceId, SiteID: request.QuickId})
	return CloseQuickConnection204Response{}, nil
}

// SaveQuickConnection keeps a quick connection as a site.
func (s *server) SaveQuickConnection(ctx context.Context, request SaveQuickConnectionRequestObject) (SaveQuickConnectionResponseObject, error) {
	p, _ := principalFrom(ctx)
	saved, err := s.deps.Quick.Save(ctx, p.User.ID, request.WorkspaceId, request.QuickId, vaultKey(p), request.Body.Name, optionalID(request.Body.CollectionId))
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.deps.Connections.CloseKey(connections.Key{UserID: p.User.ID, WorkspaceID: request.WorkspaceId, SiteID: request.QuickId})
	return SaveQuickConnection201JSONResponse(siteDTO(saved)), nil
}
