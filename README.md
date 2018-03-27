# slugger

Build and deploy [Heroku](https://heroku.com) slugs on your local machine. Handy for deploying single-binary [Go](https://golang.org) web applications.

## Usage

```shell
mkdir -p ./app/bin
GOARCH=amd64 GOOS=linux go build -o ./app/bin/web main.go
tar czfv slug.tgz ./app
slugger
```

Slugger will parse the [Procfile](https://devcenter.heroku.com/articles/procfile) to determine process types. This example assumes a Procfile containing a single web process that accepts a `-listen` argument, e.g.:

```yaml
web: PATH=$PATH:$HOME/bin web -listen=:$PORT
```

The Heroku Dev Center has more information on [building slugs from scratch](https://devcenter.heroku.com/articles/platform-api-deploying-slugs).

## Installation

Make sure you have a working Go installation (tested on 1.4+) and run:

```shell
go install github.com/nbio/slugger
```

## SEO

golang

## About

Slugger uses the fantastic [heroku-go/v3](https://github.com/cyberdelia/heroku-go) API client by [Timothée Peignier](https://github.com/cyberdelia). Extracted from and used in production at [domainr.com](https://domainr.com).

© 2015–2018 nb.io, LLC
