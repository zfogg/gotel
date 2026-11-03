import json
from concurrent.futures import ThreadPoolExecutor

import pytest
import sequel

from gotel import sqltrace
from gotel._sqlanalysis import _Cache, _size


@pytest.fixture(autouse=True)
def reset_cache():
    sqltrace.configure_inspection_cache()
    yield
    sqltrace.configure_inspection_cache()


@pytest.mark.parametrize(
    ("sql", "policy", "reason"),
    [
        ("", {}, "empty"),
        ("SELECT 'private-value'", {"omit_query_text": True}, "policy"),
        ("SELECT 'private-value", {}, "malformed"),
        ("SELECT 'private-value'", {"max_work_units": 1}, "analysis_limit"),
        ("SELECT 'private-value'", {"cancelled": lambda: True}, "cancelled"),
    ],
)
def test_omission_reasons_without_values(sql, policy, reason):
    attrs = sqltrace._attributes(sql, sequel.Dialect.POSTGRESQL, "postgresql", "test", policy)
    assert attrs["gotel.db.query.text_reason"] == reason
    assert "db.query.text" not in attrs
    assert "private-value" not in json.dumps(attrs)
    assert attrs["gotel.db.query.original_length"] == len(sql.encode())
    assert attrs["gotel.db.query.diagnostic_schema_version"] == 1


def test_opt_in_maps_binds_and_fingerprint():
    sql = "-- 🔒 secret\r\nSELECT id FROM widgets WHERE id=$1 OR parent=$1"
    policy = {"include_source_maps": True, "include_binds": True}
    attrs = sqltrace._attributes(sql, sequel.Dialect.POSTGRESQL, "postgresql", "test", policy)
    envelope = json.loads(attrs["gotel.db.query.source_map"])
    assert envelope["schema_version"] == 1
    assert envelope["original_to_sanitized"]["from_length"] == len(sql.encode())
    final_map = (
        envelope["sanitized_to_formatted"] if envelope["display"] == "formatted" else envelope["original_to_sanitized"]
    )
    assert final_map["to_length"] == len(attrs["db.query.text"].encode())
    descriptors = [sequel.BindDescriptor(**item) for item in json.loads(attrs["gotel.db.query.binds"])]
    sequel.check_binds(descriptors, 1)
    with pytest.raises(ValueError):
        sequel.check_binds(descriptors, 2)
    assert "secret" not in json.dumps(attrs)
    assert attrs["gotel.db.query.fingerprint"].startswith("sequel:v1:postgresql:")
    default = sqltrace._attributes(sql, sequel.Dialect.POSTGRESQL, "postgresql", "test", {})
    assert "gotel.db.query.source_map" not in default
    assert "gotel.db.query.binds" not in default


def test_oversized_transport_maps_are_explicitly_omitted():
    sql = "SELECT " + ",".join(f"col{i}" for i in range(600)) + " FROM widgets"
    attrs = sqltrace._attributes(sql, sequel.Dialect.POSTGRESQL, "postgresql", "", {"include_source_maps": True})
    assert attrs["gotel.db.query.source_map_omitted"] is True
    assert "gotel.db.query.source_map" not in attrs
    assert attrs["db.query.text"]


def test_diagnostics_transport_retains_bounded_canonical_prefix():
    attrs = sqltrace._attributes("SELECT );" * 100, sequel.Dialect.POSTGRESQL, "postgresql", "", {})
    diagnostics = attrs["db.query.diagnostics"]
    assert 0 < len(diagnostics) <= 64
    assert sum(len(item.encode()) for item in diagnostics) <= 16384
    assert attrs["gotel.db.query.diagnostics_omitted"] is True
    assert attrs["gotel.db.query.text_reason"] == "malformed"


def test_cache_byte_eviction_bypass_and_copies():
    key = ("SELECT 1", "generic", ())
    value = {"diagnostics": ["safe"]}
    unit = _size((key, value)) + 256
    cache = _Cache(unit * 2)
    cache.put(key, value)
    cache.put(("SELECT 2", "generic", ()), value)
    assert cache.get(key) == value
    cache.put(("SELECT 3", "generic", ()), value)
    assert cache.get(("SELECT 2", "generic", ())) is None
    cache.put(("x" * unit * 3,), value)
    got = cache.get(key)
    assert got is not None
    got["diagnostics"].append("mutation")
    assert cache.get(key) == value
    stats = cache.stats()
    assert stats.entries == 2 and stats.bytes <= stats.max_bytes
    assert stats.evictions == 1 and stats.bypasses == 1
    cache.configure(0)
    assert cache.stats().bytes == 0 and cache.stats().entries == 0


def test_cache_concurrent_reset_and_cancellation():
    def inspect(index):
        sqltrace._attributes("SELECT 1", sequel.Dialect.GENERIC, "sqlite", "", {})
        sqltrace.inspection_cache_stats()
        if index % 3 == 0:
            sqltrace.configure_inspection_cache(4096)

    with ThreadPoolExecutor(max_workers=4) as pool:
        list(pool.map(inspect, range(30)))
    attrs = sqltrace._attributes("SELECT 1", sequel.Dialect.GENERIC, "sqlite", "", {"cancelled": lambda: True})
    assert attrs["gotel.db.query.text_reason"] == "cancelled"
    assert sqltrace._attributes("SELECT 1", sequel.Dialect.GENERIC, "sqlite", "", {})["db.query.text"]
