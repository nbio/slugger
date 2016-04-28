package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
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
	var app, user, pass, token, procFile, slugFile, release, commit string
	flag.StringVar(&app, "app", "", "Heroku app `name`")
	flag.StringVar(&user, "user", "", "Heroku `username`")
	flag.StringVar(&pass, "password", "", "Heroku password")
	flag.StringVar(&token, "token", "", "Heroku API token")
	flag.StringVar(&procFile, "procfile", "Procfile", "`path` to Procfile")
	flag.StringVar(&slugFile, "slug", "slug.tgz", "`path` to slug TAR GZIP file")
	flag.StringVar(&release, "release", "", "`id` of release to deploy directly to app")
	flag.StringVar(&commit, "commit", "", "`SHA` of commit in slug")
	dryRun := flag.Bool("n", false, "dry run; skip slug upload and release")
	verbose := flag.Bool("v", false, "dump raw requests and responses from Heroku client")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: %s [arguments]

Slugger deploys a pre-built slug file to Heroku. It will attempt to
automatically determine the correct Heroku app and authentication
information from the heroku command and current directory.

To create a slug from an app directory (./app prefix is required):

  tar czvf slug.tgz ./app

For more information on Heroku and how to create a slug, visit:
https://devcenter.heroku.com/articles/platform-api-deploying-slugs

Using the -release flag, slugger can deploy to multiple apps in the
same region with a single upload.

Available arguments:
`, os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}
	flag.Parse()
	errlog := log.New(os.Stderr, "", log.Lshortfile)
	log.SetFlags(0)
	log.SetOutput(os.Stderr)

	// Get app name
	if app == "" {
		app = os.Getenv("HEROKU_APP")
	}
	if app == "" {
		cmd := exec.Command("heroku", "info", "--shell")
		out, err := cmd.Output()
		if err != nil {
			errlog.Fatalf("Unable to determine app name: `%s': %v", strings.Join(cmd.Args, " "), err)
		}
		if matches := nameMatch.FindSubmatch(out); len(matches) > 1 {
			app = string(matches[1])
		}
	}
	if app == "" {
		flag.Usage()
	}
	log.Println("App: ", app)

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
			errlog.Fatalf("Unable to determine credentials: `%s': %v", strings.Join(cmd.Args, " "), err)
		}
		token = strings.TrimSpace(string(out))
	}
	if user == "" && pass == "" && token == "" {
		errlog.Printf("Unable to determine credentials.")
		flag.Usage()
	}

	// Initialize Heroku client
	c := heroku.Client{Username: user, Password: pass, Debug: *verbose}
	if token != "" {
		c.AdditionalHeaders = http.Header{"Authorization": {"Bearer " + token}}
	}

	// Read slug and upload if release isn't known
	if release == "" {
		// Read Procfile
		f, err := os.Open(procFile)
		if err != nil {
			errlog.Fatal(err)
		}
		procBytes, err := ioutil.ReadAll(f)
		f.Close()
		if err != nil {
			errlog.Fatal(err)
		}
		var processTypes map[string]string
		err = yaml.Unmarshal(procBytes, &processTypes)
		if err != nil {
			errlog.Fatal(err)
		}
		procText := strings.Replace(strings.TrimSpace(string(procBytes)), "\n", "\n\t", -1)
		log.Println("Processes: ", procText)

		// Open the slug file
		f, err = os.Open(slugFile)
		if err != nil {
			errlog.Fatal(err)
		}
		defer f.Close()
		log.Println("Slug file: ", slugFile)

		if commit == "" {
			out, err := exec.Command("git", "describe", "--always", "--abbrev", "--dirty").Output()
			if err == nil {
				commit = strings.TrimSpace(string(out))
			}
		}
		log.Println("Commit: ", commit)

		// Create a slug
		slug, err := c.SlugCreate(app, processTypes, &heroku.SlugCreateOpts{Commit: &commit})
		if err != nil {
			errlog.Fatal(err)
		}
		slugSize, err := f.Seek(0, os.SEEK_END)
		if err != nil {
			log.Fatal(err)
		}
		log.Println("Uploading slug: ", humanize.Bytes(uint64(slugSize)))

		// Put slug data
		if _, err := f.Seek(0, os.SEEK_SET); err != nil {
			errlog.Fatal(err)
		}
		req, err := http.NewRequest(strings.ToUpper(slug.Blob.Method), slug.Blob.URL, f)
		if err != nil {
			errlog.Fatal(err)
		}
		if *dryRun {
			log.Println("Upload skipped (dry run)")
		} else {
			if _, err := http.DefaultClient.Do(req); err != nil {
				errlog.Fatal(err)
			}
		}
		release = slug.Id
	}

	if !*dryRun {
		// Release built slug to app
		log.Println("Releasing slug: ", release)
		rel, err := c.ReleaseCreate(app, release, nil)
		if err != nil {
			errlog.Fatal(err)
		}
		log.Println("Deployed version: ", rel.Version)
	}

	fmt.Fprint(os.Stderr, "Slug ID: ")
	fmt.Println(release)
}
