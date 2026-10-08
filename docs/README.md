# crewlet-operator documentation

The operator is in alpha and its `crewlet.ai/v1alpha1` API is still changing,
so these pages describe what exists today and grow with it.

| Page | What it covers |
|---|---|
| [Getting started](getting-started.md) | Running the operator on a local kind cluster, from source or as an image, applying the example company, and removing it all again |
| [How the operator models a company](concepts/overview.md) | The kinds and how they relate, the identities that never change, credentials, management policies and status |
| [Credentials](concepts/credentials.md) | Every position in a Crewlet company that holds a credential, and the field that fills it |
| [API reference](reference/api.md) | Every kind and field of `crewlet.ai/v1alpha1`, generated from the types with `make api-docs` |

For working on the operator itself, see [CONTRIBUTING.md](../CONTRIBUTING.md);
for publishing it, [RELEASING.md](../RELEASING.md).
