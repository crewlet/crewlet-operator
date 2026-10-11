# Brand assets

Illustrations and icons used by the README and the documentation. The
documentation site publishes this directory beside every version's pages, and
leaves this file out of them.

| File | Used for |
|---|---|
| `crewlet-operator-mascot.svg` | The operator's mascot, on its own dark canvas: the README's header and Documentation section, and the documentation index's header |
| `company-as-code.svg` | Feature icon: a company declared as `crewlet.ai` objects |
| `hierarchy.svg` | Feature icon: the org chart as objects |
| `applied-whole.svg` | Feature icon: one company document, applied whole or not at all |
| `secret-reference.svg` | Feature icon: credentials by reference, never by value |
| `engine-runtime.svg` | Feature icon: the engine the operator runs for each company |
| `tenants.svg` | Feature icon: one namespace per tenant |

`company-as-code.svg` and `hierarchy.svg` are copies of the
[Crewlet engine's](https://github.com/crewlet/crewlet/blob/main/docs/assets/README.md),
byte for byte, which take them from the Crewlet design system: never redraw,
recolour or re-optimise them here. The other feature icons were drawn for the
operator to the same brief.

The mascot is the Crewlet character of the engine's `crewlet-mascot.svg`, its
newspaper swapped for a mug, taking a break on a stack of shipping containers
with a ship's wheel propped beside it: the containers are what Kubernetes runs
and the helm is what its name means. The character is a Crewlet trademark,
used here because the operator is a Crewlet product. The Kubernetes logo is not
ours to combine with it, so the scene does not borrow it: the wheel has eight
spokes, not seven, and stands in the scene as a prop rather than as an emblem.

## Conventions

Feature icons are **80×80** (`viewBox="0 0 80 80"`, no width or height), drawn
on a `#111` disc so they read on both light and dark backgrounds, in one
diagonal brand gradient per file, two of `#6a5cff`, `#a055ff` and `#e24a90`,
with a gradient id of the file's own. Strokes are 3 for a hero shape, 2 for
primary outlines and 1.5 for secondary lines and connectors, with round caps
and joins; depth comes from opacity tiers (1, then 0.9 down to 0.4), and a
boundary is dashed (`stroke-dasharray="4 3"`) at 0.4 to 0.5. Content stays
within about 14 to 66 on each axis. A `#111` shape over the gradient is a
cut-out.

Gradients must use `gradientUnits="userSpaceOnUse"` spanning the 80×80 canvas.
With the default `objectBoundingBox` units, any horizontal or vertical stroke
has a zero-area bounding box, which makes the gradient degenerate: the shape
then does not paint at all.

The mascot is drawn in the character's own style: flat purples with black
outlines, and white props drawn in navy line work, with no gradient. It is
minified, and carries near-white details, so it embeds its own dark canvas
(`#0d0d12`, rounded) rather than relying on the page background.
