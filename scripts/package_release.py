#!/usr/bin/env python3
"""Package release assets without changing the source checkout."""

import argparse
import json
import platform
import re
import tarfile
import zipfile
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
VERSION = re.compile(r"v[0-9]+\.[0-9]+\.[0-9]+\Z")


def require_tag(tag: str) -> str:
    if not VERSION.fullmatch(tag):
        raise ValueError(f"expected a vMAJOR.MINOR.PATCH tag, got {tag!r}")
    return tag


def archive_name(tag: str) -> str:
    system = {"Linux": "linux", "Darwin": "macos", "Windows": "windows"}[platform.system()]
    machine = platform.machine().lower()
    arch = {"x86_64": "x64", "amd64": "x64", "aarch64": "arm64", "arm64": "arm64"}.get(machine, machine)
    return f"c-gotel-{tag}-{system}-{arch}-static"


def add_file(archive: zipfile.ZipFile, source: Path, destination: str) -> None:
    if not source.is_file():
        raise FileNotFoundError(source)
    archive.write(source, destination)


def package_c(tag: str, build: Path, otlp_source: Path, output: Path) -> Path:
    name = archive_name(tag)
    path = output / f"{name}.zip"
    suffix = ".lib" if platform.system() == "Windows" else ".a"
    gotel_lib = build / "src" / ("c-gotel.lib" if suffix == ".lib" else "libc-gotel.a")
    otlp_lib = build / "_deps" / "otlp_c-build" / ("otlp_c.lib" if suffix == ".lib" else "libotlp_c.a")
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as archive:
        for source, destination in (
            (gotel_lib, f"lib/{gotel_lib.name}"),
            (otlp_lib, f"lib/{otlp_lib.name}"),
            (ROOT / "LICENSE", "LICENSE"),
            (ROOT / "c-gotel" / "README.md", "README.md"),
            (ROOT / "c-gotel" / "RELEASE.md", "LINKING.md"),
            (otlp_source / "LICENSE", "third-party/otlp-c-LICENSE"),
        ):
            add_file(archive, source, f"{name}/{destination}")
        for prefix, directory in (
            ("include/gotel", ROOT / "c-gotel" / "include" / "gotel"),
            ("include/otlp-c", otlp_source / "include" / "otlp-c"),
        ):
            headers = sorted(directory.rglob("*.h"))
            if not headers:
                raise FileNotFoundError(f"no headers in {directory}")
            for source in headers:
                if prefix == "include/gotel" and source.name == "testing.h":
                    continue  # The optional gotel::testing library is not in release archives.
                add_file(archive, source, f"{name}/{prefix}/{source.relative_to(directory).as_posix()}")
    return path


def package_typescript(tag: str, tarball: Path, output: Path) -> Path:
    name = f"gotel-typescript-{tag}"
    path = output / f"{name}.zip"
    with tarfile.open(tarball, "r:gz") as package, zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as archive:
        members = [member for member in package.getmembers() if member.isfile()]
        metadata = next((member for member in members if member.name == "package/package.json"), None)
        if metadata is None:
            raise ValueError("npm tarball is missing package.json")
        package_json = json.load(package.extractfile(metadata))
        if package_json["version"] != tag[1:]:
            raise ValueError(f"package version {package_json['version']} does not match {tag}")
        if not any(member.name.startswith("package/dist/") for member in members):
            raise ValueError("npm tarball is missing compiled dist files")
        for member in members:
            relative = Path(member.name)
            if relative.parts[0] != "package" or ".." in relative.parts:
                raise ValueError(f"unexpected npm tarball path: {member.name}")
            data = package.extractfile(member).read()
            archive.writestr(f"{name}/{'/'.join(relative.parts[1:])}", data)
    return path


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("kind", choices=("c", "typescript"))
    parser.add_argument("--tag", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--build", type=Path)
    parser.add_argument("--otlp-source", type=Path)
    parser.add_argument("--tarball", type=Path)
    args = parser.parse_args()
    tag = require_tag(args.tag)
    args.output.mkdir(parents=True, exist_ok=True)
    if args.kind == "c":
        if not args.build or not args.otlp_source:
            parser.error("C packaging needs --build and --otlp-source")
        path = package_c(tag, args.build, args.otlp_source, args.output)
    else:
        if not args.tarball:
            parser.error("TypeScript packaging needs --tarball")
        path = package_typescript(tag, args.tarball, args.output)
    print(path)


if __name__ == "__main__":
    main()
