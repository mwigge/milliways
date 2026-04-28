# Contributing to milliways

## Getting started

```bash
git clone https://github.com/mwigge/milliways.git
cd milliways
go build ./...
go test ./...
```

Requires Go 1.22+.

## Submitting changes

- One branch per logical unit of work
- Open a pull request against `master`
- All CI checks must pass before merge
- Keep commits focused — one logical change per commit

## Commit format

```
type(scope): short description
```

Types: `feat` `fix` `refactor` `test` `docs` `chore`

## Code style

- `go fmt` and `go vet` before committing
- No hardcoded secrets — use environment variables
- Structured logging only — no `fmt.Print` in library code

## Reporting bugs

Open an issue at https://github.com/mwigge/milliways/issues with:
- milliways version (`milliways --version`)
- OS and Go version
- Steps to reproduce
- What you expected vs. what happened

## License

By contributing you agree your contributions will be licensed under the
Apache License 2.0 (see `LICENSE`).
