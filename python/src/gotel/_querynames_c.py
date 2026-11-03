"""Clang AST adapter for querynames; source text is never matched with regexes."""

from __future__ import annotations

import ast
import json
import subprocess
from collections.abc import Iterable
from pathlib import Path
from typing import Any

import sequel

from .querynames import CConfig, Issue, NamingError, Registry, Report

Node = dict[str, Any]


def _children(node: Node) -> list[Node]:
    return node.get("inner", [])


def _walk(node: Node):
    yield node
    for child in _children(node):
        yield from _walk(child)


def _unwrap(node: Node) -> Node:
    while node.get("kind") in {"ImplicitCastExpr", "ConstantExpr", "ParenExpr", "CStyleCastExpr", "ExprWithCleanups"}:
        inner = _children(node)
        if len(inner) != 1:
            break
        node = inner[0]
    return node


def _string(node: Node) -> str | None:
    node = _unwrap(node)
    if node.get("kind") != "StringLiteral":
        return None
    try:
        value = ast.literal_eval(node.get("value", ""))
        return value if isinstance(value, str) else None
    except (SyntaxError, ValueError):
        return None


def _callee(node: Node) -> str:
    args = _children(node)
    if args:
        ref = _unwrap(args[0]).get("referencedDecl", {})
        return ref.get("name", "")
    return ""


def check_c(paths: Iterable[str | Path], config: CConfig) -> Report:
    """Use evaluated typed initializers, aliases and forwarding call arguments."""
    report = Report()
    registry = Registry(check_duplicate_definitions=config.check_duplicate_definitions)
    # Fingerprints associate static templates with their source-owned catalog
    # names. Repeated compilation of an included catalog is harmless.
    catalog: dict[str, str] = {}
    for path_value in paths:
        path = Path(path_value).resolve()
        try:
            process = subprocess.run(
                [config.clang, *config.clang_args, "-fsyntax-only", "-Xclang", "-ast-dump=json", str(path)],
                capture_output=True,
                timeout=config.timeout_seconds,
                check=False,
            )
            if process.returncode:
                report.issues.append(
                    Issue(str(path), 1, "source_error", "Clang could not type-check this translation unit")
                )
                continue
            tree: Node = json.loads(process.stdout)
            source = path.read_bytes()
        except (OSError, subprocess.TimeoutExpired, ValueError):
            report.issues.append(
                Issue(str(path), 1, "source_error", "Clang AST unavailable; source check requires manual review")
            )
            continue
        nodes = list(_walk(tree))
        ids = {node["id"]: node for node in nodes if "id" in node}
        assignments: dict[str, list[Node]] = {}
        calls: dict[str, list[Node]] = {}
        parameter_calls: dict[str, list[Node]] = {}
        for node in nodes:
            if node.get("kind") == "VarDecl":
                initializers = _children(node)
                if initializers:
                    assignments.setdefault(node["id"], []).append(initializers[-1])
            elif node.get("kind") == "BinaryOperator" and node.get("opcode") == "=":
                inner = _children(node)
                if len(inner) == 2:
                    ref = _unwrap(inner[0]).get("referencedDecl", {})
                    if "id" in ref:
                        assignments.setdefault(ref["id"], []).append(inner[1])
            elif node.get("kind") == "CallExpr":
                calls.setdefault(_callee(node), []).append(node)
            elif node.get("kind") in {"UnaryOperator", "CompoundAssignOperator"}:
                if node.get("opcode") in {"++", "--", "&", "+=", "-="} and _children(node):
                    ref = _unwrap(_children(node)[0]).get("referencedDecl", {})
                    if "id" in ref:
                        # Mutation or an address escape invalidates a static
                        # initializer, even when later uses look like aliases.
                        assignments.setdefault(ref["id"], []).append({"kind": "UnknownMutation"})
        for node in nodes:
            if node.get("kind") == "FunctionDecl":
                params = [child for child in _children(node) if child.get("kind") == "ParmVarDecl"]
                for index, param in enumerate(params):
                    arguments = []
                    # Only internal linkage gives a closed set of callers in
                    # this translation unit. External functions need review.
                    for call in calls.get(node.get("name", ""), []) if node.get("storageClass") == "static" else []:
                        inner = _children(call)
                        if len(inner) > index + 1:
                            arguments.append(inner[index + 1])
                    parameter_calls[param["id"]] = arguments

        def integer(node: Node, seen: frozenset[str] = frozenset()) -> int | None:
            node = _unwrap(node)
            if node.get("kind") in {"IntegerLiteral", "EnumConstantDecl"}:
                if "value" in node:
                    try:
                        return int(node["value"], 0)
                    except ValueError:
                        return None
                inner = _children(node)
                return integer(inner[-1], seen) if inner else None
            ref = node.get("referencedDecl", {})
            if ref.get("id") in ids and ref["id"] not in seen:
                return integer(ids[ref["id"]], seen | {ref["id"]})
            return None

        arrays: dict[str, list[tuple[str, str]]] = {}
        for node in nodes:
            if node.get("kind") != "VarDecl" or not any(
                kind in node.get("type", {}).get("qualType", "").replace("const ", "").split("[")[0].split()
                for kind in config.catalog_types
            ):
                continue
            initializers = [child for child in _children(node) if child.get("kind") == "InitListExpr"]
            if not initializers:
                continue
            items = _children(initializers[-1])
            # A single record and an array of records are both supported.
            if items and items[0].get("kind") != "InitListExpr":
                items = initializers
            records = []
            for item in items:
                fields = _children(item)
                name = _string(fields[0]) if fields else None
                sql = _string(fields[1]) if len(fields) > 1 else None
                if name is None or sql is None:
                    report.issues.append(
                        Issue(
                            str(path), 1, "dynamic_catalog", "query catalog requires static name and SQL initializers"
                        )
                    )
                    continue
                records.append((name, sql))
                try:
                    registry.observe(name, sql, dialect=config.dialect)
                    catalog[sequel.fingerprint(sql, dialect=config.dialect)] = name
                except NamingError as exc:
                    report.issues.append(Issue(str(path), 1, "identity", str(exc)))
            arrays[node["id"]] = records

        def resolve(node: Node, seen: frozenset[str] = frozenset()) -> list[str] | None:
            node = _unwrap(node)
            value = _string(node)
            if value is not None:
                return [value]
            if node.get("kind") == "MemberExpr" and node.get("name") == "sql" and _children(node):
                base = _unwrap(_children(node)[0])
                if base.get("kind") == "ArraySubscriptExpr" and len(_children(base)) == 2:
                    array, index = _children(base)
                    ref = _unwrap(array).get("referencedDecl", {})
                    records = arrays.get(ref.get("id", ""))
                    if records is not None:
                        selected = integer(index)
                        if selected is not None:
                            return [records[selected][1]] if 0 <= selected < len(records) else None
                        # An unknown index still selects only named catalog
                        # members. Bounds checking remains the C program's job.
                        return [sql for _, sql in records]
                ref = base.get("referencedDecl", {})
                records = arrays.get(ref.get("id", ""))
                if records is not None:
                    return [sql for _, sql in records]
            ref = node.get("referencedDecl", {})
            key = ref.get("id")
            if key and key not in seen:
                expressions = assignments.get(key, parameter_calls.get(key, []))
                result = []
                for expression in expressions:
                    values = resolve(expression, seen | {key})
                    if values is None:
                        return None
                    result.extend(values)
                return result or None
            return None

        rules = {rule.name: rule for rule in config.executors}

        def visit(node: Node, main_file: bool = True) -> None:
            location = node.get("loc", {})
            if "file" in location:
                main_file = Path(location["file"]).resolve() == path
            elif "includedFrom" in location:
                main_file = False
            if node.get("kind") == "CallExpr" and main_file and _callee(node) in rules:
                report.count += 1
                begin = node.get("range", {}).get("begin", {})
                begin = begin.get("expansionLoc", begin)
                offset = begin.get("offset", 0)
                line = source[:offset].count(b"\n") + 1
                rule = rules[_callee(node)]
                arguments = _children(node)[1:]
                values = resolve(arguments[rule.sql_argument]) if 0 <= rule.sql_argument < len(arguments) else None
                if values is None:
                    if not config.allow_dynamic_sql:
                        report.issues.append(
                            Issue(
                                str(path),
                                line,
                                "dynamic_sql",
                                "dynamic SQL identity requires manual review or a runtime Registry",
                            )
                        )
                else:
                    for sql in set(values):
                        try:
                            identity = sequel.fingerprint(sql, dialect=config.dialect)
                        except (ValueError, TypeError):
                            identity = ""
                        if identity not in catalog:
                            report.issues.append(
                                Issue(str(path), line, "unnamed", "execution needs a source-owned query catalog entry")
                            )
            for child in _children(node):
                visit(child, main_file)

        visit(tree)
    return report
