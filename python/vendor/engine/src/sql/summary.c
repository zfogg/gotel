static size_t close_group(token *t, size_t n, size_t i) {
  size_t depth = 0;
  for (size_t j = i; j < n; ++j) {
    if (sym(t[j], '('))
      ++depth;
    else if (sym(t[j], ')') && --depth == 0)
      return j;
  }
  return n;
}
static bool identifier(token t) {
  return (t.clickhouse && clickhouse_quote(t.p, t.n, 0) == 0x9c) || t.word || *t.p == '"' || *t.p == '`' || *t.p == '[';
}
static size_t name(output *b, token *t, size_t n, size_t i) {
  if (i >= n || !identifier(t[i]))
    return i;
  emit_token(b, t[i++]);
  while (i + 1 < n && sym(t[i], '.') && identifier(t[i + 1])) {
    text(b, ".");
    emit_token(b, t[i + 1]);
    i += 2;
  }
  return i;
}
static bool boundary(token t) {
  const char *words[] = {"FROM",  "WHERE",     "GROUP",  "HAVING",    "ORDER", "LIMIT", "OFFSET",  "FETCH", "FOR",
                         "UNION", "INTERSECT", "EXCEPT", "RETURNING", "SET",   "USING", "QUALIFY", "WINDOW"};
  if (t.clickhouse && (kw(t, "PREWHERE") || kw(t, "SETTINGS") || kw(t, "FORMAT") || kw(t, "SAMPLE") || kw(t, "VALUES")))
    return true;
  for (size_t i = 0; i < sizeof(words) / sizeof(*words); ++i)
    if (kw(t, words[i]))
      return true;
  return sym(t, ';') || sym(t, ')');
}
static size_t clause_end(token *t, size_t n, size_t i) {
  unsigned cases = 0;
  for (; i < n; ++i) {
    if (sym(t[i], '(')) {
      i = close_group(t, n, i);
      continue;
    }
    if (kw(t[i], "CASE")) {
      ++cases;
      continue;
    }
    if (kw(t[i], "END") && cases) {
      --cases;
      continue;
    }
    if (cases)
      continue;
    if (i && kw(t[i], "FROM") && kw(t[i - 1], "DISTINCT"))
      continue;
    if (i && kw(t[i], "GROUP") && kw(t[i - 1], "WITHIN"))
      continue;
    if (i + 1 < n && kw(t[i], "ON") && kw(t[i + 1], "CONFLICT"))
      break;
    if (boundary(t[i]))
      break;
  }
  return i;
}
static bool query_start(token t) {
  return kw(t, "SELECT") || kw(t, "WITH") || kw(t, "INSERT") || kw(t, "UPDATE") || kw(t, "DELETE") || kw(t, "MERGE") ||
         kw(t, "VALUES") || kw(t, "EXPLAIN");
}
static void summarize(output *, token *, size_t, unsigned);
static void nested(output *b, token *t, size_t n, unsigned depth) {
  if (depth > 64) {
    item(b, "…");
    return;
  }
  for (size_t i = 0; i < n; ++i)
    if (sym(t[i], '(')) {
      size_t end = close_group(t, n, i);
      if (i + 1 < end && query_start(t[i + 1])) {
        if (i && (kw(t[i - 1], "EXISTS") || kw(t[i - 1], "IN"))) {
          if (i > 1 && kw(t[i - 2], "NOT"))
            item(b, "NOT");
          item(b, kw(t[i - 1], "IN") ? "IN" : "EXISTS");
        }
        item(b, "(");
        summarize(b, t + i + 1, end - i - 1, depth + 1);
        text(b, ")");
      } else
        nested(b, t + i + 1, end - i - 1, depth + 1);
      i = end;
    }
}
static bool join_modifier(token t) {
  const char *words[] = {"NATURAL", "LEFT", "RIGHT", "FULL", "OUTER",     "INNER",
                         "CROSS",   "SEMI", "ANTI",  "ASOF", "POSITIONAL"};
  if (t.clickhouse && (kw(t, "GLOBAL") || kw(t, "ANY") || kw(t, "ALL") || kw(t, "ARRAY")))
    return true;
  for (size_t i = 0; i < sizeof(words) / sizeof(*words); ++i)
    if (kw(t, words[i]))
      return true;
  return false;
}
static size_t join_at(token *t, size_t n, size_t i) {
  size_t j = i;
  while (j < n && join_modifier(t[j]))
    ++j;
  if (j < n && kw(t[j], "JOIN")) {
    ++j;
    if (j < n && kw(t[j], "LATERAL"))
      ++j;
    return j;
  }
  return i;
}
static void expression(output *b, token *t, size_t n, unsigned depth) {
  size_t started = b->n;
  for (size_t i = 0; i < n; ++i) {
    if (t[i].space && b->n > started && b->p[b->n - 1] != ' ')
      text(b, " ");
    if (sym(t[i], '(')) {
      size_t end = close_group(t, n, i);
      text(b, "(");
      if (i + 1 < end && query_start(t[i + 1]))
        summarize(b, t + i + 1, end - i - 1, depth + 1);
      else if (depth < 64)
        expression(b, t + i + 1, end - i - 1, depth + 1);
      text(b, ")");
      i = end;
    } else if (isdigit((unsigned char)*t[i].p) ||
               (sym(t[i], '.') && i + 1 < n && isdigit((unsigned char)*t[i + 1].p))) {
      text(b, "?");
      if (sym(t[i], '.'))
        ++i;
    } else
      emit_token(b, t[i]);
  }
}
static void relations(output *, token *, size_t, unsigned);
static size_t relation(output *b, token *t, size_t n, size_t i, unsigned depth) {
  while (i < n && (kw(t[i], "ONLY") || kw(t[i], "LATERAL"))) {
    upper(b, t[i++]);
    text(b, " ");
  }
  if (i >= n) {
    text(b, "…");
    return i;
  }
  if (sym(t[i], '(')) {
    size_t end = close_group(t, n, i);
    text(b, "(");
    if (i + 1 < end && query_start(t[i + 1]))
      summarize(b, t + i + 1, end - i - 1, depth + 1);
    else
      relations(b, t + i + 1, end - i - 1, depth + 1);
    text(b, ")");
    i = end < n ? end + 1 : n;
  } else {
    size_t next = name(b, t, n, i);
    if (next == i) {
      text(b, "…");
      return i + 1;
    }
    i = next;
    if (i < n && sym(t[i], '(')) {
      size_t end = close_group(t, n, i);
      text(b, "(…)");
      i = end < n ? end + 1 : n;
    }
  }
  if (i + 1 < n && kw(t[i], "WITH") && kw(t[i + 1], "ORDINALITY")) {
    text(b, " WITH ORDINALITY");
    i += 2;
  }
  if (i < n && t[i].clickhouse && kw(t[i], "FINAL")) {
    text(b, " FINAL");
    ++i;
  }
  if (i < n && kw(t[i], "AS"))
    ++i;
  if (i < n && identifier(t[i]) && !boundary(t[i]) && !kw(t[i], "ON") && !kw(t[i], "WHEN") && !kw(t[i], "WITH") &&
      join_at(t, n, i) == i) {
    text(b, " AS ");
    emit_token(b, t[i++]);
    if (i < n && sym(t[i], '(')) {
      size_t end = close_group(t, n, i);
      expression(b, t + i, end - i + (end < n), depth + 1);
      i = end < n ? end + 1 : n;
    }
  }
  return i;
}
static void relations(output *b, token *t, size_t n, unsigned depth) {
  if (depth > 64) {
    text(b, "…");
    return;
  }
  size_t i = relation(b, t, n, 0, depth);
  while (i < n) {
    size_t next = join_at(t, n, i);
    if (next > i) {
      text(b, " ");
      for (; i < next; ++i) {
        upper(b, t[i]);
        text(b, " ");
      }
      i = relation(b, t, n, i, depth);
    } else if (sym(t[i], ',')) {
      text(b, ", ");
      i = relation(b, t, n, i + 1, depth);
    } else if (kw(t[i], "ON") || kw(t[i], "USING")) {
      text(b, " ");
      upper(b, t[i++]);
      text(b, " ");
      size_t start = i;
      int cases = 0;
      for (; i < n; ++i) {
        if (sym(t[i], '(')) {
          i = close_group(t, n, i);
          continue;
        }
        if (sym(t[i], '[')) {
          size_t depth_square = 1;
          while (++i < n && depth_square) {
            if (sym(t[i], '['))
              ++depth_square;
            else if (sym(t[i], ']'))
              --depth_square;
          }
          if (i < n)
            --i;
          continue;
        }
        if (kw(t[i], "CASE"))
          ++cases;
        else if (kw(t[i], "END") && cases)
          --cases;
        if (!cases && (join_at(t, n, i) > i || sym(t[i], ',')))
          break;
      }
      expression(b, t + start, i - start, depth + 1);
    } else
      ++i;
  }
}
static void bound(output *b, token *t, size_t n) {
  if (n == 1 && kw(t[0], "ALL")) {
    text(b, "ALL");
    return;
  }
  bool valid = n == 1 || (n == 2 && (sym(t[0], '-') || sym(t[0], '+')));
  if (valid) {
    token v = t[n - 1];
    for (size_t i = 0; i < v.n; ++i)
      if (!isdigit((unsigned char)v.p[i]))
        valid = false;
  }
  if (valid) {
    for (size_t i = 0; i < n; ++i)
      emit_token(b, t[i]);
  } else
    text(b, "?");
}
static void summarize(output *b, token *t, size_t n, unsigned depth) {
  if (depth > 64) {
    text(b, "…");
    return;
  }
  if (!n)
    return;
  size_t i = 0;
  if (t[0].clickhouse && kw(t[0], "WITH")) {
    /* Scalar aliases and CTEs share WITH in ClickHouse. Preserve the outer
     * query shape without exporting arbitrary scalar expressions. */
    for (size_t j = 1; j < n; ++j) {
      if (sym(t[j], '(')) {
        j = close_group(t, n, j);
        continue;
      }
      if (kw(t[j], "SELECT")) {
        text(b, "WITH … ");
        summarize(b, t + j, n - j, depth + 1);
        return;
      }
    }
  }
  if (kw(t[0], "WITH")) {
    text(b, "WITH ");
    i = 1;
    if (i < n && kw(t[i], "RECURSIVE")) {
      text(b, "RECURSIVE ");
      ++i;
    }
    for (;;) {
      size_t next = name(b, t, n, i);
      if (next == i) {
        text(b, "SQL");
        return;
      }
      i = next;
      if (i < n && sym(t[i], '('))
        i = close_group(t, n, i) + 1;
      if (i >= n || !kw(t[i], "AS")) {
        text(b, " SQL");
        return;
      }
      ++i;
      text(b, " AS");
      if (i < n && kw(t[i], "NOT")) {
        text(b, " NOT");
        ++i;
      }
      if (i < n && kw(t[i], "MATERIALIZED")) {
        text(b, " MATERIALIZED");
        ++i;
      }
      if (i >= n || !sym(t[i], '(')) {
        text(b, " SQL");
        return;
      }
      size_t end = close_group(t, n, i);
      text(b, " (");
      summarize(b, t + i + 1, end - i - 1, depth + 1);
      text(b, ")");
      i = end < n ? end + 1 : n;
      if (i >= n || !sym(t[i], ','))
        break;
      text(b, ", ");
      ++i;
    }
    text(b, " ");
  }
  if (i >= n)
    return;
  if (kw(t[i], "EXPLAIN")) {
    text(b, "EXPLAIN");
    ++i;
    if (i < n && sym(t[i], '(')) {
      size_t end = close_group(t, n, i);
      for (size_t j = i + 1; j < end; ++j)
        if (kw(t[j], "ANALYZE")) {
          text(b, " ANALYZE");
          break;
        }
      i = end < n ? end + 1 : n;
    }
    while (i < n && (kw(t[i], "ANALYZE") || kw(t[i], "VERBOSE") || kw(t[i], "QUERY") || kw(t[i], "PLAN"))) {
      if (kw(t[i], "ANALYZE"))
        text(b, " ANALYZE");
      ++i;
    }
    if (i < n) {
      text(b, " ");
      summarize(b, t + i, n - i, depth + 1);
    }
    return;
  }
  token operation_token = t[i];
  if (sym(t[i], '(')) {
    size_t end = close_group(t, n, i);
    text(b, "(");
    summarize(b, t + i + 1, end - i - 1, depth + 1);
    text(b, ")");
    i = end < n ? end + 1 : n;
  } else {
    token op = t[i++];
    const char *operations[] = {"SELECT", "INSERT", "UPDATE",   "DELETE", "MERGE",   "COPY",   "CALL",   "TRUNCATE",
                                "CREATE", "ALTER",  "DROP",     "VACUUM", "ANALYZE", "PRAGMA", "SHOW",   "SET",
                                "BEGIN",  "COMMIT", "ROLLBACK", "VALUES", "REPLACE", "PIVOT",  "UNPIVOT"};
    bool supported = false;
    for (size_t j = 0; j < sizeof(operations) / sizeof(*operations); ++j)
      if (kw(op, operations[j]))
        supported = true;
    if (!supported) {
      text(b, "SQL");
      return;
    }
    upper(b, op);
    if (kw(op, "PIVOT") || kw(op, "UNPIVOT")) {
      if (i < n) {
        text(b, " ");
        name(b, t, n, i);
      }
      return;
    }
    if (kw(op, "CREATE")) {
      size_t j = i;
      bool unique = j < n && kw(t[j], "UNIQUE");
      if (unique)
        ++j;
      if (j < n && kw(t[j], "INDEX")) {
        ++j;
        if (j < n && kw(t[j], "CONCURRENTLY"))
          ++j;
        if (j + 2 < n && kw(t[j], "IF") && kw(t[j + 1], "NOT") && kw(t[j + 2], "EXISTS"))
          j += 3;
        char index_name[4097] = {0};
        output index = {index_name, 0, sizeof(index_name)};
        size_t next = name(&index, t, n, j);
        if (next > j && next < n && kw(t[next], "ON")) {
          item(b, unique ? "UNIQUE INDEX" : "INDEX");
          item(b, index_name);
          item(b, "ON ...");
          if (index.overflow)
            b->overflow = true;
          return;
        }
      }
    }
    if (kw(op, "SELECT")) {
      if (i < n && (kw(t[i], "DISTINCT") || kw(t[i], "ALL"))) {
        text(b, " ");
        upper(b, t[i++]);
        if (i < n && kw(t[i], "ON")) {
          item(b, "ON (…)");
          ++i;
          if (i < n && sym(t[i], '('))
            i = close_group(t, n, i) + 1;
        }
      }
      size_t end = clause_end(t, n, i);
      char function_text[4097] = {0};
      output functions = {function_text, 0, sizeof(function_text)};
      for (size_t j = i; j < end; ++j) {
        if (sym(t[j], '(')) {
          j = close_group(t, n, j);
          continue;
        }
        char fn[1024] = {0};
        output f = {fn, 0, sizeof(fn)};
        size_t next = name(&f, t, end, j);
        if (next > j && next < end && sym(t[next], '(') && !kw(t[j], "EXISTS") && !kw(t[j], "CAST")) {
          size_t close = close_group(t, n, next);
          if (functions.n)
            text(&functions, ", ");
          text(&functions, fn);
          text(&functions, close == next + 1 ? "()" : "(…)");
          j = next - 1;
        }
      }
      if (end < n && kw(t[end], "FROM"))
        item(b, "…");
      else if (functions.n)
        item(b, function_text);
      else
        item(b, "…");
      nested(b, t + i, end - i, depth + 1);
      for (size_t j = i; j < end; ++j) {
        if (sym(t[j], '(')) {
          j = close_group(t, n, j);
          continue;
        }
        if (kw(t[j], "OVER"))
          item(b, "OVER (…)");
        if (kw(t[j], "FILTER"))
          item(b, "FILTER (…)");
        if (j + 1 < end && kw(t[j], "WITHIN") && kw(t[j + 1], "GROUP"))
          item(b, "WITHIN GROUP (…)");
      }
      i = end;
    } else if (kw(op, "UPDATE")) {
      text(b, " ");
      size_t end = clause_end(t, n, i);
      relations(b, t + i, end - i, depth + 1);
      i = end;
    } else if (kw(op, "VALUES")) {
      item(b, "…");
      return;
    } else if (kw(op, "CALL") || kw(op, "COPY") || kw(op, "VACUUM") || kw(op, "ANALYZE") || kw(op, "TRUNCATE")) {
      if (i < n && kw(t[i], "TABLE"))
        ++i;
      text(b, " ");
      i = name(b, t, n, i);
      if (kw(op, "CALL"))
        return;
    }
  }
  while (i < n) {
    token v = t[i];
    if (sym(v, ';') || sym(v, ')'))
      break;
    if (kw(v, "UNION") || kw(v, "INTERSECT") || kw(v, "EXCEPT")) {
      text(b, " ");
      upper(b, t[i++]);
      while (i < n && (kw(t[i], "ALL") || kw(t[i], "DISTINCT") || kw(t[i], "BY") || kw(t[i], "NAME"))) {
        text(b, " ");
        upper(b, t[i++]);
      }
      text(b, " ");
      summarize(b, t + i, n - i, depth + 1);
      return;
    }
    if (kw(v, "FROM") || kw(v, "USING")) {
      if (kw(operation_token, "COPY"))
        break;
      text(b, " ");
      upper(b, v);
      text(b, " ");
      /* A relation may itself have a clause-keyword name (e.g. ClickHouse
       * system.settings). Consume that first name before finding clauses. */
      size_t start = i + 1;
      if (start < n && identifier(t[start])) {
        ++start;
        while (start + 1 < n && sym(t[start], '.') && identifier(t[start + 1]))
          start += 2;
      }
      size_t end = clause_end(t, n, start);
      relations(b, t + i + 1, end - i - 1, depth + 1);
      i = end;
      continue;
    }
    if (kw(v, "INTO")) {
      item(b, "INTO ");
      i = kw(t[0], "MERGE") ? relation(b, t, n, i + 1, depth + 1) : name(b, t, n, i + 1);
      if (i < n && sym(t[i], '('))
        i = close_group(t, n, i) + 1;
      continue;
    }
    if (kw(v, "SELECT") && kw(operation_token, "INSERT")) {
      text(b, " ");
      summarize(b, t + i, n - i, depth + 1);
      return;
    }
    if (kw(v, "LIMIT") || kw(v, "OFFSET")) {
      text(b, " ");
      upper(b, v);
      text(b, " ");
      size_t end = clause_end(t, n, i + 1), comma = i + 1;
      size_t bound_end = end, by = end;
      if (v.clickhouse)
        for (size_t j = i + 1; j < end; ++j)
          if (kw(t[j], "BY")) {
            by = j;
            bound_end = j;
            break;
          }
      if (bound_end > i + 1 && (kw(t[bound_end - 1], "ROWS") || kw(t[bound_end - 1], "ROW")))
        --bound_end;
      while (comma < bound_end && !sym(t[comma], ',')) {
        if (sym(t[comma], '('))
          comma = close_group(t, bound_end, comma);
        if (comma < bound_end)
          ++comma;
      }
      bound(b, t + i + 1, (comma < bound_end ? comma : bound_end) - i - 1);
      if (comma < bound_end) {
        text(b, ", ");
        bound(b, t + comma + 1, bound_end - comma - 1);
      }
      if (by < end)
        item(b, "BY …");
      nested(b, t + i + 1, bound_end - i - 1, depth + 1);
      i = end;
      continue;
    }
    if (kw(v, "FOR")) {
      item(b, "FOR");
      ++i;
      while (i < n) {
        bool matched = false;
        const char *locks[] = {"UPDATE", "NO", "KEY", "SHARE", "SKIP", "LOCKED", "NOWAIT"};
        for (size_t j = 0; j < sizeof(locks) / sizeof(*locks); ++j)
          if (kw(t[i], locks[j])) {
            item(b, locks[j]);
            matched = true;
            break;
          }
        if (!matched && boundary(t[i]))
          break;
        ++i;
      }
      continue;
    }
    if (kw(v, "FETCH")) {
      size_t end = clause_end(t, n, i + 1), j = i + 1;
      item(b, "FETCH");
      if (j < end && (kw(t[j], "FIRST") || kw(t[j], "NEXT"))) {
        text(b, " ");
        upper(b, t[j++]);
      } else
        item(b, "FIRST");
      size_t row = j;
      while (row < end && !kw(t[row], "ROW") && !kw(t[row], "ROWS"))
        ++row;
      text(b, " ");
      if (row > j)
        bound(b, t + j, row - j);
      else
        text(b, "1");
      item(b, row + 2 < end && kw(t[row + 1], "WITH") && kw(t[row + 2], "TIES") ? "ROWS WITH TIES" : "ROWS ONLY");
      i = end;
      continue;
    }
    if (kw(v, "ON") && i + 1 < n && kw(t[i + 1], "CONFLICT")) {
      i += 2;
      if (i < n && sym(t[i], '('))
        i = close_group(t, n, i) + 1;
      while (i < n && !kw(t[i], "DO") && !kw(t[i], "RETURNING"))
        ++i;
      if (i + 1 < n && kw(t[i], "DO")) {
        item(b, "ON CONFLICT DO ");
        upper(b, t[i + 1]);
        i += 2;
      }
      continue;
    }
    if ((v.clickhouse && (kw(v, "PREWHERE") || kw(v, "SETTINGS") || kw(v, "SAMPLE") || kw(v, "FORMAT"))) ||
        kw(v, "WHERE") || kw(v, "HAVING") || kw(v, "QUALIFY") || kw(v, "SET") || kw(v, "RETURNING") || kw(v, "GROUP") ||
        kw(v, "ORDER") || kw(v, "WINDOW")) {
      text(b, " ");
      upper(b, v);
      ++i;
      if (i < n && kw(t[i], "BY")) {
        text(b, " BY");
        ++i;
      }
      size_t end = clause_end(t, n, i);
      item(b, "…");
      nested(b, t + i, end - i, depth + 1);
      i = end;
      continue;
    }
    if (kw(v, "VALUES")) {
      item(b, "VALUES …");
      ++i;
      continue;
    }
    if (sym(v, '(')) {
      size_t end = close_group(t, n, i);
      nested(b, t + i, end - i + (end < n), depth + 1);
      i = end < n ? end + 1 : n;
      continue;
    }
    ++i;
  }
}
