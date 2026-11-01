# Contributing

Thanks for helping. ytwatch is small on purpose: it finds a video, plays it, and
gets out of the way. A change that adds a home page, recommendations, accounts or
comments is out of scope. For a bigger change, please open an issue first so we
can agree on it before you spend time on it.

## Build and test

You need Go 1.23+ and, to try it, the programs listed in the README.

```sh
make build     # builds ./ytwatch
make test      # go test ./...
make vet       # go vet ./...
make fmt       # gofmt
```

CI runs `gofmt`, `go vet` and `go test -race` on every push and pull request, and
a dry run of the release. Please run `make fmt vet test` before you push.

If you change `install.sh`, also run `sh scripts/test-install.sh`. It tests the
installer against a release built from your checkout and downloads nothing.

## Pull requests

- One change per pull request, with a test when it fixes a bug or adds behavior.
  Tests must not use the network or need `yt-dlp` or `mpv` installed: the code
  has hooks for faking them (see `fetchResultsPage` and the fake `yt-dlp`
  scripts in `internal/youtube`).
- Commit titles follow [Conventional Commits](https://www.conventionalcommits.org):
  `fix(tui): ...`, `feat(config): ...`, `perf(search): ...`. The release notes are
  made from them.
- Keep comments and messages in simple English.

## Bugs

Please include the output of `ytwatch --version` and `ytwatch doctor`, and say
which terminal and window manager you use. See [SECURITY.md](SECURITY.md) for
security problems.
