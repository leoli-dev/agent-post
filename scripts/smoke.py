#!/usr/bin/env python3
"""Build and characterize a real local Agent Post service using synthetic data.

Uses only Python's standard library and a Go toolchain. All binary, credentials,
service state and downloaded files live in an automatically removed tempdir.
The optional benchmarks are local characterization, not release acceptance.
"""

import argparse
import collections
import concurrent.futures
import hashlib
import http.client
import json
import math
import os
import pathlib
import platform
import queue
import re
import secrets
import signal
import subprocess
import sys
import tempfile
import threading
import time
import urllib.error
import urllib.request

ROOT = pathlib.Path(__file__).resolve().parents[1]


def require(condition, description):
    if not condition:
        raise AssertionError(description)


def command_output(args):
    return subprocess.check_output(args, cwd=ROOT, text=True).strip()


def machine_info():
    result = {"os": platform.system(), "os_release": platform.release(),
              "architecture": platform.machine(), "logical_cpus": os.cpu_count(),
              "cpu": platform.processor() or "unavailable", "ram_bytes": None}
    if platform.system() == "Darwin":
        for key, name in (("cpu", "machdep.cpu.brand_string"), ("ram_bytes", "hw.memsize")):
            try:
                value = subprocess.check_output(["sysctl", "-n", name], text=True).strip()
                result[key] = int(value) if key == "ram_bytes" else value
            except (OSError, subprocess.CalledProcessError, ValueError):
                pass
    else:
        try:
            result["ram_bytes"] = os.sysconf("SC_PAGE_SIZE") * os.sysconf("SC_PHYS_PAGES")
        except (AttributeError, OSError, ValueError):
            pass
        try:
            for line in pathlib.Path("/proc/cpuinfo").read_text().splitlines():
                if line.startswith("model name"):
                    result["cpu"] = line.split(":", 1)[1].strip()
                    break
        except OSError:
            pass
    return result


class Fixture:
    def __init__(self, folder):
        self.folder = pathlib.Path(folder)
        self.binary = self.folder / ("agent-post.exe" if os.name == "nt" else "agent-post")
        subprocess.run(["go", "build", "-trimpath", "-o", str(self.binary), "./cmd/agent-post"],
                       cwd=ROOT, check=True, stdout=subprocess.DEVNULL)
        self.password = secrets.token_urlsafe(32)
        self.process = None
        self.base = None
        self.keys = {}
        self.ids = {}
        self.checks = []
        try:
            self.start()
        except Exception:
            if self.process is not None and self.process.poll() is None:
                self.process.kill()
                self.process.wait()
            raise

    def start(self):
        env = dict(os.environ, AGENT_POST_ADMIN_PASSWORD=self.password)
        self.process = subprocess.Popen(
            [str(self.binary), "serve", "--data-dir", str(self.folder / "state"),
             "--listen", "127.0.0.1:0"], env=env, stdout=subprocess.DEVNULL,
            stderr=subprocess.PIPE, text=True)
        lines = queue.Queue()

        def read_stderr():
            for line in self.process.stderr:
                lines.put(line)

        threading.Thread(target=read_stderr, daemon=True).start()
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            try:
                line = lines.get(timeout=0.1)
                match = re.search(r"Agent Post listening on (127\.0\.0\.1:\d+)", line)
                if match:
                    self.base = "http://" + match.group(1)
                    self.admin("GET", "/state")
                    return
            except queue.Empty:
                if self.process.poll() is not None:
                    raise RuntimeError("temporary service exited during startup")
        raise RuntimeError("temporary service did not become ready")

    def stop(self):
        if self.process is None:
            return
        if self.process.poll() is None:
            self.process.send_signal(signal.SIGTERM)
            try:
                self.process.wait(timeout=20)
            except subprocess.TimeoutExpired:
                self.process.kill()
                self.process.wait()
                raise RuntimeError("temporary service failed graceful shutdown")
        code = self.process.returncode
        self.process.stderr.close()
        self.process = None
        require(code == 0, "service must shut down cleanly")

    def request(self, method, path, key, body=None, headers=None, expected=200):
        data = None if body is None else json.dumps(body, ensure_ascii=False).encode()
        request_headers = {"Authorization": "Bearer " + key, "Content-Type": "application/json"}
        request_headers.update(headers or {})
        request = urllib.request.Request(self.base + path, data=data, method=method,
                                         headers=request_headers)
        try:
            response = urllib.request.urlopen(request, timeout=120)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            status, raw = response.status, response.read()
        require(status == expected, "unexpected HTTP status on " + method + " " + path + ": " + str(status))
        return json.loads(raw)

    def admin(self, method, suffix, body=None):
        return self.request(method, "/admin/api" + suffix, self.password, body)

    def api(self, name, method, suffix, body=None, **kwargs):
        return self.request(method, "/api/v1" + suffix, self.keys[name], body, **kwargs)

    def host(self, name, ips):
        return self.admin("POST", "/hosts", {"name": name, "os": "synthetic-test", "ips": ips})["host"]["id"]

    def agent(self, name, host):
        result = self.admin("POST", "/agents", {"name": name, "host_id": host})
        self.keys[name] = result["api_key"]
        self.ids[name] = result["agent"]["id"]

    def send(self, body, targets, sender="alice"):
        return self.api(sender, "POST", "/messages/direct",
                        {"body": body, "to_agent_ids": [self.ids.get(n, n) for n in targets]})["results"]

    def history(self, name, channel):
        return self.api(name, "GET", "/channels/" + channel + "/messages")["messages"]

    def upload(self, name, targets, body, file):
        """Stream metadata and one file with a known multipart content length."""
        boundary = "agent-post-smoke-" + secrets.token_hex(16)
        metadata = json.dumps({"to_agent_ids": [self.ids[n] for n in targets], "body": body}).encode()
        prefix = ("--" + boundary + '\r\nContent-Disposition: form-data; name="metadata"\r\n\r\n').encode()
        prefix += metadata + ("\r\n--" + boundary + '\r\nContent-Disposition: form-data; name="file"; filename="sample.bin"\r\nContent-Type: application/octet-stream\r\n\r\n').encode()
        suffix = ("\r\n--" + boundary + "--\r\n").encode()
        authority = self.base.removeprefix("http://")
        connection = http.client.HTTPConnection(authority, timeout=120)
        try:
            connection.putrequest("POST", "/api/v1/messages/direct")
            connection.putheader("Authorization", "Bearer " + self.keys[name])
            connection.putheader("Content-Type", "multipart/form-data; boundary=" + boundary)
            connection.putheader("Content-Length", str(len(prefix) + file.stat().st_size + len(suffix)))
            connection.endheaders()
            connection.send(prefix)
            with file.open("rb") as stream:
                while block := stream.read(65536):
                    connection.send(block)
            connection.send(suffix)
            response = connection.getresponse()
            raw = response.read()
            require(response.status == 200, "streamed upload must succeed")
            return json.loads(raw)["results"]
        finally:
            connection.close()

    def download(self, name, file_id, expected=200):
        request = urllib.request.Request(self.base + "/api/v1/files/" + file_id + "/content",
                                         headers={"Authorization": "Bearer " + self.keys[name]})
        try:
            response = urllib.request.urlopen(request, timeout=120)
        except urllib.error.HTTPError as error:
            response = error
        digest = hashlib.sha256()
        size = 0
        with response:
            require(response.status == expected, "download authorization/retention status")
            while block := response.read(65536):
                digest.update(block)
                size += len(block)
        return size, digest.hexdigest()

    def cli(self, name, *args, expected=0):
        env = {k: v for k, v in os.environ.items() if not k.startswith("AGENT_POST_")}
        env.update(AGENT_POST_URL=self.base, AGENT_POST_API_KEY=self.keys[name])
        # Use an explicit private config so existing user configuration cannot affect this test.
        config = self.folder / "client.json"
        config.write_text("{}")
        config.chmod(0o600)
        env["AGENT_POST_CONFIG"] = str(config)
        result = subprocess.run([str(self.binary), *args], env=env, capture_output=True, text=True, timeout=120)
        require(result.returncode == expected, "CLI exit status for " + args[0])
        return json.loads(result.stdout)

    def mcp_smoke(self):
        env = {k: v for k, v in os.environ.items() if not k.startswith("AGENT_POST_")}
        env.update(AGENT_POST_URL=self.base, AGENT_POST_API_KEY=self.keys["alice"],
                   AGENT_POST_CONFIG=str(self.folder / "client.json"))
        process = subprocess.Popen([str(self.binary), "mcp"], env=env, stdin=subprocess.PIPE,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        messages = queue.Queue()

        def reader():
            for line in process.stdout:
                messages.put(line)

        threading.Thread(target=reader, daemon=True).start()

        def send(value):
            process.stdin.write(json.dumps(value) + "\n")
            process.stdin.flush()

        def receive(identifier):
            deadline = time.monotonic() + 15
            while time.monotonic() < deadline:
                message = json.loads(messages.get(timeout=max(0.01, deadline - time.monotonic())))
                if message.get("id") == identifier:
                    require("error" not in message, "MCP protocol error")
                    return message["result"]
            raise AssertionError("MCP response deadline")

        try:
            send({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
                "protocolVersion": "2025-11-25", "capabilities": {}, "clientInfo": {"name": "synthetic-smoke", "version": "1"}}})
            receive(1)
            send({"jsonrpc": "2.0", "method": "notifications/initialized"})
            send({"jsonrpc": "2.0", "id": 2, "method": "tools/list", "params": {}})
            require(len(receive(2)["tools"]) == 18, "MCP operation inventory")
            send({"jsonrpc": "2.0", "id": 3, "method": "tools/call", "params": {"name": "agent_post_me", "arguments": {}}})
            result = receive(3)
            require(not result.get("isError", False), "MCP me result")
            require(result["structuredContent"]["id"] == self.ids["alice"], "MCP logical identity parity")
            self.checks.append("real stdio MCP initialize/list/tools-call identity parity")
        finally:
            process.stdin.close()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait()
            process.stdout.close()
            process.stderr.close()

    def smoke(self):
        host = self.host("loopback-fixture", ["127.0.0.1"])
        denied = self.host("denied-fixture", ["192.0.2.10"])
        for name in ("alice", "bob", "carol"):
            self.agent(name, host)
        self.agent("denied", denied)
        require(self.api("alice", "GET", "/me")["id"] == self.ids["alice"], "valid key and source")
        self.request("GET", "/api/v1/me", "synthetic-invalid-key", expected=401)
        self.api("denied", "GET", "/me", expected=403,
                 headers={"X-Forwarded-For": "192.0.2.10", "Forwarded": "for=192.0.2.10"})
        self.request("GET", "/admin/api/state", self.keys["alice"], expected=401)
        self.checks.append("API key plus exact source IP, forwarded-header spoof denial, admin isolation")
        initial_settings = self.api("alice", "GET", "/limits")["settings"]
        require(initial_settings["claim_seconds"] == 60, "confirmed default claim duration")

        results = self.send("fanout-original", ["bob", "carol", "alice", "agt_missing"])
        require([r["status"] for r in results] == ["sent", "sent", "rejected", "rejected"], "partial fanout outcomes")
        require(results[0]["message_id"] != results[1]["message_id"], "independent private copies")
        bob_channel, carol_channel = results[0]["channel_id"], results[1]["channel_id"]
        require(self.send("fanout-original", ["bob", "carol"])[0]["status"] == "duplicate", "retained deduplication")
        self.api("bob", "GET", "/channels/" + carol_channel + "/messages", expected=403)
        claim = self.api("bob", "POST", "/inbox/claim", {"limit": 1})["messages"][0]
        require(claim["id"] == results[0]["message_id"], "claim oldest message")
        require(self.api("bob", "POST", "/inbox/claim", {"limit": 1})["messages"] == [], "exclusive claim")
        rejected = self.api("bob", "POST", "/inbox/ack", {"acks": [{"message_id": claim["id"], "claim_token": "synthetic-wrong-token"}]})
        require(rejected["results"][0]["status"] == "claim_replaced", "wrong token fenced")
        ack = {"acks": [{"message_id": claim["id"], "claim_token": claim["claim_token"]}]}
        require(self.api("bob", "POST", "/inbox/ack", ack)["results"][0]["status"] == "read", "current token ACK")
        require(self.api("bob", "GET", "/inbox")["unread"] == 0, "ACK read state")
        require(self.api("carol", "GET", "/inbox")["unread"] == 1, "independent unread state")
        self.checks.append("partial independent fanout, retained dedup, private authorization, exclusive claims and fenced ACK")

        group = self.api("alice", "POST", "/groups", {"name": "synthetic-group", "invitee_ids": [self.ids["bob"]]})["channel_id"]
        self.api("alice", "POST", "/groups/" + group + "/messages", {"body": "before-join"})
        self.api("bob", "GET", "/channels/" + group + "/messages", expected=403)
        self.api("carol", "POST", "/groups/" + group + "/join", {}, expected=403)
        self.api("bob", "POST", "/groups/" + group + "/join", {})
        require(self.history("bob", group)[0]["body"] == "before-join", "prejoin retained history")
        require(self.api("bob", "GET", "/inbox?channel_id=" + group)["unread"] == 0, "prejoin history is not unread")
        self.api("alice", "POST", "/groups/" + group + "/messages", {"body": "after-join"})
        require(self.api("bob", "GET", "/inbox?channel_id=" + group)["unread"] == 1, "postjoin unread")
        self.api("bob", "POST", "/groups/" + group + "/leave", {})
        self.api("bob", "GET", "/channels/" + group + "/messages", expected=403)
        self.checks.append("pending group authorization, invited join, prejoin history without unread, postjoin unread and leave denial")

        file = self.folder / "sample.bin"
        file.write_bytes(bytes(range(256)) * 1024)
        digest = hashlib.sha256(file.read_bytes()).hexdigest()
        results = self.upload("alice", ["bob", "carol"], "binary-copy", file)
        bob_message = next(m for m in self.history("bob", bob_channel) if m["id"] == results[0]["message_id"])
        carol_message = next(m for m in self.history("carol", carol_channel) if m["id"] == results[1]["message_id"])
        bob_file, carol_file = bob_message["attachment"]["id"], carol_message["attachment"]["id"]
        require(bob_file != carol_file, "independent file IDs")
        bob_owned = self.folder / "state" / "files" / bob_file
        carol_owned = self.folder / "state" / "files" / carol_file
        require(bob_owned.exists() and carol_owned.exists(), "independent physical owned files")
        require(not os.path.samefile(bob_owned, carol_owned), "fanout files are not hard links")
        require(self.download("bob", bob_file) == (file.stat().st_size, digest), "binary roundtrip")
        self.download("bob", carol_file, expected=404)
        settings = dict(initial_settings, message_count=2)
        self.admin("PUT", "/settings", settings)
        self.send("evict-bob-only", ["bob"])
        require(all(m["body"] != "fanout-original" for m in self.history("bob", bob_channel)), "oldest count eviction")
        self.send("evict-bob-file", ["bob"])
        self.download("bob", bob_file, expected=404)
        require(not bob_owned.exists() and carol_owned.exists(), "physical file deletion isolated to one channel")
        require(self.download("carol", carol_file) == (file.stat().st_size, digest), "fanout retention isolation")
        require(all(m["id"] != bob_message["id"] for m in self.history("bob", bob_channel)), "message deletion deletes attachment")
        require(self.send("fanout-original", ["bob"])[0]["status"] == "sent", "dedup evidence removed with eviction")
        self.admin("PUT", "/settings", dict(settings, file_count=0))
        self.download("carol", carol_file, expected=404)
        require(all(m["id"] != carol_message["id"] for m in self.history("carol", carol_channel)), "file eviction deletes full carrying message")
        self.admin("PUT", "/settings", dict(initial_settings, body_codepoints=3))
        require(self.history("bob", bob_channel) == [], "retroactive body-limit deletion")
        require(self.send("é🙂中", ["bob"])[0]["status"] == "sent", "Unicode codepoint boundary")
        too_long = self.api("alice", "POST", "/messages/direct", {"to_agent_ids": [self.ids["bob"]], "body": "é🙂中x"}, expected=400)
        require(too_long["error"]["code"] == "text_too_long" and too_long["error"]["limit"] == 3, "whole Unicode body rejected with current limit")
        # Remove the Unicode boundary fixture before the durable single-message claim check.
        self.admin("PUT", "/settings", dict(initial_settings, body_codepoints=1))
        self.admin("PUT", "/settings", initial_settings)
        self.checks.append("256 KiB binary streaming, independent fanout files, count eviction, both deletion directions and retroactive limits")

        self.admin("PUT", "/settings", dict(initial_settings, claim_seconds=1))
        fenced = self.send("real-expiration-fence", ["bob"])[0]
        expired_claim = self.api("bob", "POST", "/inbox/claim", {"limit": 1})["messages"][0]
        time.sleep(1.1)
        expired_ack = {"acks": [{"message_id": expired_claim["id"], "claim_token": expired_claim["claim_token"]}]}
        require(self.api("bob", "POST", "/inbox/ack", expired_ack)["results"][0]["status"] == "claim_expired", "real elapsed claim deadline")
        current = self.api("bob", "POST", "/inbox/claim", {"limit": 1})["messages"][0]
        require(current["id"] == fenced["message_id"] and current["claim_token"] != expired_claim["claim_token"], "fresh credential after expiry")
        require(self.api("bob", "POST", "/inbox/ack", expired_ack)["results"][0]["status"] == "claim_replaced", "superseded claim credential fenced")
        self.api("bob", "POST", "/inbox/ack", {"acks": [{"message_id": current["id"], "claim_token": current["claim_token"]}]})
        self.admin("PUT", "/settings", initial_settings)
        self.checks.append("real claim expiration, superseded credential fencing and Unicode codepoint length boundary")
        durable = self.send("survives-restart", ["bob"])[0]
        require(self.cli("bob", "me")["id"] == self.ids["bob"], "CLI identity parity")
        before = self.api("bob", "GET", "/inbox")["unread"]
        cli_claim = self.cli("bob", "claim", "--limit", "1")["messages"][0]
        require(self.api("bob", "GET", "/inbox")["unread"] == before, "CLI never auto ACK")
        cli_ack = self.cli("bob", "ack", "--acks", json.dumps([{"message_id": cli_claim["id"], "claim_token": cli_claim["claim_token"]}]))
        require(cli_ack["results"][0]["status"] == "read", "CLI ACK parity")
        self.mcp_smoke()
        self.stop()
        self.start()
        require(any(m["id"] == durable["message_id"] for m in self.history("bob", bob_channel)), "durable history after restart")
        require(self.api("bob", "GET", "/inbox")["unread"] == 0, "durable ACK after restart")
        require(self.api("alice", "GET", "/limits")["settings"]["message_count"] == initial_settings["message_count"], "durable settings")
        self.checks.append("real CLI identity/claim/ACK parity without auto ACK and graceful-restart durable history/read state/settings")
        return host, initial_settings


def percentiles(values):
    ordered = sorted(values)
    if not ordered:
        return {"p50": None, "p95": None, "p99": None}
    return {"p" + str(p): round(ordered[max(0, math.ceil(p / 100 * len(ordered)) - 1)] * 1000, 3)
            for p in (50, 95, 99)}


def benchmark(fixture, host, initial_settings, seconds):
    """Schedule exactly 100 offered calls/second without response-paced admission."""
    fixture.agent("load-sender", host)
    fixture.agent("load-receiver", host)
    settings = dict(initial_settings, message_count=10000, claim_seconds=300)
    fixture.admin("PUT", "/settings", settings)
    # A reserve of valid claims keeps ACK calls real while the pipeline warms up.
    for index in range(100):
        fixture.send("warmup-" + str(index), ["load-receiver"], sender="load-sender")
    tokens = collections.deque()
    for _ in range(2):
        tokens.extend(fixture.api("load-receiver", "POST", "/inbox/claim", {"limit": 50})["messages"])
    lock = threading.Lock()
    records = []
    labels = ("send", "directory", "inbox", "claim", "ack")
    offered = int(seconds * 100)
    start = time.monotonic() + 0.05

    def work(index, scheduled):
        intended = labels[index % len(labels)]
        actual = intended
        failure = None
        empty_claim = False
        started = time.monotonic()
        try:
            if intended == "send":
                result = fixture.send("load-" + str(index), ["load-receiver"], sender="load-sender")
                require(result[0]["status"] == "sent", "load send outcome")
            elif intended == "directory":
                fixture.api("load-sender", "GET", "/directory?limit=100")
            elif intended == "inbox":
                fixture.api("load-receiver", "GET", "/inbox")
            elif intended == "claim":
                messages = fixture.api("load-receiver", "POST", "/inbox/claim", {"limit": 1})["messages"]
                empty_claim = not messages
                with lock:
                    tokens.extend(messages)
            else:
                with lock:
                    token = tokens.popleft() if tokens else None
                if token is None:
                    # Label the real operation instead of inventing a successful ACK.
                    actual = "inbox_fallback_for_empty_ack_queue"
                    fixture.api("load-receiver", "GET", "/inbox")
                else:
                    result = fixture.api("load-receiver", "POST", "/inbox/ack", {"acks": [
                        {"message_id": token["id"], "claim_token": token["claim_token"]}]})
                    require(result["results"][0]["status"] == "read", "load ACK must use a valid current claim")
        except Exception as error:
            failure = type(error).__name__ + ": " + str(error)
        ended = time.monotonic()
        with lock:
            records.append({"intended": intended, "actual": actual, "error": failure,
                            "scheduled_latency": ended - scheduled,
                            "service_latency": ended - started,
                            "queue_delay": started - scheduled,
                            "empty_claim": empty_claim, "ended": ended})

    # Finite scheduling keeps a slow server's backlog visible in end-to-end latency.
    with concurrent.futures.ThreadPoolExecutor(max_workers=32) as pool:
        for index in range(offered):
            scheduled = start + index / 100
            delay = scheduled - time.monotonic()
            if delay > 0:
                time.sleep(delay)
            pool.submit(work, index, scheduled)
    finish = time.monotonic()
    counts = collections.Counter(r["actual"] for r in records)
    errors = collections.Counter(r["error"] for r in records if r["error"])
    per_operation = {}
    for label in counts:
        selected = [r for r in records if r["actual"] == label]
        per_operation[label] = {"completed": len(selected), "errors": sum(r["error"] is not None for r in selected),
                                "scheduled_latency_ms": percentiles([r["scheduled_latency"] for r in selected])}
    completed_in_window = sum(r["ended"] <= start + seconds for r in records)
    return {
        "classification": "local mixed HTTP characterization; not full acceptance or platform compatibility evidence",
        "offered_rps": 100, "offered_seconds": seconds, "offered_requests": offered,
        "client_workers": 32, "client_transport": "Python urllib HTTP, new connection per call; load client shares server host",
        "scheduled_workload": {name: "20%" for name in labels},
        "workload_details": "one synthetic sender/receiver, unique short text; 100 warmup messages/claims; directory page 100; claim and ACK batches 1",
        "settings": fixture.api("load-sender", "GET", "/limits")["settings"],
        "actual_operation_counts": dict(counts), "completed_requests": len(records),
        "completed_in_offered_window": completed_in_window,
        "completed_rps_in_offered_window": round(completed_in_window / seconds, 3),
        "elapsed_including_drain_seconds": round(finish - start, 6),
        "completed_rps_including_drain": round(len(records) / (finish - start), 3),
        "errors": sum(errors.values()), "error_details": dict(errors),
        "empty_claim_responses": sum(r["empty_claim"] for r in records),
        "scheduled_end_to_end_latency_ms": percentiles([r["scheduled_latency"] for r in records]),
        "request_execution_latency_ms": percentiles([r["service_latency"] for r in records]),
        "scheduler_and_worker_delay_ms": percentiles([r["queue_delay"] for r in records]),
        "per_operation": per_operation,
        "latency_definition": "scheduled end-to-end includes scheduler lateness, executor queue, HTTP transport, server execution and response read; errors remain in latency sample",
    }


def file_benchmark(fixture, host, initial_settings, rounds):
    fixture.agent("file-sender", host)
    fixture.agent("file-receiver", host)
    fixture.admin("PUT", "/settings", initial_settings)
    file = fixture.folder / "benchmark.bin"
    file.write_bytes(bytes(range(256)) * 1024)
    expected_digest = hashlib.sha256(file.read_bytes()).hexdigest()
    size = file.stat().st_size
    upload_seconds = download_seconds = 0.0
    all_start = time.monotonic()
    for index in range(rounds):
        started = time.monotonic()
        result = fixture.upload("file-sender", ["file-receiver"], "file-load-" + str(index), file)[0]
        upload_seconds += time.monotonic() - started
        require(result["status"] == "sent", "file benchmark send")
        messages = fixture.history("file-receiver", result["channel_id"])
        message = next(m for m in messages if m["id"] == result["message_id"])
        started = time.monotonic()
        downloaded = fixture.download("file-receiver", message["attachment"]["id"])
        download_seconds += time.monotonic() - started
        require(downloaded == (size, expected_digest), "file benchmark byte count and SHA-256 integrity")
    elapsed = time.monotonic() - all_start
    return {"classification": "sequential local file characterization, independent from mixed HTTP load",
            "rounds": rounds, "file_bytes": size, "upload_chunk_bytes": 65536, "download_chunk_bytes": 65536,
            "sha256_verified_every_round": True, "useful_upload_bytes": rounds * size,
            "useful_download_bytes": rounds * size, "upload_seconds": round(upload_seconds, 6),
            "download_seconds": round(download_seconds, 6), "total_seconds_including_history_lookup": round(elapsed, 6),
            "useful_upload_bytes_per_second": round(rounds * size / upload_seconds, 3),
            "useful_download_bytes_per_second": round(rounds * size / download_seconds, 3),
            "useful_combined_bytes_per_second_including_history": round(2 * rounds * size / elapsed, 3)}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--benchmark-seconds", type=float, default=0,
                        help="optional open-loop 100 RPS local mixed HTTP characterization duration")
    parser.add_argument("--file-benchmark-rounds", type=int, default=0,
                        help="optional independent sequential 256 KiB upload/download rounds")
    args = parser.parse_args()
    if args.benchmark_seconds < 0 or not math.isfinite(args.benchmark_seconds) or args.benchmark_seconds > 3600:
        parser.error("benchmark seconds must be finite and between 0 and 3600")
    if args.file_benchmark_rounds < 0 or args.file_benchmark_rounds > 10000:
        parser.error("file benchmark rounds must be between 0 and 10000")
    fixture = None
    started = time.monotonic()
    with tempfile.TemporaryDirectory(prefix="agent-post-smoke-") as folder:
        try:
            fixture = Fixture(folder)
            host, initial_settings = fixture.smoke()
            result = {"smoke": {"passed": True, "checks": fixture.checks}, "machine": machine_info(),
                      "source_revision": command_output(["git", "rev-parse", "HEAD"]),
                      "tracked_source_dirty": bool(command_output(["git", "status", "--porcelain", "--untracked-files=no"])),
                      "toolchain": command_output(["go", "version"]),
                      "durability": {"storage": "SQLite on local temporary filesystem",
                                     "server_configured_pragmas": {"journal_mode": "WAL", "synchronous": "FULL", "secure_delete": "ON", "busy_timeout": 5000},
                                     "note": "configuration recorded from service source; graceful restart verified; no crash/power-loss guarantee measured"}}
            if args.benchmark_seconds:
                result["http_benchmark"] = benchmark(fixture, host, initial_settings, args.benchmark_seconds)
            if args.file_benchmark_rounds:
                result["file_benchmark"] = file_benchmark(fixture, host, initial_settings, args.file_benchmark_rounds)
            fixture.stop()
            result["elapsed_seconds_including_build_and_smoke"] = round(time.monotonic() - started, 3)
            print(json.dumps(result, indent=2, sort_keys=True))
        finally:
            if fixture is not None:
                fixture.stop()


if __name__ == "__main__":
    main()
