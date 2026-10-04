// Command fakeazure serves the fake of Azure Files of the test stack (ADR-0025), which Azurite does not emulate: the share share
// of the account transferly, signed with the test key of azuretest, in memory. It is never part of a release.
//
// FAKE_AZURE_ADDR is where it listens (:10002 by default). The share starts with a folder, Reports, holding a file.
package main

import (
	"log"
	"net/http"
	"os"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/azuretest"
)

// main runs the fake until the process is stopped.
func main() {
	addr := os.Getenv("FAKE_AZURE_ADDR")
	if addr == "" {
		addr = ":10002"
	}
	fake := azuretest.NewFiles(azuretest.Options{})
	fake.PutDirectory("Reports")
	fake.Put("Reports/q3.csv", []byte("month,amount\njuly,100\n"))
	server := &http.Server{Addr: addr, Handler: fake.Handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("fake Azure Files listening on %s", addr)
	log.Fatal(server.ListenAndServe())
}
