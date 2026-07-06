# crd2proto

Generate `.proto` from kubebuilder-style CRD Go types. A thin wrapper around
`go-to-protobuf` with no `protoc`, gogo, or import-path setup.

## Install

```sh
go install github.com/zanderyk/crd2proto/cmd/crd2proto@latest
```

## Usage

```sh
cd path/to/module
crd2proto generate <go-import-path>[,<go-import-path>...]
```

Walks the package's Go types, injects `protobuf:` struct tags into the `.go`
files, and writes `generated.proto` into each package's directory. apimachinery
and `k8s.io/api/*` are imported, not regenerated.

```sh
cd examples/guestbook/guestbook-crd
crd2proto generate my.domain/guestbook/api/v1   # -> api/v1/generated.proto
```

## Flags

| Flag | Default | |
|------|---------|--|
| `--output-dir` | `.` | Base dir for output. |
| `--go-header-file` | — | Header prepended to each `.proto`. |
| `--apimachinery-packages` | apimachinery + `k8s.io/api/*` | External packages. Prefix with `-` to import instead of inline. |
| `--drop-embedded-fields` | `...meta/v1.TypeMeta` | Go types to omit. |

## Note

- Generation **rewrites your `.go` files** (adds tags).
- Two inlined types with the same short name (e.g. kubevirt vs CDI
  `DataVolumeSource`; see [guestbook-example](./examples/guestbook/guestbook-crd/api/v1/guestbook_types.go)) are disambiguated by import-path prefix
  (`kubevirt_io_api_core_v1__DataVolumeSource`).
- No `protoc` is run. Validate with the `Makefile` `run-*` targets.

## License

Apache 2.0.
