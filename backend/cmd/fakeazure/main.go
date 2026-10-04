// Command fakeazure serves the fake of Azure Files of the test stacks (ADR-0025), which Azurite does not emulate: the share share
// of the account transferly, signed with the test key of azuretest, in memory. It is never part of a release.
//
// FAKE_AZURE_ADDR is where it listens (:10002 by default). The share starts with a folder, Reports, holding a file. When
// FAKE_AZURE_SAMPLES names a folder, its files are copied into the share under preview-samples. When FAKE_AZURE_BLOB names the
// Blob endpoint of an Azurite, such as http://azurite:10000, it creates the container transferly of the account devstoreaccount1
// there and copies the same samples into it, so the development test environment has data in both services.
package main

import (
	"context"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/azure"
	"github.com/fprambs/transferly/internal/adapters/remotefs/azuretest"
)

// Azurite's well-known development account, which every Azurite accepts.
const (
	azuriteAccount = "devstoreaccount1"
	azuriteKey     = "Eby8vdM02xNOcqFlqUwJPLlmEtlCDXJ1OUzFT50uSRZ6IFsuFq2UVErCz4I6tq/K1SZFPTOtr/KBHBeksoGMGw=="
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
	samples := samplesOf(os.Getenv("FAKE_AZURE_SAMPLES"))
	if len(samples) > 0 {
		fake.PutDirectory("preview-samples")
	}
	for _, s := range samples {
		for dir := path.Dir(s.name); dir != "."; dir = path.Dir(dir) {
			fake.PutDirectory("preview-samples/" + dir)
		}
		fake.Put("preview-samples/"+s.name, s.data)
	}
	if blob := os.Getenv("FAKE_AZURE_BLOB"); blob != "" {
		go seedAzurite(blob, samples)
	}
	server := &http.Server{Addr: addr, Handler: fake.Handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Printf("fake Azure Files listening on %s with %d samples", addr, len(samples))
	log.Fatal(server.ListenAndServe())
}

// sample is a file to seed, by its slash-separated name below the samples folder.
type sample struct {
	name string
	data []byte
}

// samplesOf reads every file below root, or nothing when root is empty or cannot be read.
func samplesOf(root string) []sample {
	if root == "" {
		return nil
	}
	var out []sample
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out = append(out, sample{name: filepath.ToSlash(rel), data: data})
		return nil
	})
	if err != nil {
		log.Printf("reading the samples in %s: %v", root, err)
	}
	return out
}

// seedAzurite creates the container transferly in the Azurite at endpoint, retrying while Azurite starts, and copies the samples
// into it under preview-samples.
func seedAzurite(endpoint string, samples []sample) {
	client, err := azure.New(azure.Config{Endpoint: endpoint + "/" + azuriteAccount, Account: azuriteAccount, Key: azuriteKey, Client: http.DefaultClient})
	if err != nil {
		log.Printf("seeding Azurite: %v", err)
		return
	}
	ctx := context.Background()
	for attempt := 0; ; attempt++ {
		err = send(ctx, client, "transferly", map[string][]string{"restype": {"container"}}, nil, nil)
		if err == nil || azure.Code(err) == "ContainerAlreadyExists" {
			break
		}
		if attempt == 60 {
			log.Printf("seeding Azurite: %v", err)
			return
		}
		time.Sleep(time.Second)
	}
	blob := http.Header{"X-Ms-Blob-Type": {"BlockBlob"}}
	for _, s := range samples {
		if err := send(ctx, client, "transferly/preview-samples/"+s.name, nil, blob, s.data); err != nil {
			log.Printf("seeding Azurite with %s: %v", s.name, err)
		}
	}
	log.Printf("seeded the container transferly of Azurite with %d samples", len(samples))
}

// send makes a PUT request to Azurite and closes its answer.
func send(ctx context.Context, client *azure.Client, resource string, query map[string][]string, header http.Header, body []byte) error {
	resp, err := client.Do(ctx, http.MethodPut, resource, query, header, body)
	if resp != nil {
		_ = resp.Body.Close()
	}
	return err
}
