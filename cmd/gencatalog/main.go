// Command gencatalog regenerates internal/catalog/systemd.json.gz, the
// directive catalog embedded in sysdtui, from a directory of systemd's
// section 5 man pages:
//
//	go run ./cmd/gencatalog -man /usr/share/man/man5 -version 255
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/lonedevel/sysdtui/internal/catalog"
)

func main() {
	dir := flag.String("man", "/usr/share/man/man5", "directory holding systemd.*.5[.gz]")
	version := flag.String("version", "", "systemd version the pages document")
	out := flag.String("o", "internal/catalog/systemd.json.gz", "output file")
	flag.Parse()

	c, err := catalog.FromDir(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gencatalog:", err)
		os.Exit(1)
	}
	c.Source = "embedded documentation from systemd"
	if *version != "" {
		c.Source += " " + *version
		c.Version = *version
	}
	f, err := os.Create(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gencatalog:", err)
		os.Exit(1)
	}
	if err := c.Marshal(f); err != nil {
		fmt.Fprintln(os.Stderr, "gencatalog:", err)
		os.Exit(1)
	}
	if err := f.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "gencatalog:", err)
		os.Exit(1)
	}
	fmt.Printf("%d directives from %s\n", c.Len(), *dir)
}
