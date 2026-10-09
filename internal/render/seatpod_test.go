package render

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

// seatPoolsInput is the company that runs seats in every kind of place, whose
// pools the seat pod cases render.
var seatPoolsInput = filepath.Join("testdata", "company", "seat-pools.input.yaml")

// testDataMembers are the data pods a seat pod joins, as the data
// StatefulSet's headless Service names them.
var testDataMembers = []string{"nimbus-data-0.nimbus-data.tenant-nimbus.svc"}

// seatPodCase is the Tier A of one seat pool's pods.
type seatPodCase struct {
	name string
	// file holds the class, the Company and its Seats.
	file string
	pool string
	// seat is the seat a PerSeat pool's pods run.
	seat    string
	objects *S3Credentials
}

func seatPodCases() []seatPodCase {
	return []seatPodCase{
		// A Shared pool sized by the class's defaults.
		{name: "shared", file: seatPoolsInput, pool: poolGeneral},
		// A Shared pool sizing itself.
		{name: "shared-sized", file: seatPoolsInput, pool: poolBatch},
		// The pods of one seat of a PerSeat pool.
		{name: "per-seat", file: seatPoolsInput, pool: poolIsolated, seat: seatAna},
		// The object store's keys, as the data pods have them.
		{name: "s3", file: filepath.Join("testdata", "seatpod", "s3.input.yaml"), pool: poolGeneral, objects: &testObjects},
	}
}

// input is the case's seat pod input, with its pool planned from the
// company's Seats, and the input of the company's data pods.
func (c seatPodCase) input(t *testing.T) (*SeatPodInput, *BootstrapInput) {
	t.Helper()
	o := load(t, c.file)
	topology, err := PlanTopology(o.company, o.class, o.input.Seats)
	if err != nil {
		t.Fatal(err)
	}
	pool := topology.Pool(c.pool)
	if pool == nil {
		t.Fatalf("%s declares no pool %s", c.file, c.pool)
	}
	keyring := Keyring{ActiveKeyID: "k1", Keys: map[string]string{"k1": testKeyMaterial}}
	seatPod := &SeatPodInput{
		Company: o.company, Class: o.class, Pool: pool, Seat: c.seat,
		Keyring: keyring, Objects: c.objects, DataMembers: testDataMembers,
	}
	data := &BootstrapInput{
		Company: o.company, Class: o.class, Keyring: keyring, Tokens: testTokens, Objects: c.objects,
		VolumeSize: *o.class.Spec.Data.Storage.Size,
	}
	return seatPod, data
}

// TestSeatPodBootstrapGolden pins each seat pod's Tier A and the environment
// it needs.
func TestSeatPodBootstrapGolden(t *testing.T) {
	for _, c := range seatPodCases() {
		t.Run(c.name, func(t *testing.T) {
			in, _ := c.input(t)
			doc, err := SeatPodBootstrap(in)
			if err != nil {
				t.Fatal(err)
			}
			golden(t, filepath.Join("testdata", "seatpod", c.name+".yaml"), doc.YAML)
			env, err := yaml.Marshal(doc.Env)
			if err != nil {
				t.Fatal(err)
			}
			golden(t, filepath.Join("testdata", "seatpod", c.name+".env.yaml"), env)
		})
	}
}

// TestSeatPodEnvIsExactlyTheReferences holds a seat pod's Env to its
// document, as TestBootstrapEnvIsExactlyTheReferences does a data pod's.
func TestSeatPodEnvIsExactlyTheReferences(t *testing.T) {
	for _, c := range seatPodCases() {
		t.Run(c.name, func(t *testing.T) {
			in, _ := c.input(t)
			doc, err := SeatPodBootstrap(in)
			if err != nil {
				t.Fatal(err)
			}
			var referenced []string
			for _, m := range envPointer.FindAllStringSubmatch(string(doc.YAML), -1) {
				referenced = append(referenced, m[1])
			}
			slices.Sort(referenced)
			referenced = slices.Compact(referenced)
			given := make([]string, 0, len(doc.Env))
			for _, e := range doc.Env {
				given = append(given, e.Name)
			}
			slices.Sort(given)
			if !slices.Equal(referenced, given) {
				t.Errorf("the document refers to %v, the pod is given %v", referenced, given)
			}
			for _, e := range doc.Env {
				if e.Name == EnvSeat && e.Value != c.seat {
					t.Errorf("%s = %q, want the pod's seat %q", EnvSeat, e.Value, c.seat)
				}
			}
		})
	}
}

// TestSeatPodSharesTheCompanysBlocksWithTheDataPod: a seat pod opens the
// company document and the secret store with the company's keyring, names the
// object store the company recorded (the engine refuses to start a node that
// names another), creates streams with the data pods' replica count, and
// names the same backup owner. Each of those blocks must be the data pod's,
// value for value. A seat pod binds the data pods' API port for its probes
// alone, and holds no token.
func TestSeatPodSharesTheCompanysBlocksWithTheDataPod(t *testing.T) {
	for _, c := range seatPodCases() {
		t.Run(c.name, func(t *testing.T) {
			seatPodIn, dataIn := c.input(t)
			seatPod, err := SeatPodBootstrap(seatPodIn)
			if err != nil {
				t.Fatal(err)
			}
			data, err := Bootstrap(dataIn)
			if err != nil {
				t.Fatal(err)
			}
			seatPodLeaves, dataLeaves := flatten(t, seatPod.YAML), flatten(t, data.YAML)
			for _, prefix := range []string{"secrets.", "store.objects.", "coordination.", "retention.", "stream.replicas", "stream.type"} {
				for path, want := range dataLeaves {
					if strings.HasPrefix(path, prefix) && seatPodLeaves[path] != want {
						t.Errorf("%s is %v on the seat pod and %v on the data pod", path, seatPodLeaves[path], want)
					}
				}
				for path, got := range seatPodLeaves {
					if _, ok := dataLeaves[path]; strings.HasPrefix(path, prefix) && !ok {
						t.Errorf("%s is %v on the seat pod and absent on the data pod", path, got)
					}
				}
			}
			// The listener its probes are served on, on the data pods'
			// port, and nothing else of the API: no token, no reads.
			for path, got := range seatPodLeaves {
				if strings.HasPrefix(path, "api.") && path != "api.host" && path != "api.port" {
					t.Errorf("the seat pod's document sets %s = %v; a seat pod serves only its probes", path, got)
				}
			}
			for _, path := range []string{"api.host", "api.port"} {
				if seatPodLeaves[path] != dataLeaves[path] {
					t.Errorf("%s is %v on the seat pod and %v on the data pod", path, seatPodLeaves[path], dataLeaves[path])
				}
			}
			for _, token := range []string{testTokens.Operator, testTokens.Access} {
				if strings.Contains(string(seatPod.YAML), token) {
					t.Errorf("the seat pod's document holds the API token %q", token)
				}
			}
		})
	}
}

// TestSeatPodJoinsEveryDataMember lists every data pod as a leaf URL on the
// leaf port the data pods listen on, so a seat pod rejoins through another
// member when one is down.
func TestSeatPodJoinsEveryDataMember(t *testing.T) {
	in, _ := seatPodCases()[0].input(t)
	in.DataMembers = []string{"nimbus-data-0.nimbus-data.ns.svc", "nimbus-data-1.nimbus-data.ns.svc"}
	doc, err := SeatPodBootstrap(in)
	if err != nil {
		t.Fatal(err)
	}
	leaves := flatten(t, doc.YAML)
	for i, want := range []string{"nats-leaf://nimbus-data-0.nimbus-data.ns.svc:7422", "nats-leaf://nimbus-data-1.nimbus-data.ns.svc:7422"} {
		if got := leaves[Path{"stream", "leaf", "urls", i}.String()]; got != want {
			t.Errorf("stream.leaf.urls[%d] = %v, want %s", i, got, want)
		}
	}
}

// TestSeatPodBootstrapRefusesWhatItCannotRender covers the inputs a caller
// can get wrong: a seat pod with no data pod to join, and a seat given to a
// pool of the other mode.
func TestSeatPodBootstrapRefusesWhatItCannotRender(t *testing.T) {
	for _, c := range []struct {
		name   string
		mutate func(*SeatPodInput)
		want   string
	}{
		{"no data member", func(in *SeatPodInput) { in.DataMembers = nil }, "no data pod was given"},
		{"a PerSeat pool without its seat", func(in *SeatPodInput) {
			plan := *in.Pool
			plan.Mode = "PerSeat"
			in.Pool = &plan
		}, "no seat was given"},
		{"a Shared pool given a seat", func(in *SeatPodInput) { in.Seat = seatAna }, "were given seat ana"},
		{"an unusable keyring", func(in *SeatPodInput) { in.Keyring = Keyring{} }, "holds no key"},
	} {
		t.Run(c.name, func(t *testing.T) {
			in, _ := seatPodCases()[0].input(t)
			c.mutate(in)
			_, err := SeatPodBootstrap(in)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want one saying %q", err, c.want)
			}
		})
	}
}

// TestBootstrapOpensTheLeafListenerWhenTheClassAllowsPools: the class, not
// the Company, decides whether the data pods listen for seat pods, so a
// company of a class that allows pools listens before it has one, and one of
// a class that does not never listens.
func TestBootstrapOpensTheLeafListenerWhenTheClassAllowsPools(t *testing.T) {
	for _, c := range bootstrapCases() {
		t.Run(c.name, func(t *testing.T) {
			in := c.input(t)
			doc, err := Bootstrap(in)
			if err != nil {
				t.Fatal(err)
			}
			got := flatten(t, doc.YAML)["stream.leaf.port"]
			if in.Class.Spec.SeatPools.Policy == "Allowed" {
				if got != float64(LeafPort) {
					t.Fatalf("stream.leaf.port = %v under a class that allows pools, want %d", got, LeafPort)
				}
				return
			}
			if got != nil {
				t.Fatalf("stream.leaf.port = %v under a class that does not allow pools, want none", got)
			}
		})
	}
	if !slices.ContainsFunc(bootstrapCases(), func(c bootstrapCase) bool {
		return c.input(t).Class.Spec.SeatPools.Policy == "Allowed"
	}) {
		t.Fatal("no bootstrap case allows seat pools, so the listener is never checked")
	}
}
