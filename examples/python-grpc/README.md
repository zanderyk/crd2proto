# Python gRPC server for kubevirt VirtualMachine CRUD

A minimal demo that the `.proto` files crd2proto generated for kubevirt are
wire-compatible across languages. Implements `Get`/`List`/`Create`/`Update`/
`Delete` for `kubevirt.io.api.core.v1.VirtualMachine`, backed by an
in-memory dict.

## Setup

```bash
cd examples/python-grpc
python -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt

# Generate Python stubs from kubevirt + apimachinery + k8s.io/api .protos.
# This depends on `make run-kubevirt` having been run first (so the
# kubevirt generated.proto exists).
./compile.sh
```

## Run

In one terminal:
```bash
python server.py
```

In another:
```bash
python client.py
```
