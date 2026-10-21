#!/usr/bin/env python3
"""Reapply the fork's update source and version to mygo.json after an upstream merge.

Usage: fork-sync-config.py <fork mygo.json before the merge> <upstream mygo.json>
The working tree's mygo.json keeps upstream's other fields; the version
becomes <upstream version>-cursor.1 when upstream moved to a new version.
"""
import json
import pathlib
import re
import sys

fork = json.loads(pathlib.Path(sys.argv[1]).read_text())
upstream = json.loads(pathlib.Path(sys.argv[2]).read_text())
path = pathlib.Path("mygo.json")
config = json.loads(path.read_text())

base, sep, suffix = fork["version"].partition("-")
if not re.fullmatch(r"cursor\.[1-9][0-9]*", suffix) or not sep:
    raise SystemExit("fork version must look like 1.7.3-cursor.1, got " + fork["version"])
if upstream["version"] == base:
    config["version"] = fork["version"]
elif "-" in upstream["version"]:
    raise SystemExit("upstream prerelease versions are not synced: " + upstream["version"])
else:
    config["version"] = upstream["version"] + "-cursor.1"
config["updates"]["publicKey"] = fork["updates"]["publicKey"]
config["updates"]["github"] = fork["updates"]["github"]
path.write_text(json.dumps(config, indent=2, ensure_ascii=False) + "\n")
print(config["version"])
