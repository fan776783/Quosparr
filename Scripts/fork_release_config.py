"""Identify the fork's stable releases without accepting upstream prereleases."""
import json
import pathlib
import re
import sys


def is_fork_release(config):
    updates = config.get("updates", {})
    return (re.fullmatch(r"\d+\.\d+\.\d+-cursor\.[1-9]\d*", config["version"]) is not None
            and updates.get("github") == "fan776783/Quosparr"
            and not updates.get("url"))


if __name__ == "__main__":
    if not is_fork_release(json.loads(pathlib.Path(sys.argv[1]).read_text())):
        raise SystemExit("Ad-hoc release packaging requires the fork's version and update repository")
