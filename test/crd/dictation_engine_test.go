package crd_test

import (
	"fmt"
	"strings"
	"testing"

	v1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// preparedDir is a recognizer prepared on the data volume, beside the store.
const preparedDir = "speech"

// dictationShapes are the spec.dictation blocks TestDictationFollowsTheEngine
// offers the API servers and the engine: each bound of Crewlet's
// (*Dictation).validate and its neighbours, the idle cut against the length
// with either unset, and the directory's length in bytes once trimmed.
var dictationShapes = []v1.DictationSettings{
	{},
	{Enabled: new(false)},
	{Enabled: new(true)},
	{MaxConcurrent: 1},
	{MaxConcurrent: 1024},
	{MaxConcurrent: 1025},
	{MaxConcurrent: -1},
	{SessionSeconds: 15},
	{SessionSeconds: 14},
	{SessionSeconds: 3600},
	{SessionSeconds: 3601},
	{SessionSeconds: -1},
	{IdleSeconds: 1},
	{IdleSeconds: 300},
	{IdleSeconds: 301, SessionSeconds: 3600},
	{IdleSeconds: -1},
	{IdleSeconds: 10, SessionSeconds: 10},
	{IdleSeconds: 20, SessionSeconds: 10},
	{IdleSeconds: 300, SessionSeconds: 299},
	{IdleSeconds: 300, SessionSeconds: 3601},
	{RecognizerDir: preparedDir},
	{RecognizerDir: "/srv/crewlet-speech"},
	{RecognizerDir: " /srv/crewlet-speech "},
	{RecognizerDir: "\t\t"},
	{RecognizerDir: "/" + strings.Repeat("a", 4095)},
	{RecognizerDir: "/" + strings.Repeat("a", 4096)},
	{RecognizerDir: "//" + strings.Repeat("ü", 2047)},
	{RecognizerDir: "/" + strings.Repeat("ü", 2048)},
	{RecognizerDir: "/" + strings.Repeat("ü", 2047) + "\u00a0\u0085"},
	{RecognizerDir: " /" + strings.Repeat("a", 4095)},
	{Enabled: new(false), RecognizerDir: preparedDir, MaxConcurrent: 8, SessionSeconds: 600, IdleSeconds: 30},
}

// dictationRefusesMore are the shapes the API refuses although Crewlet takes
// them, by their recognizerDir, and why.
var dictationRefusesMore = map[string]string{
	" /" + strings.Repeat("a", 4095): "white space padding a directory past 4096 characters: the API bounds the field " +
		"as written, as every string needs, and Crewlet trims it first",
}

// TestDictationFollowsTheEngine holds the API's verdict on an EngineClass's
// spec.dictation to `crewlet validate`'s on the crewlet.yaml the render
// writes for it, both ways: every shape the API admits is one the engine
// starts on, so no data pod of the class crash-loops, and every shape it
// refuses is one the engine refuses, but for dictationRefusesMore.
func TestDictationFollowsTheEngine(t *testing.T) {
	prefix := strings.ToLower(namespacePrefix(newNamespace(t)))
	for i, shape := range dictationShapes {
		t.Run(fmt.Sprintf("%02d", i), func(t *testing.T) {
			class := engineClass("", fmt.Sprintf("%s-dictation-%02d", prefix, i))
			class.Spec.Dictation = shape
			in := bootstrapInput()
			in.Class = class
			doc, err := render.Bootstrap(in)
			if err != nil {
				t.Fatal(err)
			}
			problems, out := validate(t, doc.YAML)
			for _, p := range problems {
				if !strings.HasPrefix(p, "dictation.") {
					t.Fatalf("crewlet validate refused the Tier A at %s, not only under dictation:\n%s", p, out)
				}
			}
			engine := len(problems) == 0
			why, more := dictationRefusesMore[shape.RecognizerDir]
			if more && !engine {
				t.Errorf("%+v is listed as refused by the API alone (%s), and crewlet validate refuses it:\n%s", shape, why, out)
			}
			for _, a := range admissions(t, class) {
				switch {
				case more && a.admitted:
					t.Errorf("%+v is listed as refused by the API alone (%s), and %s admits it", shape, why, a.server)
				case !more && a.admitted != engine:
					t.Errorf("%s admits %+v: %v; crewlet validate takes the crewlet.yaml the render writes for it: %v\n%s",
						a.server, shape, a.admitted, engine, out)
				}
			}
		})
	}
}
