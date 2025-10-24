# k8s-checksum-injector

`k8s-checksum-injector` adds deterministic checksums to Kubernetes Deployments so pods restart automatically when referenced ConfigMaps or Secrets change. The CLI reads manifests from stdin and writes the updated YAML to stdout, making it easy to drop into GitOps or CI pipelines.

## Features
- Injects checksum labels or annotations with `--mode label` (default) or `--mode annotation`
- Tracks ConfigMap and Secret usage in `envFrom`, `env.valueFrom`, and volume definitions
- Maintains existing comments, formatting, and original YAML document order
- Works with multi-document YAML streams and leaves unrelated resources untouched
- Purpose-built for Argo CD Config Management Plugins and other GitOps automation

## Installation

```bash
go install github.com/komailo/k8s-checksum-injector@latest
```

## Usage

Pipe manifests into the tool and capture the output:

```bash
cat manifests.yaml | k8s-checksum-injector --mode label > output.yaml
cat manifests.yaml | k8s-checksum-injector --mode annotation > output.yaml
```

The tool only mutates Deployments that reference ConfigMaps or Secrets present in the same input stream. Other documents pass through unchanged.

## Example

The `example/` directory shows a full input/output pair:

```bash
cat example/input.yaml | k8s-checksum-injector > example/output.yaml
```

After injection, checksum keys such as `checksum/configmap-app-config` appear on the Pod template metadata, ensuring Kubernetes rolls out changes whenever the underlying ConfigMap or Secret contents change.
