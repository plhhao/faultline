#!/usr/bin/env python3
"""Prepare an isolated local Phase 7 fixture; never overwrite an existing directory."""
import argparse
import getpass
from pathlib import Path
import shutil
import subprocess

root = Path(__file__).resolve().parents[2]
parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument("--directory", default="/tmp/faultline-phase7")
args = parser.parse_args()
directory = Path(args.directory).resolve()
directory.mkdir(mode=0o700)
(directory / "data").mkdir(mode=0o700)
shutil.copyfile(root / "examples/tester/config.yaml", directory / "config.yaml")
subprocess.run(["rtk", "proxy", "go", "build", "-o", str(directory / "faultline"), "./cmd/faultline"], cwd=root, check=True)
subprocess.run(["rtk", "proxy", "openssl", "req", "-x509", "-newkey", "rsa:2048", "-sha256", "-nodes", "-keyout", str(directory / "key.pem"), "-out", str(directory / "cert.pem"), "-days", "14", "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost,IP:127.0.0.1"], check=True)
(directory / "key.pem").chmod(0o600)
for name, role in [("tester", "editor"), ("viewer", "viewer")]:
    while True:
        password = getpass.getpass(f"Password for {name} (12–1024 bytes): ")
        if 12 <= len(password.encode()) <= 1024:
            break
        print("Password must be 12–1024 bytes.")
    subprocess.run(["rtk", "proxy", str(directory / "faultline"), "user", "--data-dir", str(directory / "data"), "--name", name, "--role", role], input=password.encode(), check=True)
print(f"Ready: {directory}. Follow examples/tester/README.md to start services and test the UI.")
