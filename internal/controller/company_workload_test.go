package controller

import (
	"encoding/json"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"

	crewletaiv1alpha1 "github.com/crewlet/crewlet-operator/api/v1alpha1"
	"github.com/crewlet/crewlet-operator/internal/render"
)

// TestDataPodStartupSeconds holds the data pod's startup window to the
// recovery it has to cover: the boot's own bounds, by the class's object
// store, plus six seconds for every GiB the volume may hold, rounded up,
// unless the class names a window, and never past the API's day.
func TestDataPodStartupSeconds(t *testing.T) {
	t.Parallel()
	const nats, s3 = 1746, 1886
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
		{"the smallest volume", unset, "12Gi", nats + 12*6},
		{"the smallest volume, objects on the stream", classWith(0, crewletaiv1alpha1.ObjectBackendNATS), "12Gi", nats + 12*6},
		{"the smallest volume, objects in S3", classWith(0, crewletaiv1alpha1.ObjectBackendS3), "9Gi", s3 + 9*6},
		{"a volume of no size, objects in S3", classWith(0, crewletaiv1alpha1.ObjectBackendS3), "0", s3},
		{"a volume of whole GiB", unset, "100Gi", nats + 100*6},
		{"a part of a GiB counts as one", unset, "10737418241", nats + 11*6},
		{"a volume in decimal units", unset, "500G", nats + 466*6},
		{"a volume whose window would pass a day", unset, "20Ti", maxStartupSeconds},
		{"the largest volume whose window is a day", unset, "14109Gi", nats + 14109*6},
		{"a GiB more", unset, "14110Gi", maxStartupSeconds},
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

// TestStartupFailureThreshold holds the startup probe's failures to the window
// they make up, counted as the kubelet counts it: from a first probe that may
// land at once, threshold-1 periods, never shorter than the window and short
// of a period more.
func TestStartupFailureThreshold(t *testing.T) {
	t.Parallel()
	for _, seconds := range []int32{1142, 1192, 1746, 1747, 1750, 1751, 1842, 1886, maxStartupSeconds - 1, maxStartupSeconds} {
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
	company := &crewletaiv1alpha1.Company{Namespace: "tenant-a", Name: companyName}
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

// TestEnginePodsKeepBoxesOnAVolumeOfTheirOwn holds every engine pod, a data
// pod and a seat pod alike, to a sandboxes volume of its own mounted at
// render.SandboxDir: an emptyDir on the node's disk, never in memory, under
// the limit the class sets for that kind of pod or render's default, and
// nested in no other mount. A box's checkout written under the data volume
// would take the event stream's room, under a seat pod's scratch volume its
// limit, and in memory the engine's.
func TestEnginePodsKeepBoxesOnAVolumeOfTheirOwn(t *testing.T) {
	t.Parallel()
	company := &crewletaiv1alpha1.Company{Namespace: "tenant-a", Name: companyName}
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
	dataPod := func(class *crewletaiv1alpha1.EngineClass) (*corev1ac.PodTemplateSpecApplyConfiguration, error) {
		return podTemplate(company, class, &workload{
			image: engineImageV020, bootstrap: &render.BootstrapDocument{}, startupSeconds: 1746,
		})
	}
	seatPod := func(class *crewletaiv1alpha1.EngineClass) (*corev1ac.PodTemplateSpecApplyConfiguration, error) {
		// As the reconciler plans a pool, from the class's defaults.
		plan := render.PoolPlanOf(&crewletaiv1alpha1.SeatPool{Name: poolGeneral}, class.Spec.SeatPools.Defaults)
		return seatPodTemplate(company, class, &seatWorkload{name: "nimbus-seats-general", pool: poolGeneral, plan: &plan},
			"nimbus-seats-general-bootstrap", engineImageV020, &render.BootstrapDocument{}, "")
	}
	for _, c := range []struct {
		name     string
		template func(*crewletaiv1alpha1.EngineClass) (*corev1ac.PodTemplateSpecApplyConfiguration, error)
		class    *crewletaiv1alpha1.EngineClass
		want     string
	}{
		{"a data pod of a class that sets no limit", dataPod, classWith("", ""), render.DefaultSandboxSizeLimit},
		{"a data pod of a class that sets one for seat pods alone", dataPod, classWith("", "8Gi"), render.DefaultSandboxSizeLimit},
		{"a data pod of a class that sets its limit", dataPod, classWith("50Gi", "8Gi"), "50Gi"},
		{"a seat pod of a class that sets no limit", seatPod, classWith("", ""), render.DefaultSandboxSizeLimit},
		{"a seat pod of a class that sets one for data pods alone", seatPod, classWith("50Gi", ""), render.DefaultSandboxSizeLimit},
		{"a seat pod of a class that sets its limit", seatPod, classWith("50Gi", "8Gi"), "8Gi"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			applied, err := c.template(c.class)
			if err != nil {
				t.Fatal(err)
			}
			// The apply configuration and the type share their JSON form.
			raw, err := json.Marshal(applied)
			if err != nil {
				t.Fatal(err)
			}
			var pod corev1.PodTemplateSpec
			if err := json.Unmarshal(raw, &pod); err != nil {
				t.Fatal(err)
			}

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

// within reports whether path is dir or under it.
func within(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, strings.TrimSuffix(dir, "/")+"/")
}
