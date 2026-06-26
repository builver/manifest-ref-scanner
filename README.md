# manifest-ref-scanner

A static scanner for Kubernetes GitOps repositories that extracts all OCI artifact references (container images, OCI Helm charts, Flux OCI sources) from YAML manifests without requiring a running cluster.

## Features

- Two-pass architecture: collects all resources first, then resolves reference chains
- Understands standard Kubernetes workload specs (`Pod`, `Deployment`, `DaemonSet`, `StatefulSet`, `Job`, `CronJob`)
- Understands Flux CRDs: `OCIRepository`, `HelmRelease`, `FluxInstance`
- Synthesizes implicit resources created at runtime (e.g. `FluxInstance.spec.sync` → `OCIRepository/flux-system`)
- Expands `ResourceSet` inline templates per input entry
- Follows reference chains (`Kustomization.sourceRef` → `OCIRepository`, `HelmRelease.chartRef` → `OCIRepository`)
- Extensible via a config file for custom CRDs (same concept as kustomize `nameReference.yaml`)
- Outputs YAML with a full resolution chain per artifact showing how each reference was discovered

## Usage

```bash
# Scan a repository with defaults
manifest-ref-scanner ./my-gitops-repo

# Write output to a file
manifest-ref-scanner ./my-gitops-repo -o artifacts.yaml

# Extend built-in rules with custom CRD config
manifest-ref-scanner ./my-gitops-repo -c my-refs.yaml

# Exclude directories by glob (repeatable)
manifest-ref-scanner ./my-gitops-repo -e vendor -e testdata

# Override the default namespace fallback (default: "default")
manifest-ref-scanner ./my-gitops-repo --default-namespace flux-system
```

## Config file format

The `-c` config file is merged on top of the built-in rules. All sections are optional.

```yaml
fieldTypes:
  - name: containerImage
    targets:
      # CRD with image split across two fields
      - group: apps.example.io
        kind: MyApp
        namePath: spec/image/repository
        tagPaths:
          - spec/image/tag

synthesizers:
  # Declare that MyOperator implicitly creates an OCIRepository named "my-source"
  - name: myOperatorSync
    fromGroup: example.io
    fromKind: MyOperator
    emits:
      apiVersion: source.toolkit.fluxcd.io/v1
      kind: OCIRepository
      metadata:
        name: my-source
        namespace: "{{.metadata.namespace}}"
      spec:
        url: "{{.spec.sync.url}}"
        ref:
          tag: "{{.spec.sync.ref}}"

resolvers:
  - name: myAppSourceRef
    fromGroup: apps.example.io
    fromKind: MyApp
    path: spec/sourceRef
    resolves:
      kind: "{{.kind}}"
      name: "{{.name}}"
      namespace: "{{.namespace}}"

inlineExpanders:
  - fromGroup: example.io
    fromKind: MySet
    resourcesPath: spec/resources
    inputsPath: spec/inputs
    templateDelimLeft: "<<"
    templateDelimRight: ">>"
```

## Output format

```yaml
artifacts:
  - fieldType: ociArtifact
    raw: oci://ghcr.io/my-org/my-repo:latest
    registry: ghcr.io
    repository: my-org/my-repo
    tag: latest
    resolution:
      - kind: Kustomization
        name: components
        namespace: flux-system
        file: clusters/staging/components.yaml
        via: spec/sourceRef
      - kind: OCIRepository
        name: flux-system
        namespace: flux-system
        synthesized: true   # was not in any YAML file
```

---

## Known Limitations / Backlog

The following drawbacks are known and tracked for future work:

### Static analysis limitations

- **Kustomize config files cause duplicate resource warnings.**
  Files named `kustomization.yaml` with `apiVersion: kustomize.config.k8s.io/v1beta1` have `kind: Kustomization` but no `metadata.name`. They collide in the registry and produce noisy `warn: duplicate resource kustomization//` messages. Proper fix requires either filtering this group entirely or adding full `kustomize build` support so overlays are resolved before scanning.

- **No `kustomize build` pre-step.**
  Kustomize overlays (patches, variable substitutions, image transformations via `images:`) are not resolved. The scanner reads raw YAML as-is. Images injected or modified by Kustomize overlays will not appear in the output unless they exist verbatim in a base manifest.

- **`${VARIABLE}` style substitutions in `postBuild.substituteFrom` are not resolved.**
  Flux's `postBuild` variable substitution (e.g. `path: ./apps/${ENVIRONMENT}`) cannot be resolved statically without knowing the `ConfigMap`/`Secret` values at scan time. Affected paths are left with the literal variable placeholder.

- **Helm chart templates are not scanned.**
  Directories containing a `Chart.yaml` are skipped entirely with a warning. Helm's Go template syntax (`{{ }}`) is not valid YAML and cannot be parsed statically. Images referenced only inside Helm chart templates will not appear in the output. Use `helm template` to render charts first, then scan the rendered output.

- **`--exclude` globs use `filepath.Match` semantics (no `**` support).**
  Double-star patterns like `**/templates` are not supported. Match against directory base names (`templates`) or exact relative paths (`artifact/podinfo/podinfo`).

### OCI reference handling

- **Semver constraints produce a non-standard `raw` value.**
  When an `OCIRepository` uses `spec.ref.semver` (e.g. `>=1.0.0` or `*`), the scanner appends the constraint to the URL as if it were a tag (e.g. `oci://ghcr.io/example/chart:>=1.0.0`). This string is not a valid OCI reference. The `Ref` map field on `Artifact` exists for storing such qualifiers but is not yet populated for semver constraints.

- **The `Ref` field is currently unused.**
  The `Artifact.Ref map[string]string` field is intended for non-tag qualifiers (semver, digest pinning) but is not populated by the current resolver.

### Namespace resolution

- **Default namespace fallback is a best-effort heuristic.**
  When a resource reference omits the namespace field entirely, the scanner falls back to `--default-namespace` (default: `"default"`). If the referenced resource lives in a different namespace and that namespace is not inferrable from the surrounding manifest, the reference will appear as unresolved in the output.

### Output

- **Only YAML output is supported.**
  There is no JSON output mode and no Go template output for custom formats (e.g. to generate an OCM component descriptor, a Renovate config, or a flat image list). This was planned in the original architecture but not yet implemented.

- **No deduplication across field types.**
  The same image URL appearing as both a `containerImage` (in a `Deployment`) and an `ociArtifact` (in an `OCIRepository`) will produce two separate entries. Deduplication is per `(raw, fieldType)` pair only.

### Missing resource type coverage

- **`ImagePolicy` and `ImageUpdateAutomation` are not covered.**
  Flux's image automation CRDs reference images but are not included in the built-in field types.

- **`HelmChart` resources are not covered.**
  The `HelmChart` CRD (used when `HelmRelease.spec.chart` points to a `HelmRepository`) has its own `spec.chart` and `spec.version` fields which are not currently extracted.


### Issues
 - test against argocd repo
