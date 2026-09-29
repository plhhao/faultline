#!/usr/bin/env python3
"""Opt-in live S3 acceptance through the real Faultline CLI and Docker DNS route."""

import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time
from urllib.parse import urlsplit
import uuid

HERE = Path(__file__).resolve().parent
ENV = HERE / ".env"
COMPOSE = HERE / "compose.yaml"
SOCKET = "/tmp/faultline/admin.sock"


def normalize_test_host(value):
    if "://" in value:
        parsed = urlsplit(value)
        try:
            port = parsed.port
        except ValueError as error:
            raise RuntimeError("S3_TEST_HOST URL has an invalid port") from error
        if (
            parsed.scheme != "https"
            or parsed.username is not None
            or parsed.password is not None
            or parsed.path not in ("", "/")
            or parsed.query
            or parsed.fragment
            or port not in (None, 443)
        ):
            raise RuntimeError("S3_TEST_HOST URL must contain only an HTTPS hostname and optional port 443")
        value = parsed.hostname or ""
    if not re.fullmatch(r"[A-Za-z0-9.-]+", value):
        raise RuntimeError("S3_TEST_HOST must be one hostname or HTTPS URL without a path")
    return value


def error_category(message):
    message = message.lower()
    categories = (
        ("access_denied", ("accessdenied", "status code: 403")),
        ("signature", ("signaturedoesnotmatch", "invalidaccesskeyid")),
        ("redirect", ("status code: 301", "status code: 307", "permanentredirect")),
        ("dns", ("no such host", "name resolution")),
        ("tls", ("x509", "certificate", "tls")),
        ("connection", ("connection refused", "connection reset", "broken pipe", "unexpected eof")),
        ("timeout", ("deadline exceeded", "timeout", "timed out")),
    )
    for category, markers in categories:
        if any(marker in message for marker in markers):
            return category
    return "sdk_error"


def error_summary(result):
    parts = []
    if result.get("http_status"):
        parts.append("HTTP " + str(result["http_status"]))
    if result.get("error_code"):
        parts.append("code " + result["error_code"])
    if not parts:
        parts.append(error_category(result.get("error", "")))
    return ", ".join(parts)


def load_env():
    if not ENV.is_file():
        print("SKIP: examples/s3/.env is absent")
        return None
    values = {}
    for line in ENV.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        name, value = line.split("=", 1)
        values[name.strip()] = value.strip().strip('"\'')
    if values.get("FAULTLINE_S3_TEST", "0") != "1":
        print("SKIP: FAULTLINE_S3_TEST is not 1")
        return None
    required = ["S3_REGION", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY", "S3_BUCKET_NAME", "S3_TEST_HOST", "S3_TEST_PREFIX"]
    missing = [name for name in required if not values.get(name)]
    if missing:
        raise RuntimeError("missing required variable names: " + ", ".join(missing))
    values["S3_TEST_HOST"] = normalize_test_host(values["S3_TEST_HOST"])
    if values.get("S3_ENDPOINT"):
        endpoint = urlsplit(values["S3_ENDPOINT"])
        if endpoint.hostname == values["S3_TEST_HOST"]:
            values["S3_TEST_HOST"] = normalize_test_host(values["S3_BUCKET_NAME"] + "." + values["S3_TEST_HOST"])
    if not values["S3_TEST_PREFIX"].endswith("/") or ".." in values["S3_TEST_PREFIX"]:
        raise RuntimeError("S3_TEST_PREFIX must be a dedicated prefix ending in /")
    return values


def main():
    values = load_env()
    if values is None:
        return 0
    project = "faultline-s3-" + uuid.uuid4().hex[:8]
    base = ["docker", "compose", "--project-name", project, "-f", str(COMPOSE), "--env-file", str(ENV)]
    compose_env = os.environ.copy()
    compose_env["S3_TEST_HOST"] = values["S3_TEST_HOST"]

    def compose(*args, check=True):
        result = subprocess.run([*base, *args], text=True, capture_output=True, env=compose_env)
        if check and result.returncode:
            raise RuntimeError("Docker Compose command failed: " + " ".join(args[:3]))
        return result

    def cli(operation, *args):
        result = compose("exec", "-T", "faultline", "/faultline", operation, *args, "--admin-socket", SOCKET)
        return json.loads(result.stdout)

    def fixture(service, op, key, size=65536, timeout="12s"):
        result = compose("run", "--rm", service, "-op", op, "-key", key, "-size", str(size), "-timeout", timeout, check=False)
        lines = [line for line in result.stdout.splitlines() if line.startswith("{")]
        if not lines:
            raise RuntimeError("SDK fixture returned no JSON for " + op)
        parsed = json.loads(lines[-1])
        if result.returncode and not parsed.get("error"):
            raise RuntimeError("SDK fixture process failed for " + op)
        return parsed

    def status():
        return cli("status")

    def reload_case(name):
        cli("reload", "--config", "/configs/" + name + ".yaml")
        current = status()
        return current["run_counters"]["applied"], current["info"]["config_revision"]

    def wait_applied(previous):
        for _ in range(30):
            if status()["run_counters"]["applied"] > previous:
                return
            time.sleep(0.1)
        raise RuntimeError("fault_applied counter did not advance")

    def key_for(name):
        return values["S3_TEST_PREFIX"] + uuid.uuid4().hex + "-" + name

    def finished_event(name, revision):
        for _ in range(30):
            logs = compose("logs", "--no-color", "--no-log-prefix", "faultline").stdout
            for line in reversed(logs.splitlines()):
                try:
                    event = json.loads(line)
                except json.JSONDecodeError:
                    continue
                if event.get("type") == "flow_finished" and event.get("rule_id") == name and event.get("config_revision") == revision:
                    return event
            time.sleep(0.1)
        raise RuntimeError("flow_finished event missing for " + name)

    created = []
    try:
        compose("build", "faultline", "sdk", "verifier")

        direct = key_for("direct-baseline")
        created.append(direct)
        direct_put = fixture("verifier", "put", direct)
        if direct_put.get("error"):
            raise RuntimeError("direct baseline PutObject failed: " + error_summary(direct_put))
        direct_get = fixture("verifier", "get", direct)
        if direct_get.get("error") or direct_get.get("sha256") != direct_put.get("sha256"):
            raise RuntimeError("direct baseline GetObject failed: " + error_summary(direct_get))
        print("PASS direct baseline: SDK TLS/SigV4 and object contents")

        compose("up", "-d", "config", "faultline")
        compose("exec", "-T", "faultline", "/faultline", "validate", "--config", "/configs/baseline.yaml")
        for _ in range(30):
            try:
                if status()["ready"]:
                    break
            except (RuntimeError, KeyError, json.JSONDecodeError):
                pass
            time.sleep(0.2)
        else:
            raise RuntimeError("Faultline listener did not become ready")

        baseline = key_for("baseline")
        created.append(baseline)
        before = status()["run_counters"]["total"]
        put = fixture("sdk", "put", baseline)
        if put.get("error"):
            raise RuntimeError("proxied baseline PutObject failed: " + error_summary(put))
        after = status()["run_counters"]["total"]
        if after <= before:
            raise RuntimeError("baseline did not traverse Faultline; check S3_TEST_HOST")
        got = fixture("verifier", "get", baseline)
        if got.get("error") or got.get("sha256") != put.get("sha256"):
            raise RuntimeError("independent baseline verification failed")
        if status()["run_counters"]["total"] != after:
            raise RuntimeError("verifier unexpectedly traversed Faultline")
        print("PASS baseline: SDK route, TLS/SigV4, independent object contents")

        cli("enable")
        cases = ["disconnect", "hold_response", "hold_upload", "slow_upload", "slow_response", "cut", "delay_connect"]
        for name in cases:
            previous, revision = reload_case(name)
            key = key_for(name)
            created.append(key)
            if name == "slow_response":
                seed = fixture("verifier", "put", key)
                if seed.get("error"):
                    raise RuntimeError("direct setup failed for " + name)
                result = fixture("sdk", "get", key)
                if result.get("sha256") != seed.get("sha256"):
                    raise RuntimeError("download content differs for " + name)
            else:
                payload_size = 8 << 20 if name == "hold_upload" else 65536
                deadline = "3s" if name.startswith("hold") else "12s"
                result = fixture("sdk", "put", key, size=payload_size, timeout=deadline)
            wait_applied(previous)
            event = finished_event(name, revision)
            if not event.get("selected") or not event.get("reached") or not event.get("applied"):
                raise RuntimeError("event does not confirm selected/reached/applied for " + name)
            up = event.get("client_to_upstream_bytes", 0)
            down = event.get("upstream_to_client_bytes", 0)
            if name == "cut" and up < 4096:
                raise RuntimeError("cut byte counter did not reach threshold")
            if (name == "slow_upload" and up == 0) or (name == "slow_response" and down == 0):
                raise RuntimeError("throttle direction had no forwarded bytes")
            if name in {"disconnect", "hold_response", "hold_upload", "cut"} and not result.get("error"):
                raise RuntimeError("SDK did not report an error for " + name)
            if name in {"slow_upload", "slow_response", "delay_connect"} and result.get("error"):
                raise RuntimeError("SDK operation failed for " + name)
            if name in {"slow_upload", "slow_response"} and result["elapsed_ms"] < 1200:
                raise RuntimeError("throttle did not measurably slow " + name)
            if name == "delay_connect" and result["elapsed_ms"] < 1800:
                raise RuntimeError("dial delay was too short")
            independent = fixture("verifier", "head", key)
            exists = not bool(independent.get("error"))
            print(f"PASS {name}: SDK elapsed={result['elapsed_ms']}ms, SDK error={bool(result.get('error'))}, bytes up/down={up}/{down}, independent object exists={exists}")
        cli("disable")
        print("PASS CLI validate/serve/enable/status/reload/disable and all seven faults")
        print("NOTE: versioned buckets may retain old versions after DeleteObject; inspect the dedicated test prefix.")
        return 0
    finally:
        for key in created:
            try:
                fixture("verifier", "delete", key)
            except RuntimeError:
                print("Cleanup failed for a test key; inspect the dedicated prefix.", file=sys.stderr)
        compose("down", "-v", check=False)


if __name__ == "__main__":
    try:
        sys.exit(main())
    except RuntimeError as error:
        print("FAIL:", error, file=sys.stderr)
        sys.exit(1)
