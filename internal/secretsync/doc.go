// Package secretsync copies the values of referenced Kubernetes Secret keys
// into a Crewlet engine's encrypted secret store, and removes the entries
// nothing references any more, and those of a Secret that is gone where every
// revision an engine node runs or starts on goes without its value.
//
// The company document never holds a credential. Every credential position
// in it is a ${NAME} reference, NAME being StoreName of the referenced
// Secret key, and the engine resolves ${NAME} from its secret store before it
// looks at its own environment. Writing the value into the store under that
// name is this package's job, and is what makes rewriting a Secret rotate the
// credential in a running engine without a restart.
//
// A reconcile uses it in these steps:
//
//   - Resolve reads the referenced keys, with an uncached reader, into a
//     Desired set of store entries. A Secret or key that does not exist is a
//     *MissingError naming the reference, and the set it returns with one is
//     partial, which Sync refuses, unless every reference to it is one the
//     document lets go without its value (a tool server's or an executor run's
//     key listed in optionalKeys): there it is a value that is not there
//     (Desired.Missing), as a missing optional secretKeyRef is to
//     Kubernetes.
//   - LoadState reads what the operator last pushed, Sync makes the store
//     match the Desired set, and SaveState records what Sync achieved.
//   - A name the running document still references but the next one does
//     not is passed to Desired.Keep until the next document is applied, so
//     the engine never resolves a reference to a deleted entry.
//   - So is every name of each other revision an engine node runs or starts
//     on (Desired.KeepServed). A node applies the fleet's revision on its
//     own loop, and one that refuses it goes on running the revision it ran
//     before, which needs its values for as long as the node runs it: every
//     attempt to apply a revision reads the store again into the snapshot
//     the running revision resolves its references from. A node whose store
//     outlives a restart, as a data pod's does on its volume, starts on the
//     revision that store names, which the engine reports apart from the one
//     it runs (engine.ApplyRecord.Boots), since it moves it only after an
//     apply has succeeded; the node cannot start once a value that revision
//     requires is gone. A seat pod's store is scratch, which the engine
//     discards at every start, so it starts on nothing and applies the
//     fleet's revision; and a node that runs no company and starts on none
//     needs nothing kept beyond what the fleet's revision references. What
//     is kept for such a revision alone is reported (Result.Held), and goes
//     once no node runs or starts on it. A node whose revisions the caller
//     does not know, or could not read, keeps everything
//     (Desired.KeepUnread), and the caller knows them only from the fleet:
//     a node applies the fleet's revision on its own loop, and a data pod
//     moves what its store starts it on after every apply that succeeds, so
//     nothing seen of a node before stands for what it runs or starts on
//     now, and the State remembers none of it. So every data pod the fleet
//     does not list keeps everything until it is listed again, running or
//     down, its store starting it on whichever revision it applied last,
//     which holds every deletion back while a data pod is down; and a seat
//     pod the fleet does not list keeps everything while its engine runs,
//     and nothing while it does not, since it then runs nothing and starts
//     on nothing.
//   - A reference whose Secret or key is gone has its entry removed where
//     the revision the engine runs goes without it, so that no engine starts
//     anything with the value of a Secret Kubernetes no longer holds: by
//     Sync before the next document is applied, and by Withdraw while no
//     next document can be. The store serves every revision by that
//     revision's own reading of a name, though, and the revision the engine
//     runs is the last good one, which keeps every value it requires until
//     the next document is applied. So an absent name of the next document
//     that the running revision references is removed only where that
//     revision lets it go too, and a name the running revision lets go is
//     removed whatever the next document reads of it (Desired.Release). The
//     State records the reading of one revision (State.Activated,
//     State.Adopted): one the operator activated, with the Secret keys its
//     document lets go without, or one it found running the document it
//     renders; any other revision reads every name as required. A name the
//     running revision requires is left as it is (Result.Retained) until a
//     document that lets it go is running, and removed then. The engine
//     reads its store only when a revision activates, so each name removed
//     is recorded (State.Withdrawn) until one has, for the caller to make
//     one where none is applied, or the one it applied started with the
//     value still there. Which activation came after a removal, or after a
//     value written (State.Written), is told by the fleet's activation
//     epoch read after it (State.Observed, State.Ran), whoever activated it.
//
// # What the operator owns in the store
//
// The naming is ADR-0001's (adr/0001-name-store-entries-by-their-secret-reference.md),
// which render and the config reconciler depend on too.
//
// Every name StoreName produces starts with Prefix and ends with a hash of
// the reference, and Owns recognises exactly that shape. Sync deletes only
// names Owns accepts, so entries a person or a provisioner wrote under any
// other name are never touched, whatever the operator's state says.
//
// Whether the value under an owned name is still the operator's is the
// engine's record of the credential that last wrote it
// (engine.SecretInfo.OperatorID), which the engine takes from the write's
// authentication. A value written with any other credential, however the
// writer described itself, is written back and reported (Result.Overwritten).
//
// A seat's credential the engine provisions is the one entry the document
// names that the operator does not fill: the engine derives its name, which
// ProvisionedName copies (the vendor's name first, never Prefix), the
// engine's provisioning writes it, and Owns refuses it, so no Sync ever
// overwrites or collects what the engine minted (ADR-0004).
//
// # Why the state is kept in a Secret
//
// The engine's listing carries names and metadata, never values, and reading
// a value back is a logged break-glass operation. So the operator cannot ask
// the engine whether a stored value is the one a Secret now holds; it has to
// remember what it pushed. It remembers a fingerprint per name — an
// HMAC-SHA256 of the value under a random key kept beside the fingerprints —
// in a Secret of its own in the company's namespace. A fingerprint copied out
// of that Secret without the key confirms no guess about a value, and two
// names holding the same value have different fingerprints.
//
// No value reaches an error, a log line or a string this package formats:
// errors name the Secret and key, and Desired and State print names only.
package secretsync
