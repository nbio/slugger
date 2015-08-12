package main

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/bgentry/heroku-go"
	"github.com/dustin/go-humanize"
	"gopkg.in/yaml.v2"
)

var nameMatch = regexp.MustCompile(`\bname=([^\n]+)`)

func main() {
	var app, user, pass, token, procFile, slugFile string
	flag.StringVar(&app, "app", "", "Heroku app name")
	flag.StringVar(&user, "user", "", "Heroku username")
	flag.StringVar(&pass, "password", "", "Heroku password")
	flag.StringVar(&token, "token", "", "Heroku API token")
	flag.StringVar(&procFile, "procfile", "Procfile", "path to Procfile")
	flag.StringVar(&slugFile, "slug", "slug.tgz", "path to slug file")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: %s [arguments]

Slugger deploys a pre-built slug file to Heroku. It will attempt to
automatically determine the correct Heroku app and authentication
information from the heroku command and current directory.

To create a slug from an app directory (./app prefix is required):

  tar czvf slug.tgz ./app

For more information on Heroku and how to create a slug, visit:
https://devcenter.heroku.com/articles/platform-api-deploying-slugs

Available arguments:
`, os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}
	flag.Parse()

	// Get app name
	if app == "" {
		app = os.Getenv("HEROKU_APP")
	}
	if app == "" {
		cmd := exec.Command("heroku", "info", "--shell")
		out, err := cmd.Output()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to determine app name: `%s': %v\n\n", strings.Join(cmd.Args, " "), err)
			os.Exit(2)
		}
		if matches := nameMatch.FindSubmatch(out); len(matches) > 1 {
			app = string(matches[1])
		}
	}
	if app == "" {
		flag.Usage()
	}

	// Get auth details
	if user == "" {
		user = os.Getenv("HEROKU_USER")
	}
	if pass == "" {
		pass = os.Getenv("HEROKU_PASSWORD")
	}
	if token == "" {
		token = os.Getenv("HEROKU_TOKEN")
	}
	if token == "" {
		cmd := exec.Command("heroku", "auth:token")
		out, err := cmd.Output()
		if err != nil && user == "" && pass == "" {
			fmt.Fprintf(os.Stderr, "Unable to determine credentials: `%s': %v\n\n", strings.Join(cmd.Args, " "), err)
			os.Exit(2)
		}
		token = strings.TrimSpace(string(out))
	}
	if user == "" && pass == "" && token == "" {
		fmt.Fprintf(os.Stderr, "Unable to determine credentials.\n\n")
		flag.Usage()
	}

	// Read Procfile
	f, err := os.Open(procFile)
	exitif(err)
	procBytes, err := ioutil.ReadAll(f)
	f.Close()
	exitif(err)
	var processTypes map[string]string
	err = yaml.Unmarshal(procBytes, &processTypes)
	exitif(err)
	procText := strings.Replace(strings.TrimSpace(string(procBytes)), "\n", "\n\t", -1)

	// Read the slug
	f, err = os.Open(slugFile)
	exitif(err)
	slugBytes, err := ioutil.ReadAll(f)
	f.Close()
	exitif(err)

	// Get commit ID
	commit := ""
	out, err := exec.Command("git", "describe", "--always", "--abbrev", "--dirty").Output()
	if err == nil {
		commit = strings.TrimSpace(string(out))
	}
	opts := heroku.SlugCreateOpts{Commit: &commit}

	// Log some stuff
	fmt.Printf("App: %s\n", app)
	fmt.Printf("Commit: %s\n", commit)
	fmt.Printf("Processes: %s\n", procText)
	fmt.Printf("Slug file: %s\n", slugFile)

	// Initialize Heroku client
	c := heroku.Client{Username: user, Password: pass}
	if token != "" {
		c.AdditionalHeaders = http.Header{"Authorization": {"Bearer " + token}}
	}

	// Create a slug
	slug, err := c.SlugCreate(app, processTypes, &opts)
	exitif(err)
	fmt.Printf("Slug ID: %s\n", slug.Id)
	fmt.Printf("Uploading slug: %s\n", humanize.Bytes(uint64(len(slugBytes))))

	// Put slug data
	meth := strings.ToUpper(slug.Blob.Method)
	req, err := http.NewRequest(meth, slug.Blob.URL, bytes.NewReader(slugBytes))
	exitif(err)
	_, err = http.DefaultClient.Do(req)
	exitif(err)

	// Release
	rel, err := c.ReleaseCreate(app, slug.Id, nil)
	exitif(err)
	fmt.Printf("Deployed version: %d\n", rel.Version)
}

func exitif(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Fatal: %s\n", err)
		os.Exit(1)
	}
}
