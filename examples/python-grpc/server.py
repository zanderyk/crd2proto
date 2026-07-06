"""In-memory gRPC server for kubevirt VirtualMachine CRUD.

Backs all five RPCs with a `dict[(namespace, name)] -> VirtualMachine`. Not
production code — just demonstrates that the .proto crd2proto generated for
kubevirt is wire-compatible across languages.
"""

from __future__ import annotations

import os
import sys
from concurrent import futures
from threading import Lock

import grpc

# Generated python lives under generated/; make it importable.
sys.path.insert(0, os.path.join(os.path.dirname(__file__), "generated"))

from proto import kubevirt_service_pb2 as svc
from proto import kubevirt_service_pb2_grpc as svc_grpc
from kubevirt.io.api.core.v1 import generated_pb2 as kubevirt_pb


class VirtualMachineService(svc_grpc.VirtualMachineServiceServicer):
    def __init__(self):
        self._lock = Lock()
        self._store: dict[tuple[str, str], kubevirt_pb.VirtualMachine] = {}

    @staticmethod
    def _key(namespace: str, name: str) -> tuple[str, str]:
        return (namespace or "default", name)

    def Get(self, request: svc.GetVMRequest, context):
        key = self._key(request.namespace, request.name)
        with self._lock:
            vm = self._store.get(key)
        if vm is None:
            context.abort(grpc.StatusCode.NOT_FOUND, f"vm {key[0]}/{key[1]} not found")
        return vm

    def List(self, request: svc.ListVMRequest, context):
        with self._lock:
            if request.namespace:
                items = [v for (ns, _), v in self._store.items() if ns == request.namespace]
            else:
                items = list(self._store.values())
        return svc.ListVMResponse(items=items)

    def Create(self, request: kubevirt_pb.VirtualMachine, context):
        ns = request.metadata.namespace or "default"
        name = request.metadata.name
        if not name:
            context.abort(grpc.StatusCode.INVALID_ARGUMENT, "metadata.name is required")
        key = self._key(ns, name)
        with self._lock:
            if key in self._store:
                context.abort(grpc.StatusCode.ALREADY_EXISTS, f"vm {ns}/{name} already exists")
            self._store[key] = request
        return request

    def Update(self, request: kubevirt_pb.VirtualMachine, context):
        ns = request.metadata.namespace or "default"
        name = request.metadata.name
        key = self._key(ns, name)
        with self._lock:
            if key not in self._store:
                context.abort(grpc.StatusCode.NOT_FOUND, f"vm {ns}/{name} not found")
            self._store[key] = request
        return request

    def Delete(self, request: svc.DeleteVMRequest, context):
        key = self._key(request.namespace, request.name)
        with self._lock:
            if key not in self._store:
                context.abort(grpc.StatusCode.NOT_FOUND, f"vm {key[0]}/{key[1]} not found")
            del self._store[key]
        return svc.DeleteVMResponse()


def main(port: int = 50051) -> None:
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=10))
    svc_grpc.add_VirtualMachineServiceServicer_to_server(VirtualMachineService(), server)

    # Enable server reflection so grpcurl / Postman / etc. can discover the
    # service and message schemas at runtime — no .proto files needed by clients.
    from grpc_reflection.v1alpha import reflection
    service_names = (
        svc.DESCRIPTOR.services_by_name["VirtualMachineService"].full_name,
        reflection.SERVICE_NAME,
    )
    reflection.enable_server_reflection(service_names, server)

    server.add_insecure_port(f"[::]:{port}")
    server.start()
    print(f"VirtualMachineService listening on :{port} (reflection enabled)")
    server.wait_for_termination()


if __name__ == "__main__":
    main()
