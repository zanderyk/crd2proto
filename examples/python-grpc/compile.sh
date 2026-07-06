#!/usr/bin/env bash
# Compiles all .proto files needed by the kubevirt VM service into Python
# modules under generated/. The big trick is that kubevirt's generated.proto
# imports k8s.io/api/core/v1 and k8s.io/apimachinery/...; protoc resolves
# imports by literal path, so we stage a temp symlink farm that makes the
# k8s.io/* and kubevirt.io/* import paths resolve.
set -euo pipefail

cd "$(dirname "$0")"

# Resolve module dirs against the kubevirt submodule's go.mod so we pick up
# whatever versions of k8s.io/api and k8s.io/apimachinery it pins.
pushd ../../testdata/crds/kubevirt-api >/dev/null
KUBEVIRT_DIR=$(pwd)
APIMACHINERY_DIR=$(go list -m -f '{{.Dir}}' k8s.io/apimachinery)
API_DIR=$(go list -m -f '{{.Dir}}' k8s.io/api)
popd >/dev/null

CDI_DIR=$(realpath ../../testdata/crds/cdi-api)

PROTO_ROOT=$(mktemp -d)
trap "rm -rf $PROTO_ROOT" EXIT
mkdir -p "$PROTO_ROOT/k8s.io" "$PROTO_ROOT/kubevirt.io"
ln -s "$API_DIR"          "$PROTO_ROOT/k8s.io/api"
ln -s "$APIMACHINERY_DIR" "$PROTO_ROOT/k8s.io/apimachinery"
ln -s "$KUBEVIRT_DIR"     "$PROTO_ROOT/kubevirt.io/api"
ln -s "$CDI_DIR"          "$PROTO_ROOT/kubevirt.io/containerized-data-importer-api"

rm -rf generated
mkdir -p generated

# All .proto files we need to compile: our service + every transitive
# generated.proto. We just compile every generated.proto in the symlink farm
# — overcompiling is harmless and avoids hand-listing the dep graph.
PROTOS=()
PROTOS+=("proto/kubevirt_service.proto")
while IFS= read -r f; do PROTOS+=("$f"); done < <(
  find -L "$PROTO_ROOT" -name 'generated.proto' \
    | sed "s|^$PROTO_ROOT/||"
)

# Compile from the symlink farm so import paths like
# `k8s.io/api/core/v1/generated.proto` resolve.
cd "$PROTO_ROOT"
ln -s "$OLDPWD/proto" proto

python -m grpc_tools.protoc \
  -I . \
  --python_out="$OLDPWD/generated" \
  --grpc_python_out="$OLDPWD/generated" \
  "${PROTOS[@]}"

cd "$OLDPWD"

# Make every directory in generated/ a Python package so cross-module
# imports work.
find generated -type d -exec touch {}/__init__.py \;

echo "Generated Python sources under examples/python-grpc/generated/"
