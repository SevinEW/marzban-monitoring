"""Isolated integration harness; creates only temporary loopback listeners.

Run on a Linux test host with an existing Xray binary. Observer dependencies can
come from an existing Python container. Does not change the existing core/config,
reset traffic counters, connect to external destinations, or save credentials.
The source of online_presence.py is supplied on stdin, not installed remotely.
"""
import argparse
import base64
import json
import os
import socket
import subprocess
import sys
import tempfile
import threading
import time
import uuid


def spare_port():
    with socket.socket() as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def main():
    p = argparse.ArgumentParser()
    p.add_argument("--xray", required=True)
    p.add_argument("--python-container")
    p.add_argument("--observer-binary")
    p.add_argument("--container-python", default="/code/.venv/bin/python")
    args = p.parse_args()
    source = sys.stdin.read()
    if not source and not args.observer_binary:
        raise SystemExit("collector source required on stdin")
    # Code only; no production config, user identity, or secret is transported.
    source64 = base64.b64encode(source.encode()).decode()
    api_port, inbound_port = spare_port(), spare_port()
    while api_port == inbound_port:
        inbound_port = spare_port()
    echo = socket.socket()
    echo.bind(("127.0.0.1", 0))
    echo.listen(8)
    echo.settimeout(.2)
    stop = threading.Event()
    peers, flows = [], []

    def reflect(client):
        with client:
            client.settimeout(.2)
            while not stop.is_set():
                try:
                    data = client.recv(4096)
                    if not data:
                        break
                    client.sendall(data)
                except socket.timeout:
                    pass
                except OSError:
                    break

    def accept():
        while not stop.is_set():
            try:
                c, _ = echo.accept()
                peers.append(c)
                threading.Thread(target=reflect, args=(c,), daemon=True).start()
            except socket.timeout:
                pass
            except OSError:
                break

    threading.Thread(target=accept, daemon=True).start()
    identities = [uuid.uuid4(), uuid.uuid4()]
    config = {
        "log": {"loglevel": "none"},
        "stats": {},
        "api": {"tag": "audit-api", "listen": f"127.0.0.1:{api_port}", "services": ["StatsService"]},
        "policy": {"levels": {"0": {"statsUserOnline": True}}},
        "inbounds": [{"listen": "127.0.0.1", "port": inbound_port,
                      "protocol": "vless", "tag": "audit-only",
                      "settings": {"decryption": "none", "clients": [
                          {"id": str(v), "email": f"audit-{i}", "level": 0}
                          for i, v in enumerate(identities)]}}],
        "outbounds": [{"protocol": "freedom", "tag": "direct"}],
    }
    core = None
    results = []
    scratch = tempfile.TemporaryDirectory(prefix="marzwatch-presence-audit-")

    def query(expected, label, settle_timeout=0):
        observer = f'''import asyncio,base64,json
exec(compile(base64.b64decode({source64!r}), 'online_presence.py', 'exec'))
async def run():
 c=XrayPresenceClient({api_port})
 try:
  obs=await c.sample(tracking_verified=True)
  out=public_snapshot({{"test":obs}},["test"])
  print(json.dumps({{"users":out["users"],"status":out["nodes"]["test"]["status"],"latency_ms":obs.latency_ms}}))
 finally:c.close()
asyncio.run(run())
'''
        started = time.monotonic()
        while True:
            if args.observer_binary:
                from pathlib import Path
                fields=Path(f"/proc/{core.pid}/stat").read_text().rsplit(")",1)[1].split()
                path=Path(scratch.name)/"binding.json"
                path.write_text(json.dumps({"schema":1,"namespace":"isolated-audit-panel-namespace", "tracking_verified":True,
                    "cores":[{"address":f"127.0.0.1:{api_port}","pid":core.pid,"start_ticks":fields[19]}]}))
                r=subprocess.run([args.observer_binary,"presence-check","--binding",str(path)],capture_output=True,text=True,timeout=6)
            else:
                r = subprocess.run(["docker", "exec", "-i", args.python_container,
                                    args.container_python, "-"], input=observer,
                                   capture_output=True, text=True, timeout=6)
            if r.returncode:
                raise RuntimeError("observer failed (diagnostic text withheld)")
            row = json.loads(r.stdout)
            if row["users"] == expected and row["status"] == "ok":
                break
            if time.monotonic()-started >= settle_timeout:
                raise AssertionError(f"{label}: expected {expected}, observed {row['users']} ({row['status']})")
            time.sleep(.2)
        if settle_timeout:
            row["observed_close_delay_seconds"] = round(time.monotonic()-started, 2)
        results.append(dict(case=label, **row))

    def connect(user):
        c = socket.socket()
        c.settimeout(2)
        # Xray excludes exactly 127.0.0.1; use another loopback source address.
        # Every test connection has the SAME source IP, even across accounts.
        c.bind(("127.0.0.2", 0))
        c.connect(("127.0.0.1", inbound_port))
        request = (b"\0" + identities[user].bytes + b"\0\1"
                   + echo.getsockname()[1].to_bytes(2, "big")
                   + b"\1" + socket.inet_aton("127.0.0.1") + b"ping")
        c.sendall(request)
        response = b""
        while len(response) < 6:
            part = c.recv(6-len(response))
            if not part:
                raise RuntimeError("test flow closed early")
            response += part
        if response != b"\0\0ping":
            raise RuntimeError("test flow handshake mismatch")
        flows.append(c)
        return c

    try:
        env = dict(os.environ, GOMAXPROCS="1", GOMEMLIMIT="32MiB")
        core = subprocess.Popen(["nice", "-n", "15", args.xray, "run", "-c", "stdin:"],
                                stdin=subprocess.PIPE, stdout=subprocess.DEVNULL,
                                stderr=subprocess.DEVNULL, env=env)
        core.stdin.write(json.dumps(config).encode())
        core.stdin.close()
        for _ in range(30):
            if core.poll() is not None:
                raise RuntimeError("isolated core did not start")
            try:
                with socket.create_connection(("127.0.0.1", api_port), .1):
                    break
            except OSError:
                time.sleep(.1)
        query(0, "verified_empty_core")
        first = connect(0)
        query(1, "one_account_one_flow")
        more = [connect(0) for _ in range(3)]
        query(1, "one_account_four_flows_same_ip")
        second = connect(1)
        query(2, "two_accounts_same_ip")
        for flow in [first, *more]:
            flow.close()
        time.sleep(.2)
        query(1, "first_account_closed", settle_timeout=8)
        second.close()
        time.sleep(.2)
        query(0, "all_flows_closed", settle_timeout=8)
        rss = next((line.split()[1] for line in open(f"/proc/{core.pid}/status")
                    if line.startswith("VmRSS:")), None)
        print(json.dumps({"isolated_core": True, "external_traffic": False,
                          "cases": results, "test_core_rss_kib": rss}))
    finally:
        scratch.cleanup()
        stop.set()
        for s in [*flows, *peers, echo]:
            s.close()
        if core is not None and core.poll() is None:
            core.terminate()
            try:
                core.wait(timeout=3)
            except subprocess.TimeoutExpired:
                core.kill()
                core.wait(timeout=3)


if __name__ == "__main__":
    main()
