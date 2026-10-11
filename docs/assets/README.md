# Brand assets

Illustrations and icons used by the README and the documentation. The
documentation site publishes this directory beside every version's pages, and
leaves this file out of them.

| File | Used for |
|---|---|
| `crewlet-icon.svg` | The Crewlet mark: the README's header and the documentation index's |
| `crewlet-mascot.svg` | The Crewlet character, on its own dark canvas: the README's Documentation section |
| `company-as-code.svg` | Feature icon: a company declared as `crewlet.ai` objects |
| `hierarchy.svg` | Feature icon: the org chart as objects |
| `applied-whole.svg` | Feature icon: one company document, applied whole or not at all |
| `secret-reference.svg` | Feature icon: credentials by reference, never by value |
| `engine-runtime.svg` | Feature icon: the engine the operator runs for each company |
| `tenants.svg` | Feature icon: one namespace per tenant |

The mark, the character, `company-as-code.svg` and `hierarchy.svg` are copies
of the [Crewlet engine's](https://github.com/crewlet/crewlet/blob/main/docs/assets/README.md),
byte for byte, which take them from the Crewlet design system: never redraw,
recolour or re-optimise them here. The other feature icons were drawn for the
operator to the same brief. The mark and the character are Crewlet trademarks,
used here because the operator is a Crewlet product; the Kubernetes logo is
not ours to combine with them.

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

The mark is traced from raster art and is **not** minified: at aggressive
settings `svgo` drops geometry from it. The character is minified, and carries
near-white details, so it embeds its own dark canvas rather than relying on
the page background.
