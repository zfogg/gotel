"""Build, validate and publish the multi-language GitHub Pages documentation."""

import argparse
import html
import importlib
import json
import os
import pkgutil
import re
import subprocess
import sys
import time
import uuid
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote, urljoin, urlsplit
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[1]
CONFIG = json.loads((ROOT / "docs/site.json").read_text(encoding="utf-8"))
GENERATORS = {"c": "Doxygen", "python": "pdoc", "go": "Golds", "typescript": "TypeDoc"}


def run(*command, cwd=ROOT, capture=False):
    return subprocess.run(command, cwd=cwd, check=True, text=True, encoding="utf-8", capture_output=capture).stdout


def revision():
    return run("git", "rev-parse", "HEAD", capture=True).strip()


def build_python(output):
    import pdoc
    import pdoc.render

    package = importlib.import_module(CONFIG["package"])
    modules = [CONFIG["package"]] + sorted(
        info.name
        for info in pkgutil.walk_packages(package.__path__, package.__name__ + ".")
        if all(not part.startswith("_") for part in info.name.split("."))
    )
    pdoc.render.configure(
        docformat="markdown",
        show_source=True,
        footer_text=f"{CONFIG['name']} Python API",
        template_directory=ROOT / "docs/pdoc",
    )
    pdoc.pdoc(*modules, output_directory=output / "python")


def build_go(output):
    run(
        "go",
        "run",
        "go101.org/golds@v0.8.7",
        "-gen",
        "-s",
        "-compact",
        "-nostats",
        "-source-code-reading=external",
        "-wdpkgs-listing=solo",
        "-render-doclinks",
        "-footer=simple",
        "-theme=light",
        f"-dir={output / 'go'}",
        "./...",
        cwd=ROOT / CONFIG["go_directory"],
    )


def build_typescript(output):
    # pnpm.cmd must be resolved explicitly on Windows when shell=False.
    run("pnpm.cmd" if sys.platform == "win32" else "pnpm", "exec", "typedoc", "--out", str(output / "typescript"))


def assemble(output):
    output.mkdir(parents=True, exist_ok=True)
    (output / "site.css").write_bytes((ROOT / "docs/site.css").read_bytes())
    name, description, repository = (html.escape(CONFIG[key]) for key in ("name", "description", "repository"))
    cards = "".join(
        f'<a class="language" href="{key}/"><strong>{label}</strong><span>{GENERATORS[key]} API reference →</span></a>'
        for key, label in CONFIG["languages"].items()
    )
    (output / "index.html").write_text(
        f'''<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="description" content="{description}"><title>{name} documentation</title>
<link rel="stylesheet" href="site.css"></head><body class="docs-home"><main>
<p class="eyebrow">Developer documentation</p><h1>{name}</h1><p class="lead">{description}</p>
<div class="languages">{cards}</div><h2>One project. Every language.</h2>
<p>Choose a language for its public types, functions and usage guidance.
These references are generated from the source and published together on every main-branch update.</p>
<p><a href="https://github.com/{repository}#readme">Getting started &amp; command-line guide</a> ·
<a href="https://github.com/{repository}">Source on GitHub</a></p>
<footer>Built from <a href="https://github.com/{repository}/commit/{revision()}">{revision()[:12]}</a>.
MIT licensed.</footer></main></body></html>\n''',
        encoding="utf-8",
    )
    manifest = {
        "project": CONFIG["name"],
        "revision": revision(),
        "languages": CONFIG["languages"],
        "smoke": CONFIG["smoke"],
    }
    (output / "build.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    (output / ".nojekyll").touch()
    # Relative links preserve GitHub Pages' /repository/ prefix and work locally.
    for language in CONFIG["languages"]:
        for page in (output / language).rglob("*.html"):
            text = page.read_text(encoding="utf-8")
            if 'id="language-nav"' in text:
                continue
            prefix = "../" * len(page.relative_to(output).parent.parts)
            links = f'<a href="{prefix}index.html">{CONFIG["name"]} docs</a>' + "".join(
                f'<a href="{prefix}{key}/">{label}</a>' for key, label in CONFIG["languages"].items()
            )
            text = re.sub(r"</head>", f'<link rel="stylesheet" href="{prefix}site.css"></head>', text, count=1)
            text = re.sub(
                r"</body>",
                f'<nav id="language-nav" aria-label="Documentation languages">{links}</nav></body>',
                text,
                count=1,
            )
            page.write_text(text, encoding="utf-8")


class Links(HTMLParser):
    def __init__(self, text):
        super().__init__()
        self.targets = []
        self.feed(text)

    def handle_starttag(self, tag, attrs):
        for key, value in attrs:
            if value and (key == "href" or (key == "src" and tag in ("script", "img", "iframe"))):
                self.targets.append(value)


def verify_local(output):
    required = ["index.html", "site.css", "build.json", ".nojekyll"]
    for language, smoke in CONFIG["smoke"].items():
        required.append(f"{language}/index.html")
        required.extend(f"{language}/{page}" for page in ([smoke] if isinstance(smoke, str) else smoke))
    for path in required:
        if not (output / path).is_file():
            raise ValueError(f"Missing documentation output: {path}")
    broken = []
    known = {}
    pages = list(output.rglob("*.html"))
    for page in pages:
        for target in Links(page.read_text(encoding="utf-8")).targets:
            url = urlsplit(target)
            if url.scheme or url.netloc or not url.path:
                continue
            key = os.path.abspath(page.parent / unquote(url.path))
            if key not in known:
                dest = Path(key).resolve()
                known[key] = dest.is_relative_to(output) and (dest.is_file() or (dest / "index.html").is_file())
            if not known[key]:
                broken.append(f"{page.relative_to(output)} -> {target}")
    if broken:
        raise ValueError("Broken local documentation links:\n" + "\n".join(broken[:30]))
    print(f"Verified {len(pages)} HTML pages, all local links/assets and every language entry point.", flush=True)


def fetch(url):
    with urlopen(Request(url, headers={"User-Agent": "documentation-verifier"}), timeout=30) as response:
        if response.status != 200:
            raise ValueError(f"HTTP {response.status}: {url}")
        return response.read()


def verify_public(url, expected, wait):
    url = url.rstrip("/") + "/"
    deadline = time.monotonic() + wait
    while True:
        try:
            manifest = json.loads(fetch(urljoin(url, "build.json")))
            if manifest["revision"] != expected:
                raise ValueError(f"Published revision {manifest['revision']} does not match {expected}")
            home = fetch(url).decode()
            fetch(urljoin(url, "site.css"))
            for language, smoke in CONFIG["smoke"].items():
                if f'href="{language}/"' not in home:
                    raise ValueError(f"Landing page does not link {language}")
                entry = fetch(urljoin(url, f"{language}/")).decode()
                if not entry or "language-nav" not in entry:
                    raise ValueError(f"Missing navigation at {language}/")
                for path in [smoke] if isinstance(smoke, str) else smoke:
                    page = fetch(urljoin(url, f"{language}/{path}")).decode()
                    if "language-nav" not in page:
                        raise ValueError(f"Missing API/navigation at {language}/{path}")
            print(f"Verified live documentation at {url} ({expected[:12]})", flush=True)
            return
        except (OSError, ValueError, KeyError) as error:
            if time.monotonic() >= deadline:
                raise
            print(f"Waiting for Pages: {error}", flush=True)
            time.sleep(5)


def publish(ref):
    if ref != "main":
        raise ValueError("docs-publish deploys remote main only; dispatch the workflow directly for other build refs")
    repo = CONFIG["repository"]
    request_id = str(uuid.uuid4())
    run("gh", "workflow", "run", "docs.yml", "--repo", repo, "--ref", ref, "-f", f"request_id={request_id}")
    deadline = time.monotonic() + 120
    while time.monotonic() < deadline:
        runs = json.loads(
            run(
                "gh",
                "run",
                "list",
                "--repo",
                repo,
                "--workflow",
                "docs.yml",
                "--event",
                "workflow_dispatch",
                "--json",
                "databaseId,displayTitle,headSha",
                capture=True,
            )
        )
        matches = [item for item in runs if request_id in item["displayTitle"]]
        if matches:
            item = matches[0]
            run("gh", "run", "watch", str(item["databaseId"]), "--repo", repo, "--exit-status", "--interval", "10")
            pages = json.loads(run("gh", "api", f"repos/{repo}/pages", capture=True))
            verify_public(pages["html_url"], item["headSha"], 180)
            return
        time.sleep(3)
    raise TimeoutError("The dispatched documentation workflow did not appear within 120 seconds")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=["python", "go", "typescript", "site", "verify", "publish"])
    parser.add_argument("--output", type=Path, default=ROOT / "build/docs/site")
    parser.add_argument("--url")
    parser.add_argument("--revision", default=None)
    parser.add_argument("--wait", type=int, default=0)
    parser.add_argument("--ref", default="main")
    args = parser.parse_args()
    output = args.output.resolve()
    if args.command == "verify":
        if args.url:
            verify_public(args.url, args.revision or revision(), args.wait)
        else:
            verify_local(output)
    elif args.command == "publish":
        publish(args.ref)
    else:
        {"python": build_python, "go": build_go, "typescript": build_typescript, "site": assemble}[args.command](output)


if __name__ == "__main__":
    main()
