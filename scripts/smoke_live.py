"""Smoke-test a fresh, loopback-bound ReconRivet process.

Requires a built embedded-UI binary and RECONRIVET_PG_DSN for an isolated, empty
PostgreSQL database. The current user needs permission to create temporary files
and connect to that database. Usage: python3 scripts/smoke_live.py <binary>.

The test initializes a temporary administrator account, checks the HTTP API and
embedded login page, then terminates the process and removes its local data.
Exit 0 means all checks passed; exit 1 means startup or a check failed. The
database is disposable and should be dropped by the caller after the run.
"""

from __future__ import annotations

import argparse
import json
import logging
import os
from pathlib import Path
import queue
import re
import secrets
import socket
import subprocess
import tempfile
import threading
import time
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.request import OpenerDirector, ProxyHandler, Request, build_opener


SETUP_TOKEN = re.compile(r"Setup token for the /setup page: ([0-9a-f]{32})")
START_TIMEOUT_SECONDS = 60


def available_port() -> int:
    with socket.socket() as listener:
        listener.bind(("127.0.0.1", 0))
        return int(listener.getsockname()[1])


def request(
    opener: OpenerDirector,
    base: str,
    path: str,
    *,
    method: str = "GET",
    payload: dict[str, Any] | None = None,
    token: str | None = None,
) -> tuple[int, Any, bytes]:
    headers = {"Content-Type": "application/json"} if payload is not None else {}
    if token is not None:
        headers["Authorization"] = f"Bearer {token}"
    data = json.dumps(payload).encode("utf-8") if payload is not None else None
    req = Request(base + path, data=data, headers=headers, method=method)
    try:
        with opener.open(req, timeout=5) as response:
            return response.status, response.headers, response.read()
    except HTTPError as error:
        return error.code, error.headers, error.read()


def expect_json(status: int, raw: bytes, wanted: int, path: str) -> dict[str, Any]:
    if status != wanted:
        raise AssertionError(f"{path}: expected HTTP {wanted}, received {status}")
    value = json.loads(raw)
    if not isinstance(value, dict):
        raise AssertionError(f"{path}: expected a JSON object")
    return value


def run(binary: Path) -> None:
    if not binary.is_file():
        raise ValueError("the embedded-UI binary does not exist")
    if not os.environ.get("RECONRIVET_PG_DSN"):
        raise ValueError("RECONRIVET_PG_DSN must name an isolated empty database")

    port = available_port()
    proxy_port = available_port()
    base = f"http://127.0.0.1:{port}"
    opener = build_opener(ProxyHandler({}))
    token_events: queue.Queue[str] = queue.Queue(maxsize=1)

    with tempfile.TemporaryDirectory(prefix="reconrivet-smoke-") as root:
        root_path = Path(root)
        env = os.environ.copy()
        env["RECONRIVET_JWT_KEY_DIR"] = str(root_path / "keys")
        env["RECONRIVET_SKILL_DIR"] = str(Path.cwd() / "skills")
        process = subprocess.Popen(
            [str(binary), "-addr", f"127.0.0.1:{port}", "-proxy", f"127.0.0.1:{proxy_port}", "-data", str(root_path / "data"), "-exec-uid", "-1"],
            cwd=Path.cwd(), env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
            text=True, encoding="utf-8", errors="replace", bufsize=1,
        )
        assert process.stdout is not None

        def read_log() -> None:
            for line in process.stdout:
                match = SETUP_TOKEN.search(line)
                if match and token_events.empty():
                    token_events.put(match.group(1))

        reader = threading.Thread(target=read_log, daemon=True)
        reader.start()
        try:
            deadline = time.monotonic() + START_TIMEOUT_SECONDS
            setup_token: str | None = None
            while time.monotonic() < deadline:
                if process.poll() is not None:
                    raise AssertionError(f"server exited during startup with code {process.returncode}")
                if setup_token is None:
                    try:
                        setup_token = token_events.get_nowait()
                    except queue.Empty:
                        pass
                try:
                    status, _, raw = request(opener, base, "/api/ready")
                    if status == 200 and json.loads(raw).get("ready") is True and setup_token:
                        break
                except (URLError, ValueError):
                    pass
                time.sleep(0.2)
            else:
                raise AssertionError("server did not become ready and issue a setup token within 60 seconds")

            status, _, raw = request(opener, base, "/api/auth/status")
            if expect_json(status, raw, 200, "/api/auth/status").get("initialized") is not False:
                raise AssertionError("fresh instance unexpectedly has an administrator password")

            status, _, raw = request(opener, base, "/api/health")
            if "service" in expect_json(status, raw, 200, "/api/health"):
                raise AssertionError("unauthenticated health response disclosed the service name")

            password = secrets.token_urlsafe(24)
            status, _, raw = request(
                opener, base, "/api/auth/init", method="POST",
                payload={"password": password, "setup_token": setup_token},
            )
            init_token = expect_json(status, raw, 200, "/api/auth/init").get("token")
            if not isinstance(init_token, str) or not init_token:
                raise AssertionError("setup did not return a session token")

            status, _, raw = request(opener, base, "/api/auth/status")
            if expect_json(status, raw, 200, "/api/auth/status").get("initialized") is not True:
                raise AssertionError("setup did not initialize the administrator")

            status, _, raw = request(
                opener, base, "/api/auth/login", method="POST",
                payload={"username": "ReconRivet", "password": password},
            )
            login_token = expect_json(status, raw, 200, "/api/auth/login").get("token")
            if not isinstance(login_token, str) or not login_token:
                raise AssertionError("login did not return a session token")

            status, _, raw = request(opener, base, "/api/health", token=login_token)
            health = expect_json(status, raw, 200, "/api/health")
            if health.get("service") != "reconrivet" or not health.get("version"):
                raise AssertionError("authenticated health response has the wrong identity")

            status, _, _ = request(opener, base, "/api/traffic/export/har")
            if status != 401:
                raise AssertionError("HAR export was accessible without a session")

            status, headers, raw = request(opener, base, "/api/traffic/export/har", token=login_token)
            har = expect_json(status, raw, 200, "/api/traffic/export/har")
            if har.get("log", {}).get("creator", {}).get("name") != "ReconRivet":
                raise AssertionError("HAR creator has the wrong identity")
            if headers.get("X-ReconRivet-HAR-Exported") != "0":
                raise AssertionError("HAR export header is missing or incorrect")

            status, _, raw = request(opener, base, "/login/")
            if status != 200 or b"ReconRivet" not in raw:
                raise AssertionError("embedded login page is unavailable or missing the new name")
        finally:
            process.terminate()
            try:
                process.wait(timeout=15)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
                raise AssertionError("server did not stop within 15 seconds") from None

    logging.info("Live smoke passed: readiness, setup, login, health, HAR export, embedded UI")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path, help="embedded-UI ReconRivet executable")
    args = parser.parse_args()
    try:
        run(args.binary.resolve())
    except (AssertionError, OSError, ValueError, json.JSONDecodeError) as error:
        logging.error("Live smoke failed: %s", error)
        return 1
    return 0


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO, format="%(message)s")
    raise SystemExit(main())
