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


def archive_name(tag: str, linkage: str, expected_arch: str | None = None) -> str:
    system = {"Linux": "linux", "Darwin": "macos", "Windows": "windows"}[platform.system()]
    machine = platform.machine().lower()
    arch = {"x86_64": "x64", "amd64": "x64", "aarch64": "arm64", "arm64": "arm64"}.get(machine, machine)
    if expected_arch is not None and arch != expected_arch:
        raise ValueError(f"runner architecture {arch} does not match requested {expected_arch}")
    return f"gotel-{tag}-{system}-{arch}-{linkage}"


def add_file(archive: zipfile.ZipFile, source: Path, destination: str) -> None:
    if not source.is_file():
        raise FileNotFoundError(source)
    archive.write(source, destination)


def package_c(tag: str, build: Path, otlp_source: Path, output: Path,
              linkage: str, expected_arch: str | None = None,
              nghttp2_source: Path | None = None) -> Path:
    if 'GOTEL_WITH_GRPC:BOOL=ON' not in (build / 'CMakeCache.txt').read_text(encoding='utf-8'):
        raise ValueError('C release libraries must be built with GOTEL_WITH_GRPC=ON')
    nghttp2_source = nghttp2_source or build / '_deps' / 'gotel_nghttp2-src'
    name = archive_name(tag, linkage, expected_arch)
    path = output / f"{name}.zip"
    system = platform.system()
    readme = ROOT / "c" / "RELEASE.md"
    if linkage == "static":
        suffix = ".lib" if system == "Windows" else ".a"
        gotel_libs = [build / "src" / ("gotel.lib" if system == "Windows" else "libgotel.a")]
        gotel_libs.append(build / "_deps" / "otlp_c-build" / ("otlp_c.lib" if system == "Windows" else "libotlp_c.a"))
        gotel_libs.append(build / "_deps" / "gotel_nghttp2-build" / "lib" /
                          ("nghttp2.lib" if system == "Windows" else "libnghttp2.a"))
    elif system == "Windows":
        gotel_libs = [build / "src" / "gotel.dll", build / "src" / "gotel.lib"]
    else:
        extension = ".dylib" if system == "Darwin" else ".so"
        pattern = "libgotel*.dylib" if system == "Darwin" else "libgotel.so*"
        gotel_libs = sorted((build / "src").glob(pattern))
        if not gotel_libs:
            raise FileNotFoundError(build / "src" / f"libgotel{extension}")
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as archive:
        for source, destination in (
            (ROOT / "LICENSE", "LICENSE"),
            (readme, "README.md"),
            (ROOT / "c" / "README.md", "API.md"),
            (otlp_source / "LICENSE", "third-party/otlp-c-LICENSE"),
            (nghttp2_source / "COPYING", "third-party/nghttp2-COPYING"),
        ):
            add_file(archive, source, f"{name}/{destination}")
        for source in gotel_libs:
            add_file(archive, source, f"{name}/lib/{source.name}")
        for prefix, directory in (
            ("include/gotel", ROOT / "c" / "include" / "gotel"),
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
            destination = "PROJECT.md" if relative.as_posix() == "package/README.md" else "/".join(relative.parts[1:])
            archive.writestr(f"{name}/{destination}", data)
        add_file(archive, ROOT / "typescript" / "RELEASE.md", f"{name}/README.md")
    return path


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("kind", choices=("c", "typescript"))
    parser.add_argument("--tag", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--build", type=Path)
    parser.add_argument("--otlp-source", type=Path)
    parser.add_argument("--nghttp2-source", type=Path)
    parser.add_argument("--tarball", type=Path)
    parser.add_argument("--expected-arch", choices=("x64", "arm64"))
    parser.add_argument("--linkage", choices=("static", "shared"), default="static")
    args = parser.parse_args()
    tag = require_tag(args.tag)
    args.output.mkdir(parents=True, exist_ok=True)
    if args.kind == "c":
        if not args.build or not args.otlp_source:
            parser.error("C packaging needs --build and --otlp-source")
        path = package_c(tag, args.build, args.otlp_source, args.output,
                         args.linkage, args.expected_arch, args.nghttp2_source)
    else:
        if not args.tarball:
            parser.error("TypeScript packaging needs --tarball")
        path = package_typescript(tag, args.tarball, args.output)
    print(path)


if __name__ == "__main__":
    main()
