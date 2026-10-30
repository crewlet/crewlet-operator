package controller

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/engine/enginetest"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// TestDataPodStartupSeconds holds the data pod's startup window to the
// recovery it has to cover: the boot's own bounds, by the class's object
// store, plus six seconds for every GiB the volume may hold, rounded up,
// unless the class names a window, and never past the API's day.
func TestDataPodStartupSeconds(t *testing.T) {
	t.Parallel()
	const nats, s3 = 1866, 2006
	classWith := func(seconds int32, backend crewletaiv1alpha1.ObjectBackend) *crewletaiv1alpha1.EngineClass {
		class := &crewletaiv1alpha1.EngineClass{}
		class.Spec.Data.StartupSeconds = seconds
		class.Spec.Objects.Backend = backend
		return class
	}
	unset := classWith(0, "")
	for _, c := range []struct {
		name    string
		class   *crewletaiv1alpha1.EngineClass
		volume  string
		seconds int32
	}{
		{"a volume of no size", unset, "0", nats},
		{"a quantity below zero, which no claim has", unset, "-2Gi", nats},
		{"the smallest volume", unset, "14Gi", nats + 14*6},
		{"the smallest volume, objects on the stream", classWith(0, crewletaiv1alpha1.ObjectBackendNATS), "14Gi", nats + 14*6},
		{"the smallest volume, objects in S3", classWith(0, crewletaiv1alpha1.ObjectBackendS3), "11Gi", s3 + 11*6},
		{"a volume of no size, objects in S3", classWith(0, crewletaiv1alpha1.ObjectBackendS3), "0", s3},
		{"a volume of whole GiB", unset, "100Gi", nats + 100*6},
		{"a part of a GiB counts as one", unset, "10737418241", nats + 11*6},
		{"a volume in decimal units", unset, "500G", nats + 466*6},
		{"a volume whose window would pass a day", unset, "20Ti", maxStartupSeconds},
		{"the largest volume whose window is a day", unset, "14089Gi", nats + 14089*6},
		{"a GiB more", unset, "14090Gi", maxStartupSeconds},
		{"a volume whose Value saturates", unset, "16Ei", maxStartupSeconds},
		{"a volume whose Value wraps below zero", unset, "9223372036854775808", maxStartupSeconds},
		{"a volume whose Value reads zero", unset, "1e30", maxStartupSeconds},
		{"the class's window, whatever the volume", classWith(2000, ""), "1Ti", 2000},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := dataPodStartupSeconds(c.class, resource.MustParse(c.volume)); got != c.seconds {
				t.Errorf("dataPodStartupSeconds(%s) = %d, want %d", c.volume, got, c.seconds)
			}
		})
	}
}

// TestDataPodBootCountsTheEnginesStateLogs holds the state logs the data
// pod's startup window counts (dataPodStateLogs) to the engine the suites
// run: a data node reports one replication loop for each state log it runs
// in its /fleet record. The join and the admission read each log one at a
// time, so a log the window does not count is a minute and more of reads a
// boot can be cut short on, which no other test sees: the engine reaches
// none of it on a disk that answers at once.
func TestDataPodBootCountsTheEnginesStateLogs(t *testing.T) {
	t.Parallel()
	e := enginetest.Start(t)
	client := e.Client()
	// The node reports its loops from its heartbeat, which may not have
	// beaten since its logs started when /health first answers.
	deadline := time.Now().Add(time.Minute)
	for {
		fleet, err := client.Fleet(t.Context())
		if err == nil {
			if node, ok := fleet.Node(fleet.ThisNode); ok && node.StateLogs > 0 {
				if node.StateLogs != dataPodStateLogs {
					t.Fatalf("a data node runs %d state logs, and the data pod's startup window counts %d",
						node.StateLogs, dataPodStateLogs)
				}
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the engine's /fleet reports no state logs for its own node within a minute: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// TestStartupFailureThreshold holds the startup probe's failures to the window
// they make up, counted as the kubelet counts it: from a first probe that may
// land at once, threshold-1 periods, never shorter than the window and short
// of a period more.
func TestStartupFailureThreshold(t *testing.T) {
	t.Parallel()
	for _, seconds := range []int32{1142, 1192, 1866, 1867, 1870, 1871, 1962, 2006, maxStartupSeconds - 1, maxStartupSeconds} {
		threshold := startupFailureThreshold(seconds)
		if window := (threshold - 1) * probePeriodSeconds; window < seconds || window >= seconds+probePeriodSeconds {
			t.Errorf("startupFailureThreshold(%d) = %d, a window of %ds after the first probe", seconds, threshold, window)
		}
	}
}

// TestSeatPodStartupSeconds holds a seat pod's window to the engine's bounds
// on a leaf's boot as read from its source, which the mirrors name: 952
// seconds before its object store, and the store's 190 on the event stream
// or the 240 of the probe of an S3 bucket.
func TestSeatPodStartupSeconds(t *testing.T) {
	t.Parallel()
	for backend, want := range map[crewletaiv1alpha1.ObjectBackend]int32{
		"":                                  1142,
		crewletaiv1alpha1.ObjectBackendNATS: 1142,
		crewletaiv1alpha1.ObjectBackendS3:   1192,
	} {
		class := &crewletaiv1alpha1.EngineClass{}
		class.Spec.Objects.Backend = backend
		if got := seatPodStartupSeconds(class); got != want {
			t.Errorf("seatPodStartupSeconds with objects.backend %q = %d, want %d", backend, got, want)
		}
	}
}

// TestDataVolumeSize holds the size the engine's event stream is sized from
// to the volumes the data pods actually have: the claim template's size (the
// existing StatefulSet's, or the class's before there is one), grown by an
// expansion of every claim once the expansion has landed, never shrunk by a
// claim given less than it asked for, and the smallest across the pods.
func TestDataVolumeSize(t *testing.T) {
	t.Parallel()
	// kept is the size the existing StatefulSet's claim template asks for.
	const kept = "16Gi"
	class := &crewletaiv1alpha1.EngineClass{}
	class.Spec.Data.Storage.Size = new(resource.MustParse("32Gi"))
	existing := &appsv1.StatefulSet{}
	existing.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
		{Name: "scratch", Spec: claimRequesting("1Gi").Spec},
		{Name: dataVolume, Spec: claimRequesting(kept).Spec},
	}
	bound := func(requested, granted string) corev1.PersistentVolumeClaim {
		claim := claimRequesting(requested)
		claim.Status.Capacity = corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(granted)}
		return claim
	}
	deleting := bound("64Gi", "64Gi")
	deleting.DeletionTimestamp = new(metav1.Now())
	for _, c := range []struct {
		name     string
		existing *appsv1.StatefulSet
		claims   []corev1.PersistentVolumeClaim
		want     string
	}{
		{"the class's size before there is a StatefulSet", nil, nil, "32Gi"},
		{"the StatefulSet's template, not the class's new size", existing, nil, kept},
		{"a claim not bound yet", existing, []corev1.PersistentVolumeClaim{claimRequesting(kept)}, kept},
		{"a claim bound at its size", existing, []corev1.PersistentVolumeClaim{bound(kept, kept)}, kept},
		{"a claim rounded up when it was made", existing, []corev1.PersistentVolumeClaim{bound(kept, "20Gi")}, kept},
		{"a claim given less than it asked", existing, []corev1.PersistentVolumeClaim{bound(kept, "8Gi")}, kept},
		{"an expansion not landed yet", existing, []corev1.PersistentVolumeClaim{bound("64Gi", kept)}, kept},
		{"an expansion partly landed", existing, []corev1.PersistentVolumeClaim{bound("64Gi", "48Gi")}, "48Gi"},
		{"an expansion landed", existing, []corev1.PersistentVolumeClaim{bound("64Gi", "64Gi")}, "64Gi"},
		{"an expansion landed past what was asked", existing, []corev1.PersistentVolumeClaim{bound("64Gi", "70Gi")}, "64Gi"},
		{
			"the smallest of the pods' claims", existing,
			[]corev1.PersistentVolumeClaim{bound("64Gi", "64Gi"), bound(kept, kept)}, kept,
		},
		{"a claim being deleted", existing, []corev1.PersistentVolumeClaim{deleting}, kept},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := dataVolumeSize(c.existing, class, c.claims); !got.Equal(resource.MustParse(c.want)) {
				t.Errorf("dataVolumeSize = %s, want %s", got.String(), c.want)
			}
		})
	}
}

// claimRequesting is a data volume claim that asks for size, not bound.
func claimRequesting(size string) corev1.PersistentVolumeClaim {
	return corev1.PersistentVolumeClaim{
		Spec: corev1.PersistentVolumeClaimSpec{
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{corev1.ResourceStorage: resource.MustParse(size)},
			},
		},
	}
}

// TestDataClaimWatch holds the watch that brings a claim's expansion to its
// Company: a data claim maps to the Company its labels name, any other claim
// to nothing, and only a change of the storage a claim asks for or was given
// queues it.
func TestDataClaimWatch(t *testing.T) {
	t.Parallel()
	company := &crewletaiv1alpha1.Company{Namespace: testTenant, Name: companyName}
	claim := claimRequesting("16Gi")
	claim.Namespace = company.Namespace
	claim.Labels = labelsFor(company, componentData)
	if got := companyOfDataClaim(t.Context(), &claim); len(got) != 1 ||
		got[0].Namespace != company.Namespace || got[0].Name != company.Name {
		t.Errorf("a data claim maps to %v, want %s/%s", got, company.Namespace, company.Name)
	}
	for _, labels := range []map[string]string{nil, labelsFor(company, componentSeats)} {
		other := claim.DeepCopy()
		other.Labels = labels
		if got := companyOfDataClaim(t.Context(), other); got != nil {
			t.Errorf("a claim labelled %v maps to %v, want nothing", labels, got)
		}
	}

	resized := claimResized()
	grown := claim.DeepCopy()
	grown.Spec.Resources.Requests[corev1.ResourceStorage] = resource.MustParse("32Gi")
	granted := claim.DeepCopy()
	granted.Status.Capacity = corev1.ResourceList{corev1.ResourceStorage: resource.MustParse("16Gi")}
	annotated := claim.DeepCopy()
	annotated.Annotations = map[string]string{EngineVersionAnnotation: "v0.2.0"}
	for _, c := range []struct {
		name    string
		updated *corev1.PersistentVolumeClaim
		want    bool
	}{
		{"a request grown", grown, true},
		{"a capacity granted", granted, true},
		{"an annotation set", annotated, false},
	} {
		if got := resized.Update(event.UpdateEvent{ObjectOld: &claim, ObjectNew: c.updated}); got != c.want {
			t.Errorf("%s: claimResized = %v, want %v", c.name, got, c.want)
		}
	}
	if resized.Create(event.CreateEvent{Object: &claim}) || resized.Delete(event.DeleteEvent{Object: &claim}) {
		t.Error("claimResized queues a claim's creation or deletion, which the StatefulSet reports")
	}
}

// testTenant is the namespace of the Companies whose pods these tests render.
const testTenant = "tenant-a"

// podOfClass is an engine pod's template of a class, as the reconciler renders
// it: a data pod's (dataPodOf) or a seat pod's (seatPodOf).
type podOfClass func(*testing.T, *crewletaiv1alpha1.EngineClass) corev1.PodTemplateSpec

// dataPodOf is a data pod's template of class, decoded.
func dataPodOf(t *testing.T, class *crewletaiv1alpha1.EngineClass) corev1.PodTemplateSpec {
	t.Helper()
	company := &crewletaiv1alpha1.Company{Namespace: testTenant, Name: companyName}
	return decodedPod(t)(podTemplate(company, class, &workload{
		image: engineImageV020, bootstrap: &render.BootstrapDocument{}, startupSeconds: 1866,
	}))
}

// seatPodOf is a seat pod's template of class, decoded, its pool planned as
// the reconciler plans one, from the class's defaults.
func seatPodOf(t *testing.T, class *crewletaiv1alpha1.EngineClass) corev1.PodTemplateSpec {
	t.Helper()
	company := &crewletaiv1alpha1.Company{Namespace: testTenant, Name: companyName}
	plan := render.PoolPlanOf(&crewletaiv1alpha1.SeatPool{Name: poolGeneral}, class.Spec.SeatPools.Defaults)
	return decodedPod(t)(seatPodTemplate(company, class, &seatWorkload{name: "nimbus-seats-general", pool: poolGeneral, plan: &plan},
		"nimbus-seats-general-bootstrap", engineImageV020, &render.BootstrapDocument{}, ""))
}

// decodedPod decodes a rendered template into the type, failing t on an
// error: the apply configuration and the type share their JSON form.
func decodedPod(t *testing.T) func(*corev1ac.PodTemplateSpecApplyConfiguration, error) corev1.PodTemplateSpec {
	return func(applied *corev1ac.PodTemplateSpecApplyConfiguration, err error) corev1.PodTemplateSpec {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(applied)
		if err != nil {
			t.Fatal(err)
		}
		var pod corev1.PodTemplateSpec
		if err := json.Unmarshal(raw, &pod); err != nil {
			t.Fatal(err)
		}
		return pod
	}
}

// TestEnginePodsKeepBoxesOnAVolumeOfTheirOwn holds every engine pod, a data
// pod and a seat pod alike, to a sandboxes volume of its own mounted at
// render.SandboxDir: an emptyDir on the node's disk, never in memory, under
// the limit the class sets for that kind of pod or render's default, and
// nested in no other mount. A box's checkout written under the data volume
// would take the event stream's room, under a seat pod's scratch volume its
// limit, and in memory the engine's.
func TestEnginePodsKeepBoxesOnAVolumeOfTheirOwn(t *testing.T) {
	t.Parallel()
	// classWith is a class whose data pods' and seat pods' sandboxes limits
	// are data and pools, each unset when empty.
	classWith := func(data, pools string) *crewletaiv1alpha1.EngineClass {
		class := &crewletaiv1alpha1.EngineClass{}
		class.Spec.Data.Storage.Size = new(resource.MustParse("16Gi"))
		if data != "" {
			class.Spec.Data.SandboxSizeLimit = new(resource.MustParse(data))
		}
		if pools != "" {
			class.Spec.SeatPools.Defaults.SandboxSizeLimit = new(resource.MustParse(pools))
		}
		return class
	}
	for _, c := range []struct {
		name  string
		pod   podOfClass
		class *crewletaiv1alpha1.EngineClass
		want  string
	}{
		{"a data pod of a class that sets no limit", dataPodOf, classWith("", ""), render.DefaultSandboxSizeLimit},
		{"a data pod of a class that sets one for seat pods alone", dataPodOf, classWith("", "8Gi"), render.DefaultSandboxSizeLimit},
		{"a data pod of a class that sets its limit", dataPodOf, classWith("50Gi", "8Gi"), "50Gi"},
		{"a seat pod of a class that sets no limit", seatPodOf, classWith("", ""), render.DefaultSandboxSizeLimit},
		{"a seat pod of a class that sets one for data pods alone", seatPodOf, classWith("50Gi", ""), render.DefaultSandboxSizeLimit},
		{"a seat pod of a class that sets its limit", seatPodOf, classWith("50Gi", "8Gi"), "8Gi"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			pod := c.pod(t, c.class)

			var volumes []corev1.Volume
			for _, v := range pod.Spec.Volumes {
				if v.Name == sandboxesVolume {
					volumes = append(volumes, v)
				}
			}
			if len(volumes) != 1 {
				t.Fatalf("the pod has %d volumes named %s, want 1: %+v", len(volumes), sandboxesVolume, pod.Spec.Volumes)
			}
			dir := volumes[0].EmptyDir
			switch {
			case dir == nil:
				t.Errorf("volume %s is %+v, want an emptyDir", sandboxesVolume, volumes[0].VolumeSource)
			case dir.Medium != corev1.StorageMediumDefault:
				t.Errorf("volume %s is an emptyDir of medium %q, want the node's disk", sandboxesVolume, dir.Medium)
			case dir.SizeLimit == nil || !dir.SizeLimit.Equal(resource.MustParse(c.want)):
				t.Errorf("volume %s is limited to %v, want %s", sandboxesVolume, dir.SizeLimit, c.want)
			}

			if len(pod.Spec.Containers) != 1 {
				t.Fatalf("the pod has %d containers, want the engine's alone", len(pod.Spec.Containers))
			}
			var mounted int
			for _, m := range pod.Spec.Containers[0].VolumeMounts {
				if m.Name == sandboxesVolume {
					mounted++
					if m.MountPath != render.SandboxDir || m.SubPath != "" || m.ReadOnly {
						t.Errorf("volume %s is mounted as %+v, want all of it, writable, at %s", sandboxesVolume, m, render.SandboxDir)
					}
					continue
				}
				if within(m.MountPath, render.SandboxDir) || within(render.SandboxDir, m.MountPath) {
					t.Errorf("volume %s mounted at %s nests with the boxes at %s", m.Name, m.MountPath, render.SandboxDir)
				}
			}
			if mounted != 1 {
				t.Errorf("volume %s is mounted %d times, want once", sandboxesVolume, mounted)
			}
		})
	}
}

// TestEnginePodsBoundWhatTheyWriteOutsideABox holds every engine pod, a data
// pod and a seat pod alike, to a limit on all its engine container writes but
// the data volume: /tmp and HOME on one scratch emptyDir of the node's disk,
// under the limit the class sets for that kind of pod or render's default,
// and every other writable mount on a bounded emptyDir too. A seat's Direct
// executor run is confined to nothing but the pod, and Crewlet points only
// its temporary directories into its box, so a command that names /tmp or
// the engine's home outright writes there: an unbounded emptyDir would let
// it fill the node's disk under the pods beside it.
func TestEnginePodsBoundWhatTheyWriteOutsideABox(t *testing.T) {
	t.Parallel()
	// classWith is a class whose data pods' and seat pods' scratch limits
	// are data and pools, each unset when empty.
	classWith := func(data, pools string) *crewletaiv1alpha1.EngineClass {
		class := &crewletaiv1alpha1.EngineClass{}
		class.Spec.Data.Storage.Size = new(resource.MustParse("16Gi"))
		if data != "" {
			class.Spec.Data.ScratchSizeLimit = new(resource.MustParse(data))
		}
		if pools != "" {
			class.Spec.SeatPools.Defaults.ScratchSizeLimit = new(resource.MustParse(pools))
		}
		return class
	}
	for _, c := range []struct {
		name  string
		pod   podOfClass
		class *crewletaiv1alpha1.EngineClass
		// claim is whether the pod may write its data volume, which a
		// volume claim template gives a data pod and no emptyDir bounds.
		claim bool
		want  string
	}{
		{"a data pod of a class that sets no limit", dataPodOf, classWith("", ""), true, render.DefaultScratchSizeLimit},
		{"a data pod of a class that sets one for seat pods alone", dataPodOf, classWith("", "4Gi"), true, render.DefaultScratchSizeLimit},
		{"a data pod of a class that sets its limit", dataPodOf, classWith("6Gi", "4Gi"), true, "6Gi"},
		{"a seat pod of a class that sets no limit", seatPodOf, classWith("", ""), false, render.DefaultScratchSizeLimit},
		{"a seat pod of a class that sets one for data pods alone", seatPodOf, classWith("6Gi", ""), false, render.DefaultScratchSizeLimit},
		{"a seat pod of a class that sets its limit", seatPodOf, classWith("6Gi", "4Gi"), false, "4Gi"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			pod := c.pod(t, c.class)
			volumes := make(map[string]corev1.Volume, len(pod.Spec.Volumes))
			for _, v := range pod.Spec.Volumes {
				volumes[v.Name] = v
			}
			if len(pod.Spec.Containers) != 1 {
				t.Fatalf("the pod has %d containers, want the engine's alone", len(pod.Spec.Containers))
			}

			// Every place the engine container writes is bounded, but for a
			// data pod's own data volume, which its claim sizes.
			for _, m := range pod.Spec.Containers[0].VolumeMounts {
				if m.ReadOnly {
					continue
				}
				v, ok := volumes[m.Name]
				switch {
				case !ok && c.claim && m.Name == dataVolume && m.MountPath == render.DataDir:
				case !ok:
					t.Errorf("%s is writable on volume %s, which is neither the pod's nor its data volume", m.MountPath, m.Name)
				case v.EmptyDir == nil:
					t.Errorf("%s is writable on volume %s, %+v, want a bounded emptyDir", m.MountPath, m.Name, v.VolumeSource)
				case v.EmptyDir.SizeLimit == nil:
					t.Errorf("%s is writable on emptyDir %s, which has no size limit", m.MountPath, m.Name)
				case v.EmptyDir.Medium != corev1.StorageMediumDefault:
					t.Errorf("%s is on emptyDir %s of medium %q, want the node's disk", m.MountPath, m.Name, v.EmptyDir.Medium)
				}
			}

			// /tmp and HOME share the scratch volume, under the class's
			// limit for this kind of pod.
			for path, sub := range map[string]string{tmpDir: tmpSubPath, homeDir: homeSubPath} {
				i := slices.IndexFunc(pod.Spec.Containers[0].VolumeMounts, func(m corev1.VolumeMount) bool {
					return m.MountPath == path
				})
				if i < 0 {
					t.Errorf("nothing is mounted at %s", path)
					continue
				}
				if m := pod.Spec.Containers[0].VolumeMounts[i]; m.Name != scratchVolume || m.SubPath != sub || m.ReadOnly {
					t.Errorf("%s is mounted as %+v, want %s's %s, writable", path, m, scratchVolume, sub)
				}
			}
			scratch, ok := volumes[scratchVolume]
			switch {
			case !ok || scratch.EmptyDir == nil:
				t.Errorf("the pod has no scratch emptyDir: %+v", pod.Spec.Volumes)
			case scratch.EmptyDir.SizeLimit == nil || !scratch.EmptyDir.SizeLimit.Equal(resource.MustParse(c.want)):
				t.Errorf("volume %s is limited to %v, want %s", scratchVolume, scratch.EmptyDir.SizeLimit, c.want)
			}
		})
	}
}

// within reports whether path is dir or under it.
func within(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, strings.TrimSuffix(dir, "/")+"/")
}
