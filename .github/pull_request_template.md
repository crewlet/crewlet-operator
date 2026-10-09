## What & why

<!-- What does this pull request change, and what problem does it solve? -->

## Checklist

- [ ] `make check` passes — gofmt, `go mod tidy -diff` and `go vet` in both
      modules, generated files in step with their sources, the
      golangci-lint configuration and golangci-lint, the Helm chart's lint,
      the build, the unit, envtest, engine and chart suites under `-race`,
      and the DCO sign-off
- [ ] Tests added or updated; an API change comes with an envtest case that
      the API server enforces it (a CEL rule refuses what it must refuse)
- [ ] Generated files regenerated and committed:
      `make manifests generate chart api-docs`
- [ ] Docs updated (`docs/`) for any change to the API, behaviour, the
      deployment or a make target
- [ ] Credentials stay references to Secrets; no field accepts a secret
      value, and no value reaches a pod template, a status, an event or a log
- [ ] A dependency, tool, image or action that was added or bumped is on its
      newest release and pinned exactly, or carries the reason it is held
      back at the pin
- [ ] Every commit is signed off — `git commit -s`; CI's `sign-off` job fails
      without it
- [ ] Commit subjects are `type(scope): summary`
- [ ] The pull request title takes the same form and reads as a release note:
      it is what appears in the generated release notes
