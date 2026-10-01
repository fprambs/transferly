// Command streamupload checks that a large upload streamed through an HTTP handler into SFTP
// keeps the server memory bounded, without touching the disk.
//
// It starts the handler, uploads zeros through it and reports the peak resident memory.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// zeros is an endless reader of zero bytes.
type zeros struct{}

// Read fills p with zeros.
func (zeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// peakResidentKiB returns the peak resident set size of the process from /proc.
func peakResidentKiB() int {
	file, err := os.Open("/proc/self/status")
	if err != nil {
		return -1
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var kib int
		if _, err := fmt.Sscanf(scanner.Text(), "VmHWM: %d kB", &kib); err == nil {
			return kib
		}
	}
	return -1
}

// handler streams the request body into a new SFTP file.
func handler(sftpAddr string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		config := &ssh.ClientConfig{
			User:            "transferly",
			Auth:            []ssh.AuthMethod{ssh.Password("transferly")},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         10 * time.Second,
		}
		conn, err := ssh.Dial("tcp", sftpAddr, config)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = conn.Close() }()
		client, err := sftp.NewClient(conn, sftp.UseConcurrentWrites(true))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = client.Close() }()

		file, err := client.Create("upload/big.bin")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		written, err := file.ReadFrom(r.Body)
		closeErr := file.Close()
		_ = client.Remove("upload/big.bin")
		if err != nil || closeErr != nil {
			http.Error(w, fmt.Sprint(err, closeErr), http.StatusBadGateway)
			return
		}
		_, _ = fmt.Fprintf(w, "%d", written)
	})
}

// main uploads the requested amount and prints throughput and peak memory.
func main() {
	sizeMiB := flag.Int64("size", 5120, "upload size in MiB")
	sftpAddr := flag.String("sftp", "sftp:22", "SFTP server address")
	flag.Parse()

	ctx := context.Background()
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Println("listen:", err)
		return
	}
	go func() { _ = http.Serve(listener, handler(*sftpAddr)) }()

	size := *sizeMiB << 20
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, "http://"+listener.Addr().String()+"/upload", io.LimitReader(zeros{}, size))
	if err != nil {
		fmt.Println("request:", err)
		return
	}
	request.ContentLength = size
	before := peakResidentKiB()
	start := time.Now()
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		fmt.Println("upload:", err)
		return
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(response.Body)
	elapsed := time.Since(start)

	fmt.Printf("status: %s, bytes stored: %s\n", response.Status, strings.TrimSpace(string(body)))
	fmt.Printf("uploaded %d MiB in %s (%.1f MB/s)\n", *sizeMiB, elapsed.Round(time.Millisecond), float64(*sizeMiB)/elapsed.Seconds())
	fmt.Printf("peak resident memory: %d MiB (before the upload: %d MiB)\n", peakResidentKiB()/1024, before/1024)
}
