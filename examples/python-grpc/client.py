"""End-to-end smoke test client for the VirtualMachineService.

Creates a VM, gets it back, lists, updates it, lists again, deletes, and
confirms it's gone. Prints what each RPC returned.
"""

from __future__ import annotations

import os
import sys

import grpc

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "generated"))

from proto import kubevirt_service_pb2 as svc
from proto import kubevirt_service_pb2_grpc as svc_grpc
from kubevirt.io.api.core.v1 import generated_pb2 as kubevirt_pb
from k8s.io.apimachinery.pkg.apis.meta.v1 import generated_pb2 as metav1_pb


def make_vm(namespace: str, name: str) -> kubevirt_pb.VirtualMachine:
    vm = kubevirt_pb.VirtualMachine()
    vm.metadata.namespace = namespace
    vm.metadata.name = name
    # Minimal spec — kubevirt validates a lot, but we're not running a real
    # virt controller, just round-tripping bytes.
    vm.spec.runStrategy = "Halted"
    return vm


def main():
    channel = grpc.insecure_channel("localhost:50051")
    stub = svc_grpc.VirtualMachineServiceStub(channel)

    # Create
    created = stub.Create(make_vm("default", "demo-vm"))
    print(f"Create -> {created.metadata.namespace}/{created.metadata.name}")

    # Get
    got = stub.Get(svc.GetVMRequest(namespace="default", name="demo-vm"))
    print(f"Get    -> runStrategy={got.spec.runStrategy}")

    # Update
    got.spec.runStrategy = "Always"
    updated = stub.Update(got)
    print(f"Update -> runStrategy={updated.spec.runStrategy}")

    # List
    listed = stub.List(svc.ListVMRequest(namespace="default"))
    print(f"List   -> {len(listed.items)} item(s)")

    # Delete
    stub.Delete(svc.DeleteVMRequest(namespace="default", name="demo-vm"))
    print("Delete -> ok")

    # Verify gone
    try:
        stub.Get(svc.GetVMRequest(namespace="default", name="demo-vm"))
        print("ERROR: expected NOT_FOUND after delete")
    except grpc.RpcError as e:
        print(f"Get    -> {e.code().name} (expected)")


if __name__ == "__main__":
    main()
