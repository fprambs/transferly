package httpapi_test

import (
	"context"
	"crypto/rand"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/fprambs/sentinel/internal/core/backup"
	"github.com/fprambs/sentinel/internal/core/fzimport"
	"github.com/fprambs/sentinel/internal/core/site"
	"github.com/fprambs/sentinel/internal/core/vault"
	"github.com/fprambs/sentinel/internal/testutil"
	"github.com/fprambs/sentinel/internal/transport/httpapi"
)

const exportPassword = "a long export password"

// exportCall asks for an export sealed with password.
func (a *api) exportCall(ws uuid.UUID, password string, cookie *http.Cookie, csrf string) reply {
	a.t.Helper()
	return a.send(http.MethodPost, "/api/v1/workspaces/"+ws.String()+"/exports", map[string]string{"password": password}, cookie, csrf)
}

// sentinelImport posts a file to the Sentinel import.
func (a *api) sentinelImport(ws uuid.UUID, query, password, file string, cookie *http.Cookie, csrf string) reply {
	a.t.Helper()
	return a.send(http.MethodPost, "/api/v1/workspaces/"+ws.String()+"/imports/sentinel"+query, map[string]string{"password": password, "file": file}, cookie, csrf)
}

// TestExport_Sentinel covers exporting the sites to an encrypted file and importing it back (ADR-0017).
//
// Use cases:
//   - a weak password is refused with weak_export_password
//   - the export answers a download that holds neither the sites nor their secrets in clear text
//   - importing it previews first, then creates the sites under free names with their secrets when asked, inside a chosen collection
//   - a wrong password is wrong_export_password, a file that is not an export is invalid_export, and a body over the limit is refused
//     before it is read
func TestExport_Sentinel(t *testing.T) {
	a := newAPI(t)
	cookie, csrf := a.setup()
	ws := a.personal(cookie)
	if r := a.vaultCall("/setup", map[string]string{"masterPassword": masterPassword}, cookie, csrf); r.Status != http.StatusCreated {
		t.Fatalf("vault setup = %d %s", r.Status, r.Body)
	}
	if r := a.importCall(ws.String(), "?passwords=true", manager, cookie, csrf); r.Status != http.StatusOK {
		t.Fatalf("import = %d %s", r.Status, r.Body)
	}

	expectProblem(t, a.exportCall(ws, "short", cookie, csrf), http.StatusUnprocessableEntity, "weak_export_password")
	exported := a.exportCall(ws, exportPassword, cookie, csrf)
	if exported.Status != http.StatusOK || !strings.Contains(exported.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("export = %d %s", exported.Status, exported.Header)
	}
	if strings.Contains(string(exported.Body), "Imported") || strings.Contains(string(exported.Body), "secret") {
		t.Error("the export shows its content in clear text")
	}
	file := string(exported.Body)

	decode := func(r reply) httpapi.ImportResult {
		t.Helper()
		if r.Status != http.StatusOK {
			t.Fatalf("import = %d %s", r.Status, r.Body)
		}
		var result httpapi.ImportResult
		mustDecode(t, r.Body, &result)
		return result
	}
	if preview := decode(a.sentinelImport(ws, "?dryRun=true&onConflict=rename", exportPassword, file, cookie, csrf)); !preview.DryRun || preview.Items[0].Status != "would_create" {
		t.Errorf("preview = %+v", preview)
	}
	var before []httpapi.Site
	mustDecode(t, a.do(request{method: http.MethodGet, path: sitesPath(ws), cookie: cookie}).Body, &before)
	into := "&collectionId=" + before[0].CollectionId.String()
	done := decode(a.sentinelImport(ws, "?secrets=true&onConflict=rename"+into, exportPassword, file, cookie, csrf))
	if item := done.Items[0]; item.Status != "created" || item.Name != "Imported (2)" || !item.PasswordStored || item.Path != "Work" {
		t.Errorf("import = %+v", done)
	}
	var sites []httpapi.Site
	mustDecode(t, a.do(request{method: http.MethodGet, path: sitesPath(ws), cookie: cookie}).Body, &sites)
	if len(sites) != 2 || len(sites[1].Secrets) != 1 || *sites[1].CollectionId == *before[0].CollectionId {
		t.Errorf("sites = %+v", sites)
	}

	expectProblem(t, a.sentinelImport(ws, "", "not the export password", file, cookie, csrf), http.StatusUnprocessableEntity, "wrong_export_password")
	expectProblem(t, a.sentinelImport(ws, "", exportPassword, "{}", cookie, csrf), http.StatusUnprocessableEntity, "invalid_export")
	expectProblem(t, a.sentinelImport(ws, "", exportPassword, strings.Repeat("x", 9<<20), cookie, csrf), http.StatusBadRequest, "invalid_request")
}

// unlistedSites fails to list the sites of a workspace.
type unlistedSites struct{ backup.Sites }

// List always fails.
func (unlistedSites) List(context.Context, uuid.UUID, uuid.UUID) ([]site.Site, error) {
	return nil, errors.New("database down")
}

// CanSeal says the vault is unlocked.
func (unlistedSites) CanSeal(string) bool { return true }

// brokenImporter fails every import.
type brokenImporter struct{}

// ImportEntries always fails.
func (brokenImporter) ImportEntries(context.Context, uuid.UUID, uuid.UUID, string, [][]string, []fzimport.Entry, []fzimport.Skipped, fzimport.Options) (fzimport.Result, error) {
	return fzimport.Result{}, errors.New("database down")
}

// TestExport_Faults covers exports and imports that fail for reasons of the server: they answer 500 without details.
func TestExport_Faults(t *testing.T) {
	var sealer *backup.Sealer
	a := newAPI(t, func(d *httpapi.Deps) {
		sealer = backup.NewSealer(vault.NewKDF(1), vault.Params{MemoryKiB: 64, Time: 1, Threads: 1}, rand.Reader)
		d.Backup = backup.NewService(d.Workspaces, unlistedSites{}, brokenImporter{}, sealer, testutil.NewFakeClock(time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)))
	})
	cookie, csrf := a.setup()
	ws := a.personal(cookie)
	expectProblem(t, a.exportCall(ws, exportPassword, cookie, csrf), http.StatusInternalServerError, "internal_error")
	file, err := sealer.Seal(t.Context(), exportPassword, backup.Content{Sites: []backup.Site{}})
	if err != nil {
		t.Fatal(err)
	}
	expectProblem(t, a.sentinelImport(ws, "", exportPassword, string(file), cookie, csrf), http.StatusInternalServerError, "internal_error")
}
