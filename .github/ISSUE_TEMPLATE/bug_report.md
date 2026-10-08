---
name: Bug report
about: Something is broken
labels: bug
---

## What happened

<!-- What did you do, what did you expect, and what happened instead? -->

## Environment

- crewlet-operator version (image tag or commit):
- How it was installed (Helm chart, install.yaml, `make deploy`):
- Kubernetes version and distribution (`kubectl version`):
- Crewlet engine image, if the problem involves an engine:

## Resources

<!-- The crewlet.ai objects involved (`kubectl get <kind> <name> -o yaml`),
     including their status and conditions. Redact anything identifying;
     they should contain no secret values, since every credential is a
     reference to a Secret. -->

## Logs and events

<!-- The manager's logs (`kubectl -n crewlet-operator-system logs
     deploy/crewlet-operator-controller-manager`) and the events on the
     objects concerned. -->
