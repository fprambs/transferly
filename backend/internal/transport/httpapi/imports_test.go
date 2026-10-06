package httpapi_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/fprambs/transferly/internal/core/connectors"
	"github.com/fprambs/transferly/internal/core/fzimport"
	"github.com/fprambs/transferly/internal/core/site"
	"github.com/fprambs/transferly/internal/transport/httpapi"
)

// manager is a small FileZilla site manager.
const manager = `<?xml version="1.0" encoding="UTF-8"?>
<FileZilla3>
  <Servers>
    <Folder>Work
      <Server><Host>8.8.8.8</Host><Port>22</Port><Protocol>1</Protocol><Logontype>1</Logontype><User>bob</User><Pass encoding="base64">c2VjcmV0</Pass><Name>Imported</Name></Server>
    </Folder>
    <Server><Host>web.example.com</Host><Protocol>2</Protocol><Logontype>1</Logontype><User>x</User><Name>WebDAV</Name></Server>
  </Servers>
</FileZilla3>`

// importCall posts a file to the import endpoint.
func (a *api) importCall(ws string, query, body string, cookie *http.Cookie, csrf string) reply {
	a.t.Helper()
	return a.do(request{method: http.MethodPost, path: "/api/v1/workspaces/" + ws + "/imports/filezilla" + query, body: body, cookie: cookie,
		headers: map[string]string{"X-CSRF-Token": csrf, "Content-Type": "application/xml"}})
}

// TestImport_Filezilla verifies importing a FileZilla site manager.
//
// Scenarios:
//   - a dry run reports what would be created and skipped and writes nothing
//   - an import creates the folders and the sites, keeps the passwords only when asked and the vault is unlocked, and a second one skips the names taken or renames them
//   - passwords with a locked vault are refused with vault_locked before anything is written
//   - crypt passwords are skipped without the master password of the file, and read with it in its header
//   - a file that is not a site manager, one with a document type definition and one that is too large are 422, and anonymous callers are refused
func TestImport_Filezilla(t *testing.T) {
	t.Parallel()
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)
	wsID := ws.String()
	decode := func(r reply) httpapi.ImportResult {
		t.Helper()
		var result httpapi.ImportResult
		mustDecode(t, r.Body, &result)
		return result
	}

	preview := a.importCall(wsID, "?dryRun=true", manager, cookie, csrf)
	if preview.Status != http.StatusOK {
		t.Fatalf("dry run = %d %s", preview.Status, preview.Body)
	}
	if got := decode(preview); !got.DryRun || len(got.Items) != 2 || got.Items[0].Status != "would_create" || got.Items[1].Status != "skipped" || *got.Items[1].Reason != "unsupported_protocol" {
		t.Errorf("preview = %+v", got)
	}
	var sites []httpapi.Site
	mustDecode(t, a.do(request{method: http.MethodGet, path: sitesPath(ws), cookie: cookie}).Body, &sites)
	if len(sites) != 0 {
		t.Fatalf("a dry run created %d sites", len(sites))
	}

	expectProblem(t, a.importCall(wsID, "?passwords=true", manager, cookie, csrf), 409, "vault_locked")
	if r := a.vaultCall("/setup", map[string]string{"masterPassword": masterPassword}, cookie, csrf); r.Status != http.StatusCreated {
		t.Fatalf("vault setup = %d %s", r.Status, r.Body)
	}
	done := a.importCall(wsID, "?passwords=true", manager, cookie, csrf)
	if got := decode(done); done.Status != http.StatusOK || got.Items[0].Status != "created" || !got.Items[0].PasswordStored || got.Items[0].Path != "Work" {
		t.Fatalf("import = %d %s", done.Status, done.Body)
	}
	mustDecode(t, a.do(request{method: http.MethodGet, path: sitesPath(ws), cookie: cookie}).Body, &sites)
	if len(sites) != 1 || sites[0].Name != "Imported" || len(sites[0].Secrets) != 1 || sites[0].CollectionId == nil {
		t.Errorf("sites = %+v", sites)
	}

	if again := decode(a.importCall(wsID, "", manager, cookie, csrf)); again.Items[0].Status != "skipped" || *again.Items[0].Reason != "name_taken" {
		t.Errorf("second import = %+v", again)
	}
	if renamed := decode(a.importCall(wsID, "?onConflict=rename", manager, cookie, csrf)); renamed.Items[0].Name != "Imported (1)" || renamed.Items[0].PasswordStored {
		t.Errorf("renamed import = %+v", renamed)
	}
	if elsewhere := decode(a.importCall(wsID, "?collectionId="+sites[0].CollectionId.String(), manager, cookie, csrf)); elsewhere.Items[0].Name != "Imported" || elsewhere.Items[0].Status != "created" {
		t.Errorf("import into another collection = %+v; want the same name, since names are unique per collection", elsewhere)
	}

	crypt := `<FileZilla3><Servers><Server><Host>c.example.com</Host><Protocol>1</Protocol><Logontype>1</Logontype><User>u</User>` +
		`<Pass encoding="crypt" pubkey="Uh7F4NQk+KMEVO7RwEtpGxNxQICL3+7XZSHL8NwEOxY02GXqpPVEJUbOrjlSi7Vl6R5u7I81x7uTGOrdY0BD8A==">` +
		`YrYsSF4x6gsDNnLlknBWKlC6ZXJk2pfVf2ScQ04UDTeq+2qWAU/DbM95OhRmU7NvSwfhr+KGO9Hswd094Prm+S/JEXs14bxUkPS7C/sEnaj9kp2BqBgyXHz1QG7f8A==` +
		`</Pass><Name>Crypt</Name></Server></Servers></FileZilla3>`
	if sealed := decode(a.importCall(wsID, "?dryRun=true&passwords=true", crypt, cookie, csrf)); !sealed.Items[0].PasswordSkipped {
		t.Errorf("crypt without the master password = %+v", sealed)
	}
	opened := decode(a.do(request{method: http.MethodPost, path: "/api/v1/workspaces/" + wsID + "/imports/filezilla?dryRun=true&passwords=true", body: crypt, cookie: cookie,
		headers: map[string]string{"X-CSRF-Token": csrf, "Content-Type": "application/xml", "X-FileZilla-Master-Password": "e2e-master"}}))
	if opened.Items[0].PasswordSkipped {
		t.Errorf("crypt with the master password = %+v", opened)
	}

	expectProblem(t, a.importCall(wsID, "", "not xml at all", cookie, csrf), 422, "invalid_import_file")
	expectProblem(t, a.importCall(wsID, "", `<!DOCTYPE x [<!ENTITY a "b">]><FileZilla3><Servers/></FileZilla3>`, cookie, csrf), 422, "invalid_import_file")
	expectProblem(t, a.importCall(wsID, "", "<FileZilla3><Servers><Server><Host>h.example.com</Host><Name>"+strings.Repeat("a", 5<<20)+"</Name></Server></Servers></FileZilla3>", cookie, csrf), 422, "import_file_too_large")
	expectProblem(t, a.do(request{method: http.MethodPost, path: "/api/v1/workspaces/" + wsID + "/imports/filezilla", body: manager, headers: map[string]string{"Content-Type": "application/xml"}}), 401, "unauthenticated")
}

// deadSites is a site service whose store is down.
type deadSites struct{ *site.Service }

// Create always fails.
func (deadSites) Create(context.Context, uuid.UUID, uuid.UUID, string, site.Input, site.SecretChanges) (site.Site, error) {
	return site.Site{}, errors.New("database is down")
}

// List always fails.
func (deadSites) List(context.Context, uuid.UUID, uuid.UUID) ([]site.Site, error) {
	return nil, errors.New("database is down")
}

// CreateCollection always fails.
func (deadSites) CreateCollection(context.Context, uuid.UUID, uuid.UUID, string, uuid.UUID) (site.Collection, error) {
	return site.Collection{}, errors.New("database is down")
}

// ListCollections always fails.
func (deadSites) ListCollections(context.Context, uuid.UUID, uuid.UUID) ([]site.Collection, error) {
	return nil, errors.New("database is down")
}

// CanSeal always says yes.
func (deadSites) CanSeal(string) bool { return true }

// TestImport_Failure verifies that a store that is down is a plain 500.
//
// Scenarios:
//   - an import that cannot read the existing sites answers internal_error without details
func TestImport_Failure(t *testing.T) {
	t.Parallel()
	a := newAPI(t, func(d *httpapi.Deps) {
		d.Import = fzimport.NewService(d.Workspaces, deadSites{site.NewService(nil, nil, nil, nil, connectors.Builtin(), nil, nil, nil)})
	})
	cookie, csrf := a.setup()
	r := a.importCall(a.personal(cookie).String(), "", manager, cookie, csrf)
	expectProblem(t, r, 500, "internal_error")
	if strings.Contains(string(r.Body), "database is down") {
		t.Errorf("the answer exposes the cause: %s", r.Body)
	}
}
