"""Compile the bundled SQL engine; no CMake or telemetry C library required."""

from setuptools import Extension, setup

setup(
    ext_modules=[
        Extension(
            "gotel._sql",
            sources=[
                "src/gotel/_sql.c",
                "vendor/engine/src/sql/main.c",
                "vendor/engine/src/utf8.c",
                "vendor/utf8proc.c",
            ],
            include_dirs=["vendor/engine/include", "vendor"],
            define_macros=[("GOTEL_SQL_STANDALONE", "1"), ("UTF8PROC_STATIC", "1")],
        )
    ]
)
