// Command fakegoogle serves the fake of Google of the test stack (ADR-0024): the OAuth endpoints, whose consent page agrees at once,
// and the subset of Drive v3 the Drive adapter uses, in memory. It is never part of a release.
//
// FAKE_GOOGLE_ADDR is where it listens (:8090 by default) and FAKE_GOOGLE_URL the origin the browser and Transferly reach it at,
// which upload sessions name. My Drive starts with a Google document, Plan, and a folder, Shared notes.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/gdrivefs/googletest"
)

// main runs the fake until the process is stopped.
func main() {
	addr := os.Getenv("FAKE_GOOGLE_ADDR")
	if addr == "" {
		addr = ":8090"
	}
	fake := googletest.New(googletest.Options{})
	fake.URL = os.Getenv("FAKE_GOOGLE_URL")
	fake.Put(fake.Root, googletest.File{Name: "Plan", MimeType: googletest.DocumentMime, Content: []byte("The plan of the quarter.")})
	fake.Put(fake.Root, googletest.File{Name: "Shared notes", MimeType: googletest.FolderMime})
	server := &http.Server{Addr: addr, Handler: fake.Handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("fake Google listening on %s as %s", addr, fake.URL)
	log.Fatal(server.ListenAndServe())
}
