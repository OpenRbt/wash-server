#!/usr/bin/env python3
"""Check that image tags referenced in docker-compose files exist in their
registry and provide the required multi-arch platforms.

Required platforms: linux/amd64, linux/arm64, linux/arm/v7

Only images hosted on the internal registries (registry.open-rbt.com,
r.open-rbt.com) are checked. External images (e.g. Docker Hub) and mutable
``:latest`` tags are skipped with a notice.

Exits non-zero if any required image is missing its tag or any required
platform. Emits GitHub Actions ``::error::`` annotations for failures and
writes a Markdown summary to $GITHUB_STEP_SUMMARY.
"""
import json
import os
import sys
import urllib.request
import urllib.error

import yaml

REQUIRED_PLATFORMS = ["linux/amd64", "linux/arm64", "linux/arm/v7"]
# Only these registries are checked; everything else is reported as skipped.
INTERNAL_REGISTRIES = ("registry.open-rbt.com", "r.open-rbt.com")
MUTABLE_TAGS = ("latest",)

# Accept OCI index, Docker manifest list, and single manifests so we can tell
# a missing tag (404) from a single-arch image (no manifest list).
ACCEPT = (
    "application/vnd.oci.image.index.v1+json, "
    "application/vnd.docker.distribution.manifest.list.v2+json, "
    "application/vnd.oci.image.manifest.v1+json, "
    "application/vnd.docker.distribution.manifest.v2+json"
)


def parse_ref(ref):
    """Split an image ref into (host, repo, tag)."""
    if "://" in ref:
        ref = ref.split("://", 1)[1]
    last_segment = ref.rsplit("/", 1)[-1]
    if ":" in last_segment:
        main, tag = ref.rsplit(":", 1)
    else:
        main, tag = ref, "latest"
    parts = main.split("/", 1)
    host = parts[0]
    repo = parts[1] if len(parts) == 2 else "library"
    return host, repo, tag


def images_from_compose(path):
    """Return list of (service, image) for a compose file."""
    with open(path) as f:
        data = yaml.safe_load(f) or {}
    services = data.get("services") or {}
    result = []
    for name, svc in services.items():
        if not isinstance(svc, dict):
            continue
        image = svc.get("image")
        if image:
            result.append((name, image))
    return result


def fetch_manifest(host, repo, tag):
    url = f"https://{host}/v2/{repo}/manifests/{tag}"
    req = urllib.request.Request(url, headers={"Accept": ACCEPT})
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return resp.status, json.load(resp)
    except urllib.error.HTTPError as e:
        return e.code, None
    except Exception as e:
        return 0, str(e)


def platforms_of(manifest):
    """Extract the set of platforms from a manifest list / OCI index.

    Returns the empty set for a single-arch image (no `manifests` list).
    arm64 variants are normalized to linux/arm64; arm uses its variant.
    Attestation/unknown entries are ignored.
    """
    entries = manifest.get("manifests") if isinstance(manifest, dict) else None
    if not entries:
        return set()
    plats = set()
    for m in entries:
        p = m.get("platform") or {}
        os_ = p.get("os")
        arch = p.get("architecture")
        variant = p.get("variant", "")
        if not os_ or os_ == "unknown" or not arch:
            continue
        if arch == "arm64":
            plats.add("linux/arm64")
        elif arch == "arm":
            plats.add(f"linux/arm/{variant}" if variant else "linux/arm")
        else:
            plats.add(f"{os_}/{arch}")
    return plats


def annotate_error(file, msg):
    print(f"::error file={file}::{msg}")


def check_image(file, service, image):
    host, repo, tag = parse_ref(image)
    if host not in INTERNAL_REGISTRIES:
        return ("skip-external", f"skip (external registry {host})")
    if tag in MUTABLE_TAGS:
        return ("skip-mutable", f"skip (mutable tag :{tag})")

    status, manifest = fetch_manifest(host, repo, tag)
    if status == 404:
        annotate_error(file, f"{service}: tag not found in registry: {image}")
        return ("fail", f"tag not found: {image}")
    if status != 200 or not isinstance(manifest, dict):
        detail = manifest if isinstance(manifest, str) else f"HTTP {status}"
        annotate_error(file, f"{service}: registry error for {image}: {detail}")
        return ("fail", f"registry error: {image} ({detail})")

    plats = platforms_of(manifest)
    if not plats:
        mt = manifest.get("mediaType", "?")
        annotate_error(
            file,
            f"{service}: {image} is a single-arch image (no manifest list "
            f"[{mt}]); required multi-arch: {', '.join(REQUIRED_PLATFORMS)}",
        )
        return ("fail", f"single-arch: {image}")

    missing = [p for p in REQUIRED_PLATFORMS if p not in plats]
    if missing:
        annotate_error(
            file,
            f"{service}: {image} missing platforms: {', '.join(missing)} "
            f"(present: {', '.join(sorted(plats))})",
        )
        return ("fail", f"missing {','.join(missing)}: {image}")
    return ("ok", f"ok: {image} [{', '.join(sorted(plats))}]")


def main(files):
    summary_lines = ["## Compose image / platform check", ""]
    summary_lines.append("| File | Service | Image | Result |")
    summary_lines.append("| --- | --- | --- | --- |")
    failed = False
    for path in files:
        for service, image in images_from_compose(path):
            status, detail = check_image(path, service, image)
            escaped = detail.replace("|", "\\|")
            summary_lines.append(f"| `{path}` | `{service}` | `{image}` | {escaped} |")
            print(f"[{status}] {path}:{service} -> {detail}")
            if status == "fail":
                failed = True

    step_summary = os.environ.get("GITHUB_STEP_SUMMARY")
    if step_summary:
        with open(step_summary, "a") as f:
            f.write("\n".join(summary_lines) + "\n")

    if failed:
        print("\n::error::One or more images are missing their tag or required "
              "platforms (linux/amd64, linux/arm64, linux/arm/v7).")
        return 1
    print("\nAll checked images present all required platforms.")
    return 0


if __name__ == "__main__":
    files = sys.argv[1:]
    if not files:
        print("usage: check_compose_images.py <docker-compose.yml> [...]")
        sys.exit(2)
    sys.exit(main(files))