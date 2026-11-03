"""Source and runtime checks for application-owned SQL operation names.

Python uses the standard AST; C uses Clang's typed AST via ``check_c``. Neither
checker executes application code or logs SQL. Dynamic sites require an explicit
review or a runtime Registry check; they are never guessed to be static.
"""

from __future__ import annotations

import ast
import re
from collections.abc import Iterable
from dataclasses import dataclass, field
from pathlib import Path
from threading import Lock

import sequel

_IDENTIFIER = re.compile(r"^[A-Za-z][A-Za-z0-9_]*$")
_FINGERPRINT = re.compile(r"^sequel:v1:(generic|postgresql|duckdb|sqlite|mysql|clickhouse):[a-f0-9]{64}$")


class NamingError(ValueError):
    """A naming boundary violation containing no SQL or argument values."""


class Registry:
    """Bounded concurrent name-to-fingerprint identity checks for dynamic SQL."""

    def __init__(self, max_entries: int = 4096, *, check_duplicate_definitions: bool = True):
        if max_entries < 1:
            raise ValueError("max_entries must be positive")
        self.max_entries = max_entries
        self.check_duplicate_definitions = check_duplicate_definitions
        self._names: dict[str, str] = {}
        self._shapes: dict[str, str] = {}
        self._lock = Lock()

    def observe(self, name: str, sql: str, *, dialect: sequel.Dialect = sequel.Dialect.GENERIC) -> None:
        """Reject missing names, malformed SQL, or conflicting canonical identities."""
        try:
            fingerprint = sequel.fingerprint(sql, dialect=dialect)
        except (ValueError, TypeError):
            raise NamingError("query identity unavailable; manual review required") from None
        self.observe_fingerprint(name, fingerprint)

    def observe_fingerprint(self, name: str, fingerprint: str) -> None:
        """Check an already-analyzed versioned identity; no SQL is retained."""
        if not _IDENTIFIER.fullmatch(name):
            raise NamingError("query name must be a static identifier")
        if not _FINGERPRINT.fullmatch(fingerprint):
            raise NamingError("query identity unavailable; manual review required")
        with self._lock:
            prior = self._names.get(name)
            if prior is not None:
                if prior != fingerprint:
                    raise NamingError(f"query name {name} is reused for a different definition")
                return
            if self.check_duplicate_definitions and fingerprint in self._shapes:
                raise NamingError(f"query {name} duplicates the definition of {self._shapes[fingerprint]}")
            if len(self._names) >= self.max_entries:
                raise NamingError("query identity registry capacity exceeded")
            self._names[name] = fingerprint
            self._shapes[fingerprint] = name


@dataclass(frozen=True)
class Issue:
    """One source-located policy violation, without SQL text."""

    path: str
    line: int
    code: str
    message: str

    def __str__(self) -> str:
        return f"{self.path}:{self.line}: {self.message} [{self.code}]"


@dataclass
class Report:
    """Aggregated static checks. A nonempty issues list fails the boundary."""

    count: int = 0
    issues: list[Issue] = field(default_factory=list)
    sites: list[Site] = field(default_factory=list)

    def raise_for_issues(self) -> None:
        if self.issues:
            raise NamingError("\n".join(map(str, self.issues)))


@dataclass(frozen=True)
class Site:
    """A checked source operation suitable for a generated query catalog."""

    name: str | None
    path: str
    line: int
    method: str


@dataclass(frozen=True)
class PythonConfig:
    """Configure executor and naming APIs using resolved import-qualified names.

    Bare executor names match methods on any receiver; qualified names match the
    resolved import alias or source spelling exactly. Exclude test/generated
    paths at the caller rather than hiding unchecked source in this library.
    ``allow_dynamic_sql`` still requires a static name and delegates identity
    validation to a runtime Registry. Its default is fail-closed manual review.
    """

    executors: tuple[str, ...] = ("execute", "executemany", "executescript", "query", "command")
    wrappers: tuple[str, ...] = ("gotel.sqltrace.call", "gotel.sqltrace.call_sync")
    scopes: tuple[str, ...] = ("gotel.sqltrace.name",)
    sql_constructors: tuple[str, ...] = ("sqlalchemy.text",)
    dialect: sequel.Dialect = sequel.Dialect.GENERIC
    allow_dynamic_sql: bool = False
    pascal_case: bool = False
    check_duplicate_definitions: bool = True
    # Only awaited direct calls match these methods; named wrappers still match
    # all executors. This distinguishes e.g. async ORM get from mapping.get.
    awaited_executors: tuple[str, ...] = ()


@dataclass(frozen=True)
class CExecutor:
    """One C executor and the zero-based SQL argument position."""

    name: str
    sql_argument: int = 1


@dataclass(frozen=True)
class CConfig:
    """Clang invocation and typed catalog/executor rules for C source.

    ``clang_args`` accepts the same -D/-I/-std options as a normal compilation.
    Compilation errors fail the check without exposing compiler source snippets.
    Forwarded SQL parameters are resolved through call sites in the translation
    unit; unresolved external callers require review or runtime validation.
    """

    executors: tuple[CExecutor, ...] = (
        CExecutor("sqlite3_prepare_v2"),
        CExecutor("sqlite3_prepare_v3"),
        CExecutor("sqlite3_exec"),
        CExecutor("gotel_sqlite_prepare"),
        CExecutor("gotel_sqlite_prepare_v2"),
        CExecutor("gotel_sqlite_prepare_v3"),
    )
    catalog_types: tuple[str, ...] = ("gotel_query_t",)
    clang: str = "clang"
    clang_args: tuple[str, ...] = ()
    dialect: sequel.Dialect = sequel.Dialect.SQLITE
    timeout_seconds: float = 60
    allow_dynamic_sql: bool = False
    check_duplicate_definitions: bool = True


def _dotted(node: ast.expr, imports: dict[str, str]) -> str:
    if isinstance(node, ast.Name):
        return imports.get(node.id, node.id)
    if isinstance(node, ast.Attribute):
        return _dotted(node.value, imports) + "." + node.attr
    return ""


def _matches(name: str, rules: tuple[str, ...]) -> bool:
    return name in rules or any("." not in rule and name.rsplit(".", 1)[-1] == rule for rule in rules)


def check_python(paths: Iterable[str | Path], config: PythonConfig = PythonConfig()) -> Report:
    """Check source-owned naming, duplicate definitions and dynamic SQL sites.

    Names on ``with sqltrace.name(...)`` and ``call[_sync](name, execute, sql)``
    apply only to executions in that lexical scope/call. A naming call hidden
    in a bind argument does not label its enclosing execution.
    """
    report = Report()
    registry = Registry(check_duplicate_definitions=config.check_duplicate_definitions)
    sites: dict[str, str] = {}
    for path_value in paths:
        path = Path(path_value)
        try:
            tree = ast.parse(path.read_text(encoding="utf-8"), filename=str(path))
        except (OSError, SyntaxError, UnicodeError):
            report.issues.append(Issue(str(path), 1, "source_error", "source could not be parsed"))
            continue
        owners: dict[ast.AST, ast.AST] = {}
        outer: dict[ast.AST, ast.AST] = {}
        bindings: dict[ast.AST, dict[str, list[ast.expr | None]]] = {tree: {}}
        imported: dict[ast.AST, dict[str, str]] = {tree: {}}

        class Bindings(ast.NodeVisitor):
            scope: ast.AST = tree

            def visit(self, node: ast.AST):
                owners[node] = self.scope
                return super().visit(node)

            def visit_FunctionDef(self, node: ast.FunctionDef | ast.AsyncFunctionDef):
                bindings[self.scope].setdefault(node.name, []).append(None)
                previous = self.scope
                # Python methods do not close over the class namespace.
                outer[node] = outer[previous] if isinstance(previous, ast.ClassDef) else previous
                bindings[node], imported[node] = {}, {}
                self.scope = node
                for arg in (*node.args.posonlyargs, *node.args.args, *node.args.kwonlyargs):
                    bindings[node][arg.arg] = [None]
                for arg in (node.args.vararg, node.args.kwarg):
                    if arg:
                        bindings[node][arg.arg] = [None]
                self.generic_visit(node)
                self.scope = previous

            def visit_AsyncFunctionDef(self, node: ast.AsyncFunctionDef):
                self.visit_FunctionDef(node)

            def visit_ClassDef(self, node: ast.ClassDef):
                bindings[self.scope].setdefault(node.name, []).append(None)
                previous = self.scope
                outer[node] = previous
                bindings[node], imported[node] = {}, {}
                self.scope = node
                self.generic_visit(node)
                self.scope = previous

            def unknown_target(self, target: ast.AST):
                for child in ast.walk(target):
                    if isinstance(child, ast.Name) and isinstance(child.ctx, ast.Store):
                        bindings[self.scope].setdefault(child.id, []).append(None)

            def visit_For(self, node: ast.For | ast.AsyncFor):
                self.unknown_target(node.target)
                self.generic_visit(node)

            def visit_AsyncFor(self, node: ast.AsyncFor):
                self.visit_For(node)

            def visit_AugAssign(self, node: ast.AugAssign):
                self.unknown_target(node.target)
                self.generic_visit(node)

            def visit_NamedExpr(self, node: ast.NamedExpr):
                self.unknown_target(node.target)
                self.generic_visit(node)

            def visit_Assign(self, node: ast.Assign | ast.AnnAssign):
                targets = node.targets if isinstance(node, ast.Assign) else [node.target]
                for target in targets:
                    if isinstance(target, ast.Name):
                        bindings[self.scope].setdefault(target.id, []).append(node.value)
                    else:
                        self.unknown_target(target)
                self.generic_visit(node)

            def visit_AnnAssign(self, node: ast.AnnAssign):
                self.visit_Assign(node)

            def visit_Import(self, node: ast.Import):
                for alias in node.names:
                    imported[self.scope][alias.asname or alias.name.split(".")[0]] = (
                        alias.name if alias.asname else alias.name.split(".")[0]
                    )

            def visit_ImportFrom(self, node: ast.ImportFrom):
                for alias in node.names:
                    imported[self.scope][alias.asname or alias.name] = (node.module or "") + "." + alias.name

        Bindings().visit(tree)

        def scope_for(expr: ast.AST, name: str) -> ast.AST:
            scope = owners.get(expr, tree)
            while name not in bindings[scope] and name not in imported[scope] and scope in outer:
                scope = outer[scope]
            return scope

        def dotted(expr: ast.expr) -> str:
            if isinstance(expr, ast.Name):
                scope = scope_for(expr, expr.id)
                return expr.id if expr.id in bindings[scope] else imported[scope].get(expr.id, expr.id)
            if isinstance(expr, ast.Attribute):
                return dotted(expr.value) + "." + expr.attr
            return ""

        def literal(expr: ast.expr | None, seen: frozenset[tuple[int, str]] = frozenset()) -> str | None:
            if isinstance(expr, ast.Constant) and isinstance(expr.value, str):
                return expr.value
            if isinstance(expr, ast.BinOp) and isinstance(expr.op, ast.Add):
                left, right = literal(expr.left, seen), literal(expr.right, seen)
                return left + right if left is not None and right is not None else None
            if isinstance(expr, ast.Name):
                scope = scope_for(expr, expr.id)
                key = (id(scope), expr.id)
                if key in seen:
                    return None
                values = [literal(value, seen | {key}) for value in bindings[scope].get(expr.id, [])]
                if values and all(value == values[0] for value in values):
                    return values[0]
            if isinstance(expr, ast.Call) and dotted(expr.func) in config.sql_constructors and expr.args:
                return literal(expr.args[0], seen)
            return None

        def issue(node: ast.AST, code: str, message: str) -> None:
            report.issues.append(Issue(str(path), getattr(node, "lineno", 1), code, message))

        def execution(node: ast.Call, operation: str | None, sql_expr: ast.expr | None, target: str) -> None:
            report.count += 1
            if operation is None:
                issue(node, "unnamed", "execution needs a source-owned static query name")
            elif (
                not _IDENTIFIER.fullmatch(operation)
                or config.pascal_case
                and not re.fullmatch(r"[A-Z][A-Za-z0-9_]*", operation)
            ):
                issue(node, "invalid_name", "query name has invalid identifier syntax")
                operation = None
            report.sites.append(Site(operation, str(path), node.lineno, target))
            if operation is not None:
                where = f"{path}:{node.lineno}:{node.col_offset + 1}"
                if operation in sites and sites[operation] != where:
                    issue(node, "duplicate_site", f"query name {operation} already used at {sites[operation]}")
                else:
                    sites[operation] = where
            sql = literal(sql_expr)
            if sql is None:
                if not config.allow_dynamic_sql:
                    issue(node, "dynamic_sql", "dynamic SQL identity requires manual review or a runtime Registry")
                return
            if operation is not None:
                try:
                    registry.observe(operation, sql, dialect=config.dialect)
                except NamingError as exc:
                    issue(node, "identity", str(exc))

        class Visitor(ast.NodeVisitor):
            operation: str | None = None
            awaited_call: ast.expr | None = None

            def visit_Await(self, node: ast.Await) -> None:
                previous, self.awaited_call = self.awaited_call, node.value
                self.visit(node.value)
                self.awaited_call = previous

            def visit_With(self, node: ast.With | ast.AsyncWith) -> None:
                previous = self.operation
                for item in node.items:
                    call = item.context_expr
                    if isinstance(call, ast.Call) and dotted(call.func) in config.scopes:
                        self.operation = literal(call.args[0]) if call.args else None
                    else:
                        self.visit(call)
                for statement in node.body:
                    self.visit(statement)
                self.operation = previous

            def visit_AsyncWith(self, node: ast.AsyncWith) -> None:
                self.visit_With(node)  # same fields and lexical naming semantics

            def visit_FunctionDef(self, node: ast.FunctionDef) -> None:
                previous, self.operation = self.operation, None
                self.generic_visit(node)
                self.operation = previous

            def visit_AsyncFunctionDef(self, node: ast.AsyncFunctionDef) -> None:
                previous, self.operation = self.operation, None
                self.generic_visit(node)
                self.operation = previous

            def visit_Call(self, node: ast.Call) -> None:
                function = dotted(node.func)
                if function in config.wrappers:
                    operation = literal(node.args[0]) if node.args else None
                    target = dotted(node.args[1]) if len(node.args) > 1 else ""
                    if not _matches(target, config.executors + config.awaited_executors):
                        issue(node, "dynamic_executor", "named wrapper target requires manual review")
                    execution(node, operation, node.args[2] if len(node.args) > 2 else None, target)
                    # The callable argument is not itself an execution. Arguments
                    # can contain actual nested executions and must be checked.
                    for arg in node.args[2:]:
                        self.visit(arg)
                    return
                if (
                    _matches(function, config.executors)
                    or node is self.awaited_call
                    and _matches(function, config.awaited_executors)
                ):
                    sql_expr = (
                        node.args[0]
                        if node.args
                        else next((kw.value for kw in node.keywords if kw.arg in {"statement", "query", "sql"}), None)
                    )
                    execution(node, self.operation, sql_expr, function)
                self.generic_visit(node)

        Visitor().visit(tree)
    return report


def check_c(paths: Iterable[str | Path], config: CConfig = CConfig()) -> Report:
    """Check C source using Clang's typed AST; see :class:`CConfig`."""
    from ._querynames_c import check_c as implementation

    return implementation(paths, config)
