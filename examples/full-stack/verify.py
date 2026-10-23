"""Require a single Spaniel trace with linked browser, Go, and C spans."""
import json
import re
import time
from pathlib import Path
from urllib.request import urlopen

SPANIEL = "http://spaniel:8080"
SERVICES = {"gotel-demo-web", "gotel-demo-api", "gotel-demo-worker"}
trace_id = Path("/shared/trace-id").read_text().strip()
if not re.fullmatch(r"[0-9a-f]{32}", trace_id):
    raise SystemExit("Runner did not provide a valid trace ID")


def data(path):
    with urlopen(SPANIEL + path, timeout=5) as response:
        return json.load(response)["data"]


deadline = time.monotonic() + 60
last = None
while time.monotonic() < deadline:
    spans = data("/api/traces/" + trace_id)
    selected = [span for span in spans if span["service_name"] in SERVICES]
    names = {span["service_name"] for span in selected}
    browser = next((span for span in selected if span["service_name"] == "gotel-demo-web" and span["name"] == "demo.web.http.client.POST /api/count"), None)
    api = next((span for span in selected if span["service_name"] == "gotel-demo-api" and span["name"] == "demo.api.http.server.POST /api/count"), None)
    call = next((span for span in selected if span["service_name"] == "gotel-demo-api" and span["name"] == "demo.api.run_wordcount"), None)
    worker = next((span for span in selected if span["service_name"] == "gotel-demo-worker" and span["name"] == "demo.worker.count_words"), None)
    if names == SERVICES and all((browser, api, call, worker)) \
            and browser["parent_span_id"] == "" \
            and api["parent_span_id"] == browser["span_id"] \
            and call["parent_span_id"] == api["span_id"] \
            and worker["parent_span_id"] == call["span_id"]:
        print("Verified three services and browser → Go → C lineage in trace", trace_id)
        print("Open http://localhost:18080/traces/" + trace_id + "?view=waterfall")
        raise SystemExit(0)
    last = (names, [(span["name"], span["parent_span_id"]) for span in selected])
    time.sleep(2)
raise SystemExit("No linked three-service trace in Spaniel for " + trace_id + ": " + repr(last))
