package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/cyberdelia/heroku-go"
	"github.com/dustin/go-humanize"
	"gopkg.in/yaml.v2"
)

var nameMatch = regexp.MustCompile(`\bname=([^\n]+)`)

func main() {
	var app, user, pass, token, stack, procFile, slugFile, release, commit, langDesc string
	flag.StringVar(&app, "app", "", "Heroku app `name`")
	flag.StringVar(&user, "user", "", "Heroku `username`")
	flag.StringVar(&pass, "password", "", "Heroku password")
	flag.StringVar(&token, "token", "", "Heroku API token")
	flag.StringVar(&stack, "stack", "", "Heroku stack (e.g. cedar-14 or heroku-16)")
	flag.StringVar(&procFile, "procfile", "Procfile", "`path` to Procfile")
	flag.StringVar(&slugFile, "slug", "slug.tgz", "`path` to slug TAR GZIP file")
	flag.StringVar(&release, "release", "", "`slug_id` to release directly to app")
	flag.StringVar(&commit, "commit", "", "provide `SHA` of commit in slug")
	flag.StringVar(&langDesc, "lang-desc", "", "the language description of this slug")
	noRelease := flag.Bool("no-release", false, "only upload slug, do not release")
	dryRun := flag.Bool("n", false, "dry run; skip slug upload and release")
	verbose := flag.Bool("v", false, "dump raw requests and responses from Heroku client")
	info := flag.Bool("info", false, "show remote information about uploaded slug")
	commitOnly := flag.Bool("commit-only", false, "show only commit SHA with -info")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `Usage: %s [arguments]

Slugger deploys a pre-built slug file to Heroku. It will attempt to
automatically determine the correct Heroku app and authentication
information from the heroku command and current directory.

To create a slug from an app directory (./app prefix is required):

  tar czvf slug.tgz ./app

For more information on Heroku and how to create a slug, visit:
https://devcenter.heroku.com/articles/platform-api-deploying-slugs

Using the -no-release flag, slugger can prepare a slug for deploy
without releasing it to an app. Running slugger again with the
-release flag, you can deploy the slug, by ID, to multiple apps.

With the -lang-desc flag, please try to match the output of
bin/detect for the buildpack you use. You can find this out by
opening the source for the relevant buildpack and looking at
bin/detect. For Go you will want to set "Go", etc.

Available arguments:
`, os.Args[0])
		flag.PrintDefaults()
		os.Exit(1)
	}
	flag.Parse()
	errlog := log.New(os.Stderr, "", log.Lshortfile)
	log.SetFlags(0)
	log.SetOutput(os.Stderr)

	if *info && release == "" {
		errlog.Fatal("use of -info requires use of -release")
	}

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
		errlog.Fatalf("Unable to determine app name from command line: %s", strings.Join(os.Args, " "))
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

	// Initialize Heroku service
	transport := &heroku.Transport{
		Username: user,
		Password: pass,
	}
	if token != "" {
		transport.AdditionalHeaders = http.Header{"Authorization": {"Bearer " + token}}
	}
	svc := heroku.NewService(&http.Client{Transport: transport})

	// Read slug and upload if release isn't known
	if release == "" {
		// Read Procfile
		f, err := os.Open(procFile)
		if err != nil {
			errlog.Fatal(err)
		}
		procBytes, err := ioutil.ReadAll(io.LimitReader(f, 1<<20)) // Limit ReadAll to 1MB
		if err != nil {
			errlog.Fatal(err)
		}
		f.Close() // OK to ignore error from read-only file usage
		var processTypes map[string]string
		err = yaml.Unmarshal(procBytes, &processTypes)
		if err != nil {
			errlog.Fatal(err)
		}
		procText := strings.Replace(strings.TrimSpace(string(procBytes)), "\n", "\n\t", -1)
		log.Println("Processes: ", procText)

		// Open the slug file for reading
		f, err = os.Open(slugFile)
		if err != nil {
			errlog.Fatal(err)
		}
		defer f.Close() // OK to ignore error from read-only file usage
		log.Println("Slug file: ", slugFile)

		if commit == "" {
			out, err := exec.Command("git", "describe", "--always", "--abbrev", "--dirty").Output()
			if err == nil {
				commit = strings.TrimSpace(string(out))
			}
		}
		log.Println("Commit: ", commit)

		if langDesc != "" {
			log.Println("Language Description: ", langDesc)
		}

		var stackp *string
		if stack != "" {
			stackp = &stack
			log.Println("Stack:", stack)
		}

		// Create a slug at Heroku
		slug, err := svc.SlugCreate(context.TODO(), app, heroku.SlugCreateOpts{
			Stack:        stackp, // For JSON omitempty
			ProcessTypes: processTypes,
			Commit:       &commit,
			BuildpackProvidedDescription: &langDesc,
		})
		if err != nil {
			errlog.Fatalf("slug: %s", err)
		}
		stat, err := f.Stat()
		if err != nil {
			errlog.Fatal(err)
		}
		log.Println("Uploading slug: ", humanize.Bytes(uint64(stat.Size())))

		// Put slug data
		req, err := http.NewRequest(http.MethodPut, slug.Blob.URL, f)
		if err != nil {
			errlog.Fatal(err)
		}
		if *dryRun {
			log.Println("Upload skipped (dry run)")
		} else {
			req.Header.Set("Content-Type", "")
			req.ContentLength = stat.Size()
			if *verbose {
				dump, err := httputil.DumpRequestOut(req, false) // don't dump large body
				if err != nil {
					errlog.Fatalf("debug: %s", err)
				} else {
					os.Stderr.Write(dump)
					os.Stderr.Write([]byte{'\n', '\n'})
				}
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errlog.Fatalf("upload: %s", err)
			}
			if resp.StatusCode > 201 {
				errlog.Fatalf("upload: %s", resp.Status)
			}
			resp.Body.Close()
		}
		release = slug.ID
	}

	if *info {
		slug, err := svc.SlugInfo(context.TODO(), app, release)
		if err != nil {
			errlog.Fatalf("slug[%s]: %s", release, err)
		}
		b, err := json.MarshalIndent(slug, "", "  ")
		if err != nil {
			errlog.Fatalf("JSON from slug(%q): %s", slug.ID, err)
		}
		if *commitOnly {
			fmt.Println(*slug.Commit)
			return
		}
		os.Stdout.Write(b)
		return

	} else if !(*dryRun || *noRelease) {
		// Release built slug to app
		log.Println("Releasing slug: ", release)
		rel, err := svc.ReleaseCreate(context.TODO(), app, heroku.ReleaseCreateOpts{
			Slug: release,
		})
		if err != nil {
			errlog.Fatalf("release: %s", err)
		}
		log.Println("Deployed version: ", rel.Version)
	}

	fmt.Fprint(os.Stderr, "Slug ID: ")
	fmt.Println(release)
}
