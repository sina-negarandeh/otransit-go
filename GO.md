# Go standards

The conventions this project follows, and the commands that enforce them.

This codebase is new, so this document states commitments rather than a record.
Nothing here waits for later. The first commit must satisfy all of it, and so
must every commit after it.

The gate needs a package to run against. A module with none is not a passing
gate, because `go vet` and `go test` both exit non-zero and report that they
matched no packages. `main.go` is the smallest honest package, and the gate has
passed since it landed.

## Contents

- [The standards](#the-standards)
- [Gate before calling anything done](#gate-before-calling-anything-done)
- [Formatting](#formatting)
- [Linting](#linting)
- [Errors](#errors)
- [API and package design](#api-and-package-design)
- [Concurrency](#concurrency)
- [Language version](#language-version)
- [Project decisions](#project-decisions)

## The standards

Consult them in this order. The first three carry most of the weight. Read only
those and you have the working set.

| Order | Standard | What it governs |
|---|---|---|
| 1 | [Google Go Style Guide](https://google.github.io/styleguide/go/) | Naming, readability, simplicity, API shape, comments. The most complete of the six, and the tie-breaker |
| 2 | [Effective Go](https://go.dev/doc/effective_go) | Core idiom. What Go code is supposed to look like |
| 3 | [Go Code Review Comments](https://go.dev/wiki/CodeReviewComments) | The rules a reviewer cites. Short, specific, and the fastest to check against |
| 4 | [Go Documentation](https://go.dev/doc/) | Official language and tooling guidance, including the spec and the module reference |
| 5 | [Go Test Comments](https://go.dev/wiki/TestComments) | Testing conventions. [TESTING.md](TESTING.md) says how they apply here |
| 6 | [Go Wiki](https://go.dev/wiki/) | Everything else the community has written down |

The Google guide has four parts and they are not equals. The **Style Guide** is
normative. **Style Decisions** explains the guidance. **Best Practices** give
patterns rather than rules. The Style Guide wins when two parts disagree.

Where a standard and a tool disagree, the tool wins. The tool is the one that
runs.

## Gate before calling anything done

Run all five, every time.

```bash
gofmt -l .                # lists no files
goimports -l .            # lists no files: gofmt's formatting, plus import groups
go vet ./...              # zero findings
golangci-lint run         # zero findings
go build ./...            # zero output
go test ./...             # all green
go test -race ./...       # all green again, with the detector
GOOS=linux go vet ./...   # and the analysis, for the machine CI runs on
```

The pass has three conditions. Every command exits zero. `gofmt -l` lists no
files. The linters report no findings.

Output on its own is not a failure. `go test` prints one line for every package
it ran, so a package with no test file prints `? pkg [no test files]`. That line
is the tool saying what it did. Do not write a test to silence it.

`gofmt -l` lists the files it would change. An empty list is the pass. It writes
nothing, so it is safe in CI. Use `gofmt -w .` to correct what it lists.

**The race detector is part of the gate.** This program polls the realtime feed
on a background goroutine. It also fetches the detours and the weather once at
launch. That is shared state and real concurrency. Go reports a data race at run
time and not at compile time, so the detector is the only thing that will tell
you. It reports only the races it observes. A concurrent path that no test
drives is a path the detector never visits. Write the test that drives it.

## Formatting

`gofmt` decides, and it takes no arguments about it. There is no configuration
file and nothing to negotiate.

Do not fight it. A line that reads badly after formatting needs a shorter name
or an extracted variable. It does not need a manual override.

`gofmt` does not decide line length. Go sets no limit. Do not wrap a line only
because it is long. Wrap it where the break makes the structure clear.

## Linting

Two commands, and they overlap on purpose.

**`go vet` must stay at zero.** It is part of the toolchain, and `go test`
already runs a subset of it. It reports defects and not preferences. A printf
verb that does not match its argument is a defect. So is a struct tag that does
not parse, a lock copied by value, and an unreachable branch. Every one of those
compiles. `golangci-lint` carries a `govet` linter of its own, built against its
own copy of the analysis passes. The toolchain ships the current one, so the
gate runs both.

**`golangci-lint run` must stay at zero, and `.golangci.yml` records the
selection.** It ships the linters as one release, so the single version in
`mise.toml` pins the version of all of them. Lints belong in one file, applied
to every package, visible to anyone who opens the repository. A lint enabled by
a comment inside one file is a lint nobody else knows about.

The selection is the default set plus one addition.

- `staticcheck` reports what `go vet` does not. That includes dead code,
  redundant conversions, and misuse of the standard library.
- `errcheck` is the only check behind the error rules below. It needs
  `check-blank` to see the `_` form those rules forbid.
- `govet`, `ineffassign` and `unused` complete the default set.
- `depguard` is the addition. It denies the `unsafe` package.

Enable a linter because it found something. Do not enable it because it exists.
Fifty linters with forty suppressions teach the reader to skip lint output. That
is the opposite of what a lint is for.

Suppress a wrong finding narrowly. Put `//nolint:<linter> // why` on the line or
the function. The reason is not optional. A bare `//nolint` is a claim with no
argument behind it.

## Errors

Go leaves error handling to the author, so these rules are explicit.

- **Do not discard an error with `_`.** A comment must say why it cannot matter.
  A write to a `strings.Builder` is one case that qualifies. `errcheck` enforces
  this rule, and it exempts those `strings.Builder` writes already.
- **Wrap with `%w` where a bare message would not say enough.** Write
  `fmt.Errorf("reading %s: %w", path, err)`. An error the user sees must name
  the file, the column, or the URL.
- **Compare with `errors.Is`. Convert with `errors.As`.** Never compare error
  strings, and never compare with `==` across a wrap.
- **An error string starts in lower case and carries no final punctuation.**
  Other errors wrap it into a longer sentence.
- **A sentinel error is part of the API.** Declare `var ErrNoKey =
  errors.New(...)` only when a caller must branch on it. Wrap in every other
  case.
- **`panic` is for a broken invariant.** A missing cache file is an input. So is
  a malformed feed, and so is an unreadable pins file. Inputs return errors.

## API and package design

- **Name for the caller.** A name is read at the call site, so `stops.Search(q)`
  is better than `stops.SearchStops(q)`. The package name is part of the name,
  and repeating it is stutter.
- **Match the length of a name to its scope.** A receiver is one or two letters.
  A loop index is `i`. A descriptive name for a variable that lives two lines is
  noise.
- **Define an interface where it is used, not beside the type that satisfies
  it.** Keep it to the methods the caller calls.
- **Take the least specific argument that works.** Prefer `io.Reader` to
  `*os.File`. Prefer `[]byte` to `*bytes.Buffer`.
- **`context.Context` is the first parameter, and its name is `ctx`.** Do not
  store it in a struct field. Anything that reaches the network takes one.
- **Keep the exported surface small.** A program needs to export almost nothing.
  `internal/` makes that structural, and the compiler enforces it.
- **Document what the signature does not show.** `// Name returns the name` is
  noise. A note that `arr` can exceed 86400 is not. Start a doc comment with the
  name of the thing it documents, because `go doc` prints it that way.
- **Make the zero value work where that is cheap.** A struct that needs four
  setters before it is useful needs a constructor instead.

## Concurrency

- **Do not start a goroutine until you know how it ends.** Every one needs a
  path to return. A closed channel, a cancelled context, or a finished loop will
  do.
- **The race detector sees only what runs.** The gate above says what follows
  from that.
- **A channel is not automatically the better answer.** Share by communicating
  where that is natural. Use a mutex where it is not. One small piece of state,
  written by one goroutine and read by another, is a mutex.
- **A mutex protects a named invariant, and a comment says which.** The fields
  below `mu` are the fields `mu` guards.

## Language version

The floor lives in the `go` directive in `go.mod`. CI holds it against a real
toolchain of that version.

Do not raise or lower the number by reading the source. Since Go 1.21 the
toolchain enforces the directive rather than reading it as a hint. A number set
by guesswork therefore fails on somebody else's machine and not on yours.

Go is not installed on the machine this document was written on, and
`go version` reports nothing there. Record the real version in `go.mod` when you
create the module. Add the CI job in the same change. The number must never be a
claim with no check behind it.

## Project decisions

Recorded so nobody argues them again.

- **`internal/` holds everything that is not `main`.** This is a program and not
  a library. Nothing outside this module should import any of it, and
  `internal/` makes the compiler say so.
- **No `pkg/`.** It adds a directory level and means nothing to the toolchain.
- **Tests live beside the code in `_test.go` files.** A test in `package foo`
  sees unexported identifiers. A test in `package foo_test` sees only the
  exported API. Use the second form when the test should be held to what a
  caller can reach.
- **`testdata/` holds checked-in inputs.** The go tool ignores a directory with
  that name, which is why it has that name.
- **No `unsafe`.** A `depguard` rule in `.golangci.yml` enforces it. A check is
  better than a sentence in a document.
- **A new dependency needs a reason in the commit message.** The standard
  library covers most of what this program does. Say what the dependency does
  that `net/http`, `database/sql`, `encoding/json` and `archive/zip` do not.
- **`go vet` checks struct tags.** A tag with a typo stops working and reports
  nothing. `go vet` is the only thing that will mention it.
