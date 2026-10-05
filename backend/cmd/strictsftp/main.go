// Command strictsftp serves an SFTP server of the test stacks that reports every file larger than it is, as the enterprise SFTP
// gateways that motivated the handling of strict servers do (docs/guides/pkg-sftp.md). It is never part of a release.
//
// STRICT_SFTP_ADDR is where it listens (:22 by default), and STRICT_SFTP_OFF how many bytes it adds to each size (300 by default).
// The user transferly signs in with the password transferly and finds export.dat, a file of 1.1 MB, bundle.zip, a ZIP of 1 MB with
// two entries, and notes.txt, a small file.
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"log"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/fprambs/transferly/internal/adapters/remotefs/sftpfs/sftptest"
)

// main runs the server until the process is stopped.
func main() {
	addr := os.Getenv("STRICT_SFTP_ADDR")
	if addr == "" {
		addr = ":22"
	}
	off, err := strconv.ParseInt(os.Getenv("STRICT_SFTP_OFF"), 10, 64)
	if err != nil {
		off = 300
	}
	root, err := os.MkdirTemp("", "strictsftp")
	if err != nil {
		log.Fatal(err)
	}
	if err := seed(root); err != nil {
		log.Fatal(err)
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", addr)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := sftptest.Serve(listener, root, sftptest.Options{Users: map[string]string{"transferly": "transferly"}, SizeOff: off}); err != nil {
		log.Fatal(err)
	}
	log.Printf("strict SFTP server listening on %s, reporting sizes %d bytes off", addr, off)
	select {}
}

// seed writes the files of the server: a file of 1.1 MB, a ZIP of 1 MB and a small text, with content that is the same on every start.
func seed(root string) error {
	random := rand.New(rand.NewPCG(1, 2))
	payload := make([]byte, 1_100_000)
	for i := range payload {
		payload[i] = byte(random.UintN(256))
	}
	var archive bytes.Buffer
	w := zip.NewWriter(&archive)
	for _, name := range []string{"data.bin", "readme.txt"} {
		entry, err := w.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store, Modified: time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)})
		if err != nil {
			return err
		}
		content := payload[:1_000_000]
		if name == "readme.txt" {
			content = []byte("A ZIP on a server that reports sizes it does not send.\n")
		}
		if _, err := entry.Write(content); err != nil {
			return err
		}
	}
	if err := w.Close(); err != nil {
		return err
	}
	files := map[string][]byte{"export.dat": payload, "bundle.zip": archive.Bytes(), "notes.txt": []byte("Sizes here are reported 300 bytes larger.\n")}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			return err
		}
	}
	return nil
}
