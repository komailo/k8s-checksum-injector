package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	yaml "gopkg.in/yaml.v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	sigyaml "sigs.k8s.io/yaml"
)

// Mode defines whether to inject as labels or annotations
type Mode string

const (
	ModeLabel      Mode = "label"
	ModeAnnotation Mode = "annotation"
)

func main() {
	var modeStr string
	flag.StringVar(&modeStr, "mode", "label", "inject checksums as 'label' or 'annotation'")
	flag.Parse()

	mode := Mode(modeStr)
	if mode != ModeLabel && mode != ModeAnnotation {
		fmt.Fprintf(os.Stderr, "invalid mode: %s (must be 'label' or 'annotation')\n", mode)
		os.Exit(1)
	}

	decoder := yaml.NewDecoder(os.Stdin)
	var docs []*yaml.Node

	for {
		doc := &yaml.Node{}
		err := decoder.Decode(doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to parse YAML: %v\n", err)
			os.Exit(1)
		}
		if isEmptyDocument(doc) {
			continue
		}
		docs = append(docs, doc)
	}

	var configMaps []*corev1.ConfigMap
	var secrets []*corev1.Secret
	var deployments []deploymentDoc

	for _, doc := range docs {
		switch getKind(doc) {
		case "ConfigMap":
			cm := &corev1.ConfigMap{}
			if err := decodeDocument(doc, cm); err == nil {
				configMaps = append(configMaps, cm)
			}
		case "Secret":
			s := &corev1.Secret{}
			if err := decodeDocument(doc, s); err == nil {
				secrets = append(secrets, s)
			}
		case "Deployment":
			dep := &appsv1.Deployment{}
			if err := decodeDocument(doc, dep); err == nil {
				deployments = append(deployments, deploymentDoc{node: doc, obj: dep})
			}
		}
	}

	cmHashes := make(map[string]string, len(configMaps))
	for _, cm := range configMaps {
		if cm.Name == "" {
			continue
		}
		cmHashes[cm.Name] = hashConfigMap(cm)
	}

	secretHashes := make(map[string]string, len(secrets))
	for _, s := range secrets {
		if s.Name == "" {
			continue
		}
		secretHashes[s.Name] = hashSecret(s)
	}

	for _, dep := range deployments {
		processDeploymentDoc(dep, cmHashes, secretHashes, mode)
	}

	encoder := yaml.NewEncoder(os.Stdout)
	encoder.SetIndent(2)
	for _, doc := range docs {
		if err := encoder.Encode(doc); err != nil {
			fmt.Fprintf(os.Stderr, "failed to render YAML: %v\n", err)
			os.Exit(1)
		}
	}
	if err := encoder.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to finalize YAML output: %v\n", err)
		os.Exit(1)
	}
}

func processDeploymentDoc(dep deploymentDoc, cmHashes, secretHashes map[string]string, mode Mode) {
	cmRefs, secretRefs := referencedObjects(dep.obj)

	type pair struct {
		key   string
		value string
	}

	var updates []pair

	for _, name := range cmRefs {
		if sum, ok := cmHashes[name]; ok {
			updates = append(updates, pair{
				key:   fmt.Sprintf("checksum/configmap-%s", sanitizeKey(name)),
				value: sum,
			})
		}
	}

	for _, name := range secretRefs {
		if sum, ok := secretHashes[name]; ok {
			updates = append(updates, pair{
				key:   fmt.Sprintf("checksum/secret-%s", sanitizeKey(name)),
				value: sum,
			})
		}
	}

	if len(updates) == 0 {
		return
	}

	root := documentRoot(dep.node)
	if root == nil {
		return
	}

	var target *yaml.Node
	switch mode {
	case ModeLabel:
		target = ensureMap(root, "spec", "template", "metadata", "labels")
	case ModeAnnotation:
		target = ensureMap(root, "spec", "template", "metadata", "annotations")
	default:
		return
	}
	if target == nil {
		return
	}

	for _, update := range updates {
		setStringMapValue(target, update.key, update.value)
	}
}

type deploymentDoc struct {
	node *yaml.Node
	obj  *appsv1.Deployment
}

func decodeDocument(doc *yaml.Node, out interface{}) error {
	root := documentRoot(doc)
	if root == nil {
		return fmt.Errorf("empty document")
	}
	data, err := yaml.Marshal(root)
	if err != nil {
		return err
	}
	return sigyaml.Unmarshal(data, out)
}

func documentRoot(doc *yaml.Node) *yaml.Node {
	if doc == nil {
		return nil
	}
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return nil
		}
		return doc.Content[0]
	}
	return doc
}

func getKind(doc *yaml.Node) string {
	root := documentRoot(doc)
	if root == nil || root.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i < len(root.Content)-1; i += 2 {
		key := root.Content[i]
		if key.Kind == yaml.ScalarNode && key.Value == "kind" {
			return root.Content[i+1].Value
		}
	}
	return ""
}

func ensureMap(node *yaml.Node, path ...string) *yaml.Node {
	current := node
	if current == nil || current.Kind != yaml.MappingNode {
		return nil
	}
	for _, key := range path {
		var next *yaml.Node
		for i := 0; i < len(current.Content)-1; i += 2 {
			if current.Content[i].Value == key {
				next = current.Content[i+1]
				break
			}
		}
		if next == nil {
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
			valueNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
			current.Content = append(current.Content, keyNode, valueNode)
			next = valueNode
		} else if next.Kind != yaml.MappingNode {
			next.Kind = yaml.MappingNode
			next.Tag = "!!map"
			next.Value = ""
			next.Content = nil
		}
		current = next
	}
	return current
}

func setStringMapValue(mapNode *yaml.Node, key, value string) {
	for i := 0; i < len(mapNode.Content)-1; i += 2 {
		if mapNode.Content[i].Value == key {
			mapNode.Content[i+1].Kind = yaml.ScalarNode
			mapNode.Content[i+1].Tag = "!!str"
			mapNode.Content[i+1].Style = 0
			mapNode.Content[i+1].Value = value
			return
		}
	}
	keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
	valueNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
	mapNode.Content = append(mapNode.Content, keyNode, valueNode)
}

func isEmptyDocument(doc *yaml.Node) bool {
	if doc == nil {
		return true
	}
	if doc.Kind != yaml.DocumentNode {
		return false
	}
	return len(doc.Content) == 0
}

func referencedObjects(dep *appsv1.Deployment) (configMaps, secrets []string) {
	cmSet := map[string]bool{}
	secretSet := map[string]bool{}

	for _, v := range dep.Spec.Template.Spec.Volumes {
		if v.ConfigMap != nil {
			cmSet[v.ConfigMap.Name] = true
		}
		if v.Secret != nil {
			secretSet[v.Secret.SecretName] = true
		}
	}

	for _, c := range dep.Spec.Template.Spec.Containers {
		for _, e := range c.EnvFrom {
			if e.ConfigMapRef != nil {
				cmSet[e.ConfigMapRef.Name] = true
			}
			if e.SecretRef != nil {
				secretSet[e.SecretRef.Name] = true
			}
		}
		for _, e := range c.Env {
			if e.ValueFrom != nil {
				if e.ValueFrom.ConfigMapKeyRef != nil {
					cmSet[e.ValueFrom.ConfigMapKeyRef.Name] = true
				}
				if e.ValueFrom.SecretKeyRef != nil {
					secretSet[e.ValueFrom.SecretKeyRef.Name] = true
				}
			}
		}
	}

	for k := range cmSet {
		if k != "" {
			configMaps = append(configMaps, k)
		}
	}
	for k := range secretSet {
		if k != "" {
			secrets = append(secrets, k)
		}
	}
	sort.Strings(configMaps)
	sort.Strings(secrets)
	return
}

func hashConfigMap(cm *corev1.ConfigMap) string {
	h := sha256.New()
	keys := make([]string, 0, len(cm.Data))
	for k := range cm.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte(cm.Data[k]))
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

func hashSecret(s *corev1.Secret) string {
	h := sha256.New()
	keys := make([]string, 0, len(s.Data))
	for k := range s.Data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		h.Write([]byte(k))
		h.Write(s.Data[k])
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

func sanitizeKey(name string) string {
	return strings.ReplaceAll(name, ".", "-")
}
