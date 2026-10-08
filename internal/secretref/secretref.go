// Package secretref names one key of one Kubernetes Secret that a custom
// resource references.
//
// It exists so the packages that meet a reference at different points — the
// renderer that writes it into the company document as a ${NAME} pointer, the
// secret sync that copies its value into the engine's encrypted store, and the
// controllers that watch the Secret for rotation — share one identity for it.
// Each of them would otherwise carry its own triple of strings, and the three
// would have to agree on what makes two references the same reference.
package secretref

import "cmp"

// Key identifies one key of one Secret.
//
// Namespace is always set: a namespaced custom resource references Secrets in
// its own namespace, and an EngineClass, which is cluster-scoped, names the
// namespace of each Secret it references. Two references to the same key of
// the same Secret are the same Key, whichever objects made them.
type Key struct {
	Namespace string
	Name      string
	Key       string
}

// String renders the key for logs and condition messages. It never carries
// the Secret's value.
func (k Key) String() string {
	return k.Namespace + "/" + k.Name + "[" + k.Key + "]"
}

// Compare orders keys by namespace, then Secret name, then key, so anything
// rendered from a set of keys is deterministic.
func Compare(a, b Key) int {
	return cmp.Or(
		cmp.Compare(a.Namespace, b.Namespace),
		cmp.Compare(a.Name, b.Name),
		cmp.Compare(a.Key, b.Key),
	)
}
