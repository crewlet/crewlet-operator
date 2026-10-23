package controller

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine"
	"github.com/crewlet/crewlet-operator/internal/secretsync"
)

// The three shapes of what a message quotes at its end: the engine's words,
// written as a sentence with its full stop (its 503 unavailable, as crewlet's
// main and crewlet#142 word it under a Retry-After); the engine's words with
// its hint, which the client puts in parentheses after the message, both
// sentences with their full stops (the 503 a node answers while it starts,
// detail and hint as the engine's httpjson.Sentence ends them); and an error
// of Go's, which has none.
var (
	quotedSentence = &engine.APIError{StatusCode: http.StatusServiceUnavailable, Code: "unavailable",
		Message: "Can't answer right now. Try again in a moment, or ask another node.", RetryAfter: 2 * time.Second}
	quotedHint = &engine.APIError{StatusCode: http.StatusServiceUnavailable, Code: "starting",
		Message: "This node is recovering what it holds — its store, its broker and its copy of the estate — " +
			"and serves nothing until it has started.",
		Hint: `Retry after the Retry-After: once the node serves, /health stops answering status "starting" ` +
			`and /ready stops naming reason "starting".`,
		RetryAfter: 10 * time.Second}
	errQuotedClause = errors.New("dial tcp 10.0.0.7:8080: connect: connection refused")
)

// endsOnce reports whether message ends with exactly one full stop: one not
// after another, or one a closing parenthesis follows, the end of a quoted
// hint, with nothing after it.
func endsOnce(message string) bool {
	if body, quoted := strings.CutSuffix(message, ")"); quoted {
		return strings.HasSuffix(body, ".") && !strings.HasSuffix(body, "..")
	}
	return strings.HasSuffix(message, ".") && !strings.HasSuffix(message, "..") && !strings.HasSuffix(message, ".).")
}

func TestEndedEndsASentenceOnce(t *testing.T) {
	for _, c := range []struct{ text, want string }{
		{"the engine refused it", "the engine refused it."},
		{"Try again in a moment.", "Try again in a moment."},
		{"Is it running?", "Is it running?"},
		{"It stopped!", "It stopped!"},
		{"  padded  ", "padded."},
		{"", ""},
		{"the value (Secret ns/a)", "the value (Secret ns/a)."},
		{"It failed. (Try again later.)", "It failed. (Try again later.)"},
		{"It failed (try again later)", "It failed (try again later)."},
		{"Is it running? (Ask the node!)", "Is it running? (Ask the node!)"},
		{")", ")."},
	} {
		if got := ended(c.text); got != c.want {
			t.Errorf("ended(%q) = %q, want %q", c.text, got, c.want)
		}
	}
}

// TestAConditionThatQuotesAnErrorEndsItOnce holds every condition that ends
// with an error the operator met to one full stop, whether the error ends in
// the engine's sentence or in Go's words.
func TestAConditionThatQuotesAnErrorEndsItOnce(t *testing.T) {
	for _, err := range []error{quotedSentence, quotedHint, errQuotedClause} {
		t.Run(err.Error(), func(t *testing.T) {
			run := &configRun{CompanyConfigReconciler: &CompanyConfigReconciler{}, company: &crewletaiv1alpha1.Company{}}
			run.unavailable(err)
			seat := &crewletaiv1alpha1.Seat{Namespace: "ns", Name: seatAna, Generation: 1}
			p := newPart(seat)
			run.fleetErr = err
			run.decideReady(p, seat)
			ready := seat.Status.Conditions[0]
			for name, message := range map[string]string{
				"ConfigApplied":   run.configApplied.Message,
				"EngineHealthy":   engineHealthy(nil, err).Message,
				"IdentityClaimed": identityClaimed(engine.Health{}, err, nil, "", "").Message,
				"a Seat's Ready":  ready.Message,
			} {
				if !endsOnce(message) {
					t.Errorf("%s does not end with one full stop: %q", name, message)
				}
			}
		})
	}
}

// TestAFailedApplyAccountEndsWhatItQuotesOnce holds ApplyFailed's account to
// one full stop after what it quotes: the engine's answer when its record of
// the attempt cannot be read, and its account of an apply that failed, which
// is a clause or a sentence as the engine had it.
func TestAFailedApplyAccountEndsWhatItQuotesOnce(t *testing.T) {
	written := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name, fleet string
		status      int
	}{
		{name: "a record that cannot be read", status: http.StatusServiceUnavailable,
			fleet: `{"error":"unavailable","message":"Can't answer right now. Try again in a moment, or ask another node."}`},
		{name: "an account the engine ends", fleet: fleet(2, "r2", "error", "Something on this node failed.")},
		{name: "an account the engine does not end", fleet: fleet(2, "r2", "error", "engine: apply: no sandbox cell")},
	} {
		t.Run(c.name, func(t *testing.T) {
			cl := (&fakeEngine{health: health("isolated", "isolated", 1), fleet: c.fleet, fleetStatus: c.status}).client(t)
			run, _ := verifyRun(written.Add(time.Second), written, crewletaiv1alpha1.ReasonApplied, nil)
			h, err := cl.Health(context.Background())
			run.verifyApplied(context.Background(), cl, h, err, ctrl.Result{})
			account, _, _ := strings.Cut(run.configApplied.Message, " It reports status")
			if run.configApplied.Reason != crewletaiv1alpha1.ReasonApplyFailed || !endsOnce(account) {
				t.Errorf("ConfigApplied is %s: %q; want ApplyFailed, its account ending with one full stop",
					run.configApplied.Reason, run.configApplied.Message)
			}
		})
	}
}

// TestASyncThatFailsEndsWhatItQuotesOnce holds SecretsSynced to one full stop
// after the engine's refusal of a write to its secret store.
func TestASyncThatFailsEndsWhatItQuotesOnce(t *testing.T) {
	ctx := context.Background()
	w := newWithdrawal(t, runningLetsGo)
	w.engine.secretsDown = true
	ref := types.NamespacedName{Namespace: w.run.company.Namespace, Name: secretSyncStateName(w.run.company)}
	if _, _, err := w.run.sync(ctx, w.client, w.resolve(t), w.state(t), ref); err == nil {
		t.Fatal("the sync succeeded against a store that refused it")
	}
	if c := w.run.secretsSynced; c == nil || c.Status != metav1.ConditionFalse || !endsOnce(c.Message) {
		t.Errorf("SecretsSynced is %+v, want False, its message ending with one full stop", c)
	}
}

// TestAReloadThatCannotBeReadBackEndsWhatItQuotesOnce holds the Warning of an
// activation the operator could not read back to one full stop after the
// engine's answer to the read, before the sentence that follows it.
func TestAReloadThatCannotBeReadBackEndsWhatItQuotesOnce(t *testing.T) {
	ctx := context.Background()
	w := newWithdrawal(t, runningLetsGo)
	// The operator asks for the activation: the engine did not at the removal.
	w.engine.reloadAnswer = new(map[string]any(nil))
	state, res, err := secretsync.Sync(ctx, w.client, w.resolve(t), w.state(t), operatorLogin)
	if err != nil {
		t.Fatal(err)
	}
	w.engine.afterReload = func(e *storeEngine) { e.configDown = true }
	if _, err := w.run.reactivate(ctx, w.client, nil, res.Reload, res.ReloadAction, state); err != nil {
		t.Fatal(err)
	}
	recorded := drained(w.events)
	i := slices.IndexFunc(recorded, func(e string) bool { return strings.HasPrefix(e, "Warning Reactivated ") })
	if i < 0 {
		t.Fatalf("events %q, want a Warning Reactivated", recorded)
	}
	if strings.Contains(recorded[i], "..") {
		t.Errorf("the Warning quotes the engine with two full stops: %q", recorded[i])
	}
}

// quotedAtTheEnd is a format whose last verb quotes text and ends the
// sentence: "…: %s." and "…: %v.".
var quotedAtTheEnd = regexp.MustCompile(`: %[sv]\.$`)

// TestQuotedEndingsGoThroughEnded holds the package's messages to ended: no
// message ends a sentence with a full stop of its own, neither as
// `text + "."` nor as a format ending ": %s." or ": %v.". Either is right only
// while what comes before it has none, and an error that carries the
// engine's words, which the engine ends with one, makes it two. A quote in
// the middle of a message (a pause's reason, a read that failed, followed by
// a sentence of the operator's) is not a shape this can see: those end the
// sentence that quotes them through ended too, and the tests beside this one
// hold each.
func TestQuotedEndingsGoThroughEnded(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var found []string
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		// joined are the "." joins inside a longer join, such as a host
		// name's labels: a full stop that ends nothing.
		joined := map[ast.Expr]bool{}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.FuncDecl:
				// ended's own body is where the rule is kept.
				return n.Name.Name != "ended"
			case *ast.BinaryExpr:
				if inner, ok := n.X.(*ast.BinaryExpr); ok && n.Op == token.ADD {
					joined[inner] = true
				}
				if lit, ok := n.Y.(*ast.BasicLit); ok && n.Op == token.ADD && lit.Value == `"."` && !joined[n] {
					found = append(found, fset.Position(n.Pos()).String()+`: ends a message with a "." of its own`)
				}
			case *ast.BasicLit:
				if text, err := strconv.Unquote(n.Value); n.Kind == token.STRING && err == nil && quotedAtTheEnd.MatchString(text) {
					found = append(found, fset.Position(n.Pos()).String()+": ends a quote with a full stop of its own")
				}
			}
			return true
		})
	}
	if len(found) == 0 {
		return
	}
	t.Errorf("end these through ended:\n%s", strings.Join(found, "\n"))
}
