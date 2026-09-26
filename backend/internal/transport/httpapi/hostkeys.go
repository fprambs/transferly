package httpapi

import (
	"context"
	"net"
	"strconv"

	"github.com/fprambs/sentinel/internal/core/audit"
	"github.com/fprambs/sentinel/internal/core/hostkey"
)

// knownHostDTO converts a trusted key to its public representation.
func knownHostDTO(k hostkey.Key) KnownHost {
	return KnownHost{Host: k.Host, Port: k.Port, KeyType: k.Type, Fingerprint: k.Fingerprint, FirstSeen: k.FirstSeen}
}

// ListKnownHosts lists the server keys trusted in a workspace.
func (s *server) ListKnownHosts(ctx context.Context, request ListKnownHostsRequestObject) (ListKnownHostsResponseObject, error) {
	p, _ := principalFrom(ctx)
	keys, err := s.deps.HostKeys.List(ctx, p.User.ID, request.WorkspaceId)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	out := make(ListKnownHosts200JSONResponse, 0, len(keys))
	for _, k := range keys {
		out = append(out, knownHostDTO(k))
	}
	return out, nil
}

// TrustHostKey records a key the user confirmed.
func (s *server) TrustHostKey(ctx context.Context, request TrustHostKeyRequestObject) (TrustHostKeyResponseObject, error) {
	p, _ := principalFrom(ctx)
	b := request.Body
	key, err := s.deps.HostKeys.Trust(ctx, p.User.ID, request.WorkspaceId, b.Host, b.Port, b.KeyType, b.Fingerprint)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.record(ctx, p.User.ID, request.WorkspaceId, audit.HostKeyTrusted, hostPort(b.Host, b.Port), map[string]string{"type": b.KeyType})
	return TrustHostKey201JSONResponse(knownHostDTO(key)), nil
}

// ForgetHostKey removes a trusted key.
func (s *server) ForgetHostKey(ctx context.Context, request ForgetHostKeyRequestObject) (ForgetHostKeyResponseObject, error) {
	p, _ := principalFrom(ctx)
	q := request.Params
	err := s.deps.HostKeys.Forget(ctx, p.User.ID, request.WorkspaceId, q.Host, q.Port, q.KeyType)
	if problem, ok := asProblem(err); ok {
		return problem, nil
	}
	if err != nil {
		return nil, err
	}
	s.record(ctx, p.User.ID, request.WorkspaceId, audit.HostKeyForgotten, hostPort(q.Host, q.Port), map[string]string{"type": q.KeyType})
	return ForgetHostKey204Response{}, nil
}

// hostPort renders a server as host:port.
func hostPort(host string, port int) string {
	return net.JoinHostPort(host, strconv.Itoa(port))
}
