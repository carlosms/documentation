package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"github.com/heroku/docker-registry-client/registry"
	"gopkg.in/bblfsh/sdk.v1/manifest"
	"gopkg.in/bblfsh/sdk.v1/manifest/discovery"
)

const (
	org = discovery.GithubOrg
)

var (
	outFormat = flag.String("o", "md", "output format (md or json)")
)

func main() {
	flag.Parse()
	if err := run(os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func run(w io.Writer) error {
	ctx := context.TODO()
	langs, err := discovery.OfficialDrivers(ctx, nil)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(langs))
	for _, d := range langs {
		names = append(names, d.Language)
	}
	log.Println(len(langs), "language drivers found:", names)

	ld := newLoader()

	var (
		list = make([]Driver, len(langs))

		wg sync.WaitGroup
		// limits the number of concurrent requests
		tokens = make(chan struct{}, 3)
	)
	for i, d := range langs {
		list[i].Driver = d
		list[i].GithubURL = d.RepositoryURL()
		wg.Add(1)
		go func(d *Driver) {
			defer wg.Done()

			tokens <- struct{}{}
			defer func() {
				<-tokens
			}()

			if name := org + `/` + d.Language + `-driver`; ld.checkDockerImage(name) {
				d.DockerhubURL = `https://hub.docker.com/r/` + name + `/`
			}
		}(&list[i])
	}
	wg.Wait()

	switch *outFormat {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "\t")
		return enc.Encode(list)
	case "md":
		fallthrough
	default:
	}

	fmt.Fprint(w, header)
	defer fmt.Fprint(w, footer)

	fmt.Fprintln(w, "\n# Supported languages")
	fmt.Fprint(w, tableHeader)

	li := len(list)
	for i, m := range list {
		if m.Status.Rank() < manifest.Alpha.Rank() {
			li = i
			break
		}
		fmt.Fprint(w, m.String())
	}

	list = list[li:]
	if len(list) == 0 {
		return nil
	}

	fmt.Fprintln(w, "\n# In development")
	fmt.Fprint(w, tableHeader)

	for _, m := range list {
		fmt.Fprint(w, m.String())
	}

	return nil
}

func newLoader() *loader {
	r, err := registry.New("https://registry-1.docker.io/", "", "")
	if err != nil {
		panic(err)
	}
	return &loader{r: r}
}

type loader struct {
	r *registry.Registry
}

type Driver struct {
	discovery.Driver
	GithubURL    string `json:",omitempty"`
	DockerhubURL string `json:",omitempty"`
}

func (m Driver) Maintainer() discovery.Maintainer {
	if len(m.Maintainers) == 0 {
		return discovery.Maintainer{Name: "-"}
	}
	return m.Maintainers[0]
}

func (m Driver) String() string {
	name := m.Name
	if name == "" {
		name = m.Language
	}
	mnt := m.Maintainer()
	var mlink string
	if mnt.Github != "" {
		mnt.Name = mnt.Github
		mlink = `https://github.com/` + mnt.Github
	} else if mnt.Email != "" {
		mlink = `mailto:` + mnt.Email
	}
	return fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s | %s |\n",
		link(name, m.GithubURL), m.Language, m.Status,
		boolIcon(m.Supports(manifest.AST)),
		boolIcon(m.Supports(manifest.UAST)),
		boolIcon(m.Supports(manifest.Roles)),
		linkMark(m.DockerhubURL),
		link(mnt.Name, mlink),
	)
}

func (l *loader) checkDockerImage(name string) bool {
	// dockerhub site always returns 200, even if repository does not exists
	// so we will check image via Docker registry protocol
	m, err := l.r.Manifest(name, "latest")
	return err == nil && m != nil
}

func boolIcon(v bool) string {
	if v {
		return "✓"
	}
	return "✗"
}

func linkMark(url string) string {
	if url == "" {
		return boolIcon(false)
	}
	return link(boolIcon(true), url)
}

func link(name, url string) string {
	if url == "" {
		return name
	}
	return fmt.Sprintf(`[%s](%s)`, name, url)
}

const header = `<!-- Code generated by 'make languages' DO NOT EDIT. -->
`

const tableHeader = `
| Language   | Key        | Status  | AST\* | UAST\*\* | Annotations\*\*\* | Container | Maintainer |
| ---------- | ---------- | ------- | ---- | ------ | -------------- | --------- | ---------- |
`

const footer = `
- \* The driver is able to return the native AST
- \*\* The driver is able to return the UAST
- \*\*\* The driver is able to return the UAST annotated


**Don't see your favorite language? [Help us!](community.md)**
`
