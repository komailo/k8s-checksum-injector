package injector

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestReferencedObjects(t *testing.T) {
	dep := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "cfg",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "vol-cm"}},
							},
						},
						{
							Name: "creds",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{SecretName: "vol-secret"},
							},
						},
					},
					Containers: []corev1.Container{
						{
							EnvFrom: []corev1.EnvFromSource{
								{ConfigMapRef: &corev1.ConfigMapEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "env-cm"}}},
								{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "env-secret"}}},
							},
							Env: []corev1.EnvVar{
								{
									Name: "FROM_CONFIG",
									ValueFrom: &corev1.EnvVarSource{
										ConfigMapKeyRef: &corev1.ConfigMapKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "key-cm"}},
									},
								},
								{
									Name: "FROM_SECRET",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "key-secret"}},
									},
								},
								{
									Name:  "NO_REF",
									Value: "literal",
								},
							},
						},
					},
				},
			},
		},
	}

	gotCMs, gotSecrets := referencedObjects(dep)

	wantCMs := []string{"env-cm", "key-cm", "vol-cm"}
	wantSecrets := []string{"env-secret", "key-secret", "vol-secret"}

	if !reflect.DeepEqual(gotCMs, wantCMs) {
		t.Fatalf("configmap refs mismatch\nwant: %v\ngot:  %v", wantCMs, gotCMs)
	}
	if !reflect.DeepEqual(gotSecrets, wantSecrets) {
		t.Fatalf("secret refs mismatch\nwant: %v\ngot:  %v", wantSecrets, gotSecrets)
	}
}

func TestHashConfigMapAndSecretDeterministic(t *testing.T) {
	cm1 := &corev1.ConfigMap{Data: map[string]string{"b": "two", "a": "one"}}
	cm2 := &corev1.ConfigMap{Data: map[string]string{"a": "one", "b": "two"}}

	if got, want := hashConfigMap(cm1), hashConfigMap(cm2); got != want {
		t.Fatalf("expected hashConfigMap to ignore key order\nwant: %s\ngot:  %s", want, got)
	}

	cm3 := &corev1.ConfigMap{Data: map[string]string{"a": "changed"}}
	if got, want := hashConfigMap(cm1), hashConfigMap(cm3); got == want {
		t.Fatalf("expected different data to produce different hashes, got %s", got)
	}

	s1 := &corev1.Secret{Data: map[string][]byte{"y": []byte("beta"), "x": []byte("alpha")}}
	s2 := &corev1.Secret{Data: map[string][]byte{"x": []byte("alpha"), "y": []byte("beta")}}
	if got, want := hashSecret(s1), hashSecret(s2); got != want {
		t.Fatalf("expected hashSecret to ignore key order\nwant: %s\ngot:  %s", want, got)
	}
}

func TestProcessDeploymentDocModes(t *testing.T) {
	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo
spec:
  template:
    metadata:
      labels:
        app: demo
    spec:
      volumes:
        - name: cfg
          configMap:
            name: app.config
        - name: creds
          secret:
            secretName: top.secret
      containers:
        - name: app
          envFrom:
            - configMapRef:
                name: shared-config
          env:
            - name: DATA
              valueFrom:
                secretKeyRef:
                  name: top.secret
                  key: password
`

	doc, dep := decodeDeploymentManifest(t, manifest)

	cmHashes := map[string]string{
		"app.config":    "111111111111",
		"shared-config": "222222222222",
	}
	secretHashes := map[string]string{
		"top.secret": "333333333333",
	}

	processDeploymentDoc(deploymentDoc{node: doc, obj: dep}, cmHashes, secretHashes, ModeLabel)

	updated := &appsv1.Deployment{}
	if err := decodeDocument(doc, updated); err != nil {
		t.Fatalf("decodeDocument: %v", err)
	}

	labels := updated.Spec.Template.Labels
	if labels["app"] != "demo" {
		t.Fatalf("expected existing label to persist, got %v", labels)
	}

	cases := map[string]string{
		"checksum/configmap-app-config":    "111111111111",
		"checksum/configmap-shared-config": "222222222222",
		"checksum/secret-top-secret":       "333333333333",
	}
	for key, want := range cases {
		if got := labels[key]; got != want {
			t.Fatalf("expected label %s=%s, got %s", key, want, got)
		}
	}

	if ann := updated.Spec.Template.Annotations; len(ann) != 0 {
		t.Fatalf("expected no annotations in label mode, got %v", ann)
	}

	// Re-decode a fresh document for annotation mode to avoid cumulative mutations.
	docAnn, depAnn := decodeDeploymentManifest(t, manifest)
	processDeploymentDoc(deploymentDoc{node: docAnn, obj: depAnn}, cmHashes, secretHashes, ModeAnnotation)

	annotated := &appsv1.Deployment{}
	if err := decodeDocument(docAnn, annotated); err != nil {
		t.Fatalf("decodeDocument: %v", err)
	}

	ann := annotated.Spec.Template.Annotations
	if len(ann) == 0 {
		t.Fatalf("expected annotations to be created, none found")
	}
	for key, want := range cases {
		if got := ann[key]; got != want {
			t.Fatalf("expected annotation %s=%s, got %s", key, want, got)
		}
	}
	if labels := annotated.Spec.Template.Labels; labels["app"] != "demo" {
		t.Fatalf("expected original labels untouched in annotation mode, got %v", labels)
	}
}

func TestProcessDeploymentDocWithoutMatches(t *testing.T) {
	manifest := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo
spec:
  template:
    metadata: {}
    spec:
      containers:
        - name: app
          image: demo:latest
`
	doc, dep := decodeDeploymentManifest(t, manifest)

	processDeploymentDoc(deploymentDoc{node: doc, obj: dep}, map[string]string{}, map[string]string{}, ModeLabel)

	updated := &appsv1.Deployment{}
	if err := decodeDocument(doc, updated); err != nil {
		t.Fatalf("decodeDocument: %v", err)
	}

	if labels := updated.Spec.Template.Labels; len(labels) != 0 {
		t.Fatalf("expected no labels to be injected, got %v", labels)
	}
}

func TestSanitizeKey(t *testing.T) {
	if got, want := sanitizeKey("a.b.c"), "a-b-c"; got != want {
		t.Fatalf("sanitizeKey mismatch: want %q, got %q", want, got)
	}
	if got := sanitizeKey("no-dots"); got != "no-dots" {
		t.Fatalf("sanitizeKey should leave hyphens intact, got %q", got)
	}
}

func TestInjectChecksums(t *testing.T) {
	input := `apiVersion: v1
kind: ConfigMap
metadata:
  name: app.config
---
apiVersion: v1
kind: Secret
metadata:
  name: top.secret
stringData:
  password: s3cr3t
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: demo
spec:
  template:
    metadata:
      labels:
        app: demo
    spec:
      containers:
        - name: app
          image: demo:latest
          envFrom:
            - configMapRef:
                name: app.config
          env:
            - name: PASSWORD
              valueFrom:
                secretKeyRef:
                  name: top.secret
                  key: password
`

	got, err := InjectChecksums(input, ModeAnnotation)
	if err != nil {
		t.Fatalf("InjectChecksums: %v", err)
	}

	if !strings.Contains(got, "checksum/configmap-app-config") {
		t.Fatalf("expected configmap checksum in output, got:\n%s", got)
	}
	if !strings.Contains(got, "checksum/secret-top-secret") {
		t.Fatalf("expected secret checksum in output, got:\n%s", got)
	}
}

func decodeDeploymentManifest(t *testing.T, manifest string) (*yaml.Node, *appsv1.Deployment) {
	t.Helper()
	decoder := yaml.NewDecoder(strings.NewReader(manifest))
	doc := &yaml.Node{}
	if err := decoder.Decode(doc); err != nil {
		t.Fatalf("failed to decode YAML: %v", err)
	}
	dep := &appsv1.Deployment{}
	if err := decodeDocument(doc, dep); err != nil {
		t.Fatalf("decodeDocument: %v", err)
	}
	return doc, dep
}
