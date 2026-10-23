"""Require a single Spaniel trace with linked browser, Go, and C spans."""
import json
import time
from urllib.request import urlopen

SPANIEL = "http://spaniel:8080"
SERVICES = {"gotel-demo-web", "gotel-demo-api", "gotel-demo-worker"}


def data(path):
    with urlopen(SPANIEL + path, timeout=5) as response:
        return json.load(response)["data"]


deadline = time.monotonic() + 60
last = None
while time.monotonic() < deadline:
    for trace in data("/api/traces?limit=100"):
        spans = data("/api/traces/" + trace["trace_id"])
        selected = [span for span in spans if span["service_name"] in SERVICES]
        names = {span["service_name"] for span in selected}
        if names != SERVICES:
            last = (trace["trace_id"], names)
            continue
        by_id = {span["span_id"]: span for span in selected}
        browser = next((span for span in selected if span["name"].startswith("demo.web.http.client.POST")), None)
        api = next((span for span in selected if span["name"].startswith("demo.api.http.server.POST")), None)
        call = next((span for span in selected if span["name"] == "demo.api.run_wordcount"), None)
        worker = next((span for span in selected if span["name"] == "demo.worker.count_words"), None)
        if all((browser, api, call, worker)) and api["parent_span_id"] == browser["span_id"] \
                and call["parent_span_id"] == api["span_id"] \
                and worker["parent_span_id"] == call["span_id"]:
            assert all(span["span_id"] in by_id for span in (browser, api, call, worker))
            print("Verified three services and browser → Go → C lineage in trace", trace["trace_id"])
            print("Open http://localhost:18080/traces/" + trace["trace_id"] + "?view=waterfall")
            raise SystemExit(0)
        last = (trace["trace_id"], names, [(span["name"], span["parent_span_id"]) for span in selected])
    time.sleep(2)
raise SystemExit("No linked three-service trace in Spaniel: " + repr(last))
