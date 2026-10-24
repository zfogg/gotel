def inspect(
    sql: str,
    dialect: int,
    max_query_text_bytes: int,
    omit_query_text: bool,
    keep_in_lists: bool,
) -> tuple[str, str, str, bool, int]: ...
def format(sql: str, dialect: int) -> str: ...
