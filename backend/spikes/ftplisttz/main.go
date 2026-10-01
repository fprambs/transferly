// Command ftplisttz checks how to interpret FTP LIST timestamps that carry no time zone.
//
// It compares two ways to detect the server's local offset: against the MDTM time of the same
// file, and against the client clock at upload time. It then verifies that parsing the listing in
// the detected zone yields the true instant.
package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

const probe = "upload/tz-probe.txt"

// dial opens a plain FTP session whose listings are parsed in loc.
func dial(loc *time.Location) (*ftp.ServerConn, error) {
	conn, err := ftp.Dial("ftp:21", ftp.DialWithLocation(loc), ftp.DialWithTimeout(10*time.Second))
	if err != nil {
		return nil, err
	}
	if err := conn.Login("transferly", "transferly"); err != nil {
		_ = conn.Quit()
		return nil, err
	}
	return conn, nil
}

// listedTime returns the modification time LIST reports for the probe file.
func listedTime(conn *ftp.ServerConn) (time.Time, error) {
	entries, err := conn.List("upload")
	if err != nil {
		return time.Time{}, err
	}
	for _, e := range entries {
		if e.Name == "tz-probe.txt" {
			return e.Time, nil
		}
	}
	return time.Time{}, fmt.Errorf("probe file not listed")
}

// main uploads a probe, detects the offset and validates the fix.
func main() {
	conn, err := dial(time.UTC)
	if err != nil {
		fmt.Println("dial:", err)
		return
	}
	defer func() { _ = conn.Quit() }()
	uploaded := time.Now().UTC()
	if err := conn.Stor(probe, strings.NewReader("x")); err != nil {
		fmt.Println("upload:", err)
		return
	}
	defer func() { _ = conn.Delete(probe) }()

	truth, err := conn.GetTime(probe)
	if err != nil {
		fmt.Println("MDTM:", err)
		return
	}
	listed, err := listedTime(conn)
	if err != nil {
		fmt.Println("LIST:", err)
		return
	}
	viaMDTM := listed.Sub(truth).Round(15 * time.Minute)
	viaClock := listed.Sub(uploaded).Round(15 * time.Minute)
	fmt.Printf("client clock at upload (UTC): %s\n", uploaded.Format(time.RFC3339))
	fmt.Printf("MDTM answer:                  %s\n", truth.UTC().Format(time.RFC3339))
	fmt.Printf("LIST parsed as UTC:           %s\n", listed.UTC().Format(time.RFC3339))
	fmt.Printf("offset detected via MDTM:     %s\n", viaMDTM)
	fmt.Printf("offset detected via clock:    %s\n", viaClock)
	fmt.Printf("time precise in LIST:         %v\n", conn.IsTimePreciseInList())

	zone, err := time.LoadLocation("America/Santiago")
	if err != nil {
		fmt.Println("zone:", err)
		return
	}
	local, err := dial(zone)
	if err != nil {
		fmt.Println("dial with zone:", err)
		return
	}
	defer func() { _ = local.Quit() }()
	fixed, err := listedTime(local)
	if err != nil {
		fmt.Println("LIST with zone:", err)
		return
	}
	fmt.Printf("LIST parsed in %s: %s (drift from the client clock: %s)\n",
		zone, fixed.UTC().Format(time.RFC3339), fixed.Sub(uploaded).Abs().Round(time.Second))
}
