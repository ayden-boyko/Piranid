#!/usr/bin/env python3
"""Validate the Kubernetes manifests in manifests/.

Checks three classes of mistake that are easy to make and awkward to notice by
reading:

  1. every file parses as YAML
  2. every ConfigMap and PersistentVolumeClaim a Deployment references is
     actually defined somewhere in the tree
  3. no container declares the same environment variable twice

(2) matters because the logging stack was one Deployment referencing an image
nothing builds. (3) matters because a duplicated env key is silently accepted
by Kubernetes and the last value wins.

Exits non-zero on the first category of failure found, printing every problem.
"""

from __future__ import annotations

import glob
import sys

import yaml


def load(paths: list[str]) -> tuple[list[dict], bool]:
    docs: list[dict] = []
    ok = True

    for path in paths:
        try:
            with open(path) as handle:
                parsed = [d for d in yaml.safe_load_all(handle) if d]
        except yaml.YAMLError as exc:
            print(f"{path}: does not parse: {exc}")
            ok = False
            continue
        docs.extend(parsed)

    return docs, ok


def check_references(docs: list[dict]) -> bool:
    configmaps = {d["metadata"]["name"] for d in docs if d["kind"] == "ConfigMap"}
    claims = {
        d["metadata"]["name"] for d in docs if d["kind"] == "PersistentVolumeClaim"
    }
    ok = True

    for doc in docs:
        if doc["kind"] != "Deployment":
            continue

        name = doc["metadata"]["name"]
        spec = doc["spec"]["template"]["spec"]

        for volume in spec.get("volumes", []):
            if "configMap" in volume:
                ref = volume["configMap"]["name"]
                if ref not in configmaps:
                    print(f"{name}: references undefined ConfigMap {ref!r}")
                    ok = False
            if "persistentVolumeClaim" in volume:
                ref = volume["persistentVolumeClaim"]["claimName"]
                if ref not in claims:
                    print(f"{name}: references undefined PVC {ref!r}")
                    ok = False

        # A mount with no matching volume leaves the pod unschedulable.
        volume_names = {v["name"] for v in spec.get("volumes", [])}
        for container in spec.get("containers", []):
            for mount in container.get("volumeMounts", []):
                if mount["name"] not in volume_names:
                    print(
                        f"{name}/{container['name']}: mounts "
                        f"{mount['name']!r} but no such volume is defined"
                    )
                    ok = False

            names = [e["name"] for e in container.get("env", [])]
            dupes = {n for n in names if names.count(n) > 1}
            if dupes:
                print(f"{name}/{container['name']}: duplicate env vars {sorted(dupes)}")
                ok = False

    return ok


def main() -> int:
    paths = sorted(glob.glob("manifests/*.yaml"))
    if not paths:
        print("no manifests found under manifests/")
        return 1

    docs, ok = load(paths)
    if ok:
        ok = check_references(docs)

    print(f"checked {len(docs)} documents across {len(paths)} files")
    return 0 if ok else 1


if __name__ == "__main__":
    sys.exit(main())