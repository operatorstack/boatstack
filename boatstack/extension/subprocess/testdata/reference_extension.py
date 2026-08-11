#!/usr/bin/python3
import json
import os
import sys

request = json.load(sys.stdin)
operation = request["operation"]
response = {
    "protocol_version": 1,
    "operation": operation,
    "extension_id": request["extension_id"],
    "extension_version": request["extension_version"],
    "correlation_id": request["correlation_id"],
}

if operation == "manifest":
    response["manifest"] = {
        "id": "fixture.echo",
        "version": "1.0.0",
        "protocol_version": 1,
        "settings_schema": {"type": "object"},
        "facts": ["fixture.echo.present"],
        "privacy_classification": "metadata-only",
        "telemetry_classification": "transition-receipt",
    }
elif operation == "observe":
    value = "leaked" if "BOATSTACK_TEST_SECRET" in os.environ else "clean"
    response["facts"] = [{
        "id": "fixture.echo.present",
        "status": "known",
        "value": value,
        "fingerprint": "fixture-observation",
    }]
elif operation == "verify":
    response["verified"] = True
elif operation in ("plan-local-effect", "recover"):
    response["writes"] = []
elif operation == "execute-external":
    response["external_result"] = {"settlement": "settled"}
else:
    response["error_class"] = "unsupported-operation"
    response["error"] = "unsupported"

json.dump(response, sys.stdout, separators=(",", ":"))
