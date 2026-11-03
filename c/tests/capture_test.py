"""Decode real in-process captures without starting any network receiver."""

import pathlib
import struct
import subprocess
import sys
import tempfile

from gotel_testing import protobuf, records, service_names


def main() -> None:
    with tempfile.TemporaryDirectory() as directory:
        output = pathlib.Path(directory) / "capture.pb"
        for mode, expected in (("normal", 4), ("concurrent", 82), ("snapshot", 6)):
            subprocess.run([sys.argv[1], mode, str(output)], check=True, timeout=20)
            requests = [("/v1/traces", output.read_bytes())]
            assert service_names(requests) == {"capture-one", "capture-two"}
            spans = records(requests, "/v1/traces")
            assert len(spans) == expected
            assert not any(span[5] == [b"capture.hot"] for span in spans)
            children = [span for span in spans if span[5] == [b"capture.child"]]
            for child in children:
                parent = next(span for span in spans if span[2] == child[4])
                assert parent[1] == child[1]
                assert parent[5] == [b"capture.root"]
                attrs = {
                    protobuf(value)[1][0]: protobuf(protobuf(value)[2][0])
                    for value in child[9]
                }
                assert attrs[b"capture.integer"][3] == [42]
                assert attrs[b"capture.string"][1] == [
                    'quoted "value"\nutf8 😀'.encode()
                ]
                event = protobuf(child[11][0])
                assert event[2] == [b"capture.event"]
                link = protobuf(child[13][0])
                assert link[1] == parent[1] and link[2] == parent[2]
                assert protobuf(child[15][0])[3] == [2]
            if mode == "snapshot":
                snapshot = next(
                    span
                    for span in spans
                    if any(
                        protobuf(value)[1] == [b"gotel.span.snapshot"]
                        for value in span.get(9, [])
                    )
                )
                assert (
                    struct.unpack("<Q", snapshot[8][0])[0]
                    - struct.unpack("<Q", snapshot[7][0])[0]
                    == 1000000000
                )
        subprocess.run([sys.argv[1], "overflow", str(output)], check=True, timeout=20)


if __name__ == "__main__":
    main()
